# ICE Loopback (127.0.0.1) 测试分析报告

## 背景

在 WSL2 Mirrored 网络模式下，SFU 与浏览器之间建立 WebRTC 连接时出现了 `connected -> disconnected -> connected` 的抖动现象。为验证该抖动是否由 `127.0.0.1` loopback 候选的 ICE Consent 超时引起，我们实现了一个测试模式（`SFU_TEST_MODE=true`），强制 SFU 在 SDP 中只保留 `127.0.0.1` ICE 候选。

## 测试方法

1. 在 `NewSFUServer` 中读取环境变量 `SFU_TEST_MODE`，启用测试模式。
2. 测试模式下，SFU 的 ICE Servers 替换为国内 STUN（`stun.qq.com:3478`、`stun.miwifi.com:3478`、`stun.syncthing.net:3478`）。
3. `filterSDPCandidates` 方法在 `handleOffer`（初始 Answer）和 `renegotiate`（重新协商 Offer）中对 SDP 进行过滤：
   - `testMode=true`：仅保留 `127.0.0.1` 候选。
   - `testMode=false`：过滤掉 `127.0.0.1` 和 `::1` 候选。
4. 使用 `frontend/public/sfu-test-no-camera.html` 在 Edge 浏览器中打开两个页面，分别使用不同的 User ID（1 和 2）。

## 关键发现

### 1. 浏览器不会生成 127.0.0.1 ICE 候选

从 `edge://webrtc-internals` 的日志中观察到，浏览器发出的 `onicecandidate` 事件仅包含两类候选：

- **mDNS host 候选**：如 `candidate:1385164648 1 udp ... 9cf68495-9e7b-4552-af0c-52432330a603.local ... typ host`
- **srflx 候选**：如 `candidate:1167991091 1 udp ... 211.148.195.212 ... typ srflx`

**完全没有 `127.0.0.1` 的 host 候选。** 这是 Edge/Chrome 的标准行为（出于隐私和安全考虑，浏览器默认不将 loopback 地址作为 ICE 候选暴露）。

### 2. 单方面过滤 SFU 候选导致路由不对称

当 SFU 的 SDP 中只有 `127.0.0.1` 时：

- **浏览器 -> SFU（上行）**：浏览器使用自己的 srflx (`211.148.195.212`) 或 mDNS 解析后的局域网 IP，尝试连接 SFU 的 `127.0.0.1`。在 WSL2 Mirrored 模式下，Windows 浏览器访问 WSL2 的 `127.0.0.1` 在**入向**是通的，因此 SFU 能够成功收到浏览器发送的 RTP 包（日志中出现了 `New track received`）。
- **SFU -> 浏览器（下行）**：SFU 从 `127.0.0.1` 的 socket 向浏览器的公网/局域网 IP 发送数据。这条路径在 WSL2 Mirrored 的 NAT hairpinning 下**大概率失败**。

结果是：ICE 连接在初期可能建立，但由于持续的双向 ICE Consent 检查失败，约 5 秒后进入 `disconnected`，随后 `failed`。

### 3. 重新协商竞态问题仍未修复

在测试过程中，日志中仍然出现了以下错误：

```
InvalidModificationError: invalid proposed signaling state transition: have-local-offer->SetLocal(offer)->have-local-offer
```

原因：`onTrack` 中 audio 和 video 两个 track 的到达几乎同时触发 `go s.renegotiate(otherPeer)`，两个 goroutine 并发对同一个 PeerConnection 调用 `CreateOffer` + `SetLocalDescription`，导致信令状态冲突。需要引入串行化机制（如 per-peer 锁或队列）来保证同一时刻只有一个 renegotiation 在进行。

## 结论

1. **强制 SFU 只保留 `127.0.0.1` 在当前标准浏览器环境下不可行。** 浏览器自身不会生成 `127.0.0.1` 候选，无法建立对称的双向 loopback 通道。
2. **WSL2 Mirrored 模式下的 ICE 抖动问题，其根因并非 `127.0.0.1` 候选本身，而是 renegotiation Offer 中泄漏了非 loopback 候选（如 `192.168.82.50`、`10.255.255.254`、srflx），导致浏览器在重新协商后尝试切换到这些候选，引发连接震荡。** 该问题已在本次代码修改中修复（`renegotiate()` 中也加入了 `filterSDPCandidates`）。
3. **建议下一步：**
   - 关闭 `SFU_TEST_MODE`（或不设置该环境变量）。
   - 使用正常模式（或国内 STUN）重新测试。
   - 修复 renegotiation 的竞态锁问题。
   - 观察 `connected -> disconnected -> connected` 是否消失。

## 相关代码变更

- `backend/internal/sfu/server.go`：新增 `filterSDPCandidates` 统一过滤方法；`renegotiate()` 和 `handleOffer()` 均调用该方法；`NewSFUServer` 支持 `SFU_TEST_MODE` 和国内 STUN 切换。
