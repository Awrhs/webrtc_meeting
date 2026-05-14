# WSL2 环境下 WebRTC/SFU 本地测试指南

本文档记录了在 WSL2 中运行 SFU 服务器、Windows 主机浏览器中进行 WebRTC 本地测试时遇到的核心问题、原因分析与解决方案。

---

## 1. 问题现象

在 WSL2 中启动 SFU（`backend/cmd/sfu/main.go`），Windows 浏览器访问前端测试页面（`sfu-test.html` / `sfu-test-no-camera.html`），出现以下现象：

- **信令层完全正常**：WebSocket 连接成功，`join`、`offer`、`answer`、`ice-candidate` 消息全部正常收发。
- **媒体层失败**：约 20~30 秒后，SFU 日志输出 `Peer connection failed (ICE negotiation failed)`，浏览器收不到远程视频流。

### 典型日志

浏览器端：
```
[15:26:03] SDP Offer已发送到SFU
[15:26:03] WebRTC协商完成！
[15:26:03] 本地ICE候选 [host]: candidate:2590442690 ... fdcede12-5f50-4551-9a9b-946c76e88624.local ...
[15:26:09] 警告: 5秒内未收到任何本地ICE候选
[15:26:33] PeerConnection 连接状态: failed
```

SFU 端：
```
{"level":"warn","msg":"Peer connection failed (ICE negotiation failed)","roomID":1,"userID":1}
```

---

## 2. 根本原因分析

### 2.1 WebSocket（TCP） ≠ WebRTC（UDP）

很多开发者（包括我们最初）误以为：既然 WebSocket TCP 连接通了，说明双方 IP 互通，WebRTC 的 UDP 媒体通道也应该能通。

**实际上两者是完全独立的机制：**

| 维度 | WebSocket 信令 | WebRTC ICE 媒体 |
|------|---------------|----------------|
| 协议 | TCP (HTTP Upgrade) | UDP (STUN/TURN/RTP) |
| 发起方式 | 浏览器**单向**连接 SFU | **双向**互相发送 STUN 探测包 |
| 地址确定 | 你手动输入 `http://172.25.24.77:8081` | 双方各自收集 ICE 候选，交换列表后配对 |
| NAT/防火墙 | WSL2 自动转发入站 TCP | UDP 入站常被防火墙丢弃；WSL2 NAT 下回环 UDP 可能受阻 |

WebSocket 的成功只证明了**浏览器能主动发 TCP 到 WSL2**。WebRTC ICE 要求**双方都能主动向对端发 UDP**，这是两回事。

### 2.2 Chrome mDNS 隐私保护

Chrome 默认启用 **WebRTC IP 隐私保护**：浏览器不会直接暴露真实内网 IP（如 `192.168.x.x`、`172.25.24.1`），而是生成一个 **mDNS 地址**（如 `xxxx.local`）作为 ICE host 候选。

**带来的问题：**
- SFU 运行在 WSL2 Linux 虚拟机中，**没有 mDNS 解析能力**（缺少 avahi/bonjour）。
- SFU 收到 `.local` 候选后，调用 `AddICECandidate` 时，Pion ICE agent 无法解析该域名，导致**无法向浏览器发送 UDP STUN 探测包**。
- 浏览器虽然可以向 SFU 的 host 候选（`172.25.24.77`）发送 UDP，但 ICE 是双向握手机制，单方向成功不算数。

### 2.3 WSL2 NAT 网络模型

WSL2 默认采用 **NAT 模式**：
- WSL2 虚拟机拥有独立网卡和 IP（如 `172.25.24.77`）。
- Windows 主机通过虚拟交换机访问 WSL2。
- WSL2 访问 Windows 主机时，目标 IP 是虚拟网关（如 `172.25.24.1`）。

这层 NAT 带来两个麻烦：
1. **mDNS 多播无法穿透**：即使 WSL2 安装了 avahi-daemon，Windows 浏览器广播的 mDNS 是否能被 WSL2 收到，取决于 Hyper-V 虚拟交换机对多播的支持，**不可靠**。
2. **Windows 防火墙入站 UDP**：WSL2 主动向 Windows 主机发 UDP 时，可能被 Windows Defender Firewall 拦截（尤其是 Public 网络 profile 下）。

---

## 3. 解决方案对比

以下是在**本地测试环境**中可行的方案，按推荐程度排序。

### 方案 A：SFU 服务端自动替换 mDNS 候选（推荐，已实现）

**原理：**
浏览器通过 WebSocket 连接 SFU 时，`RemoteAddr` 已经暴露了浏览器在 WSL2 虚拟子网中的真实 IP（如 `172.25.24.1`）。既然 SFU 无法解析 `.local`，**直接用 WebSocket 的 `RemoteAddr` IP 替换掉 candidate 字符串中的 mDNS 域名**即可。

**优点：**
- 不需要启动 coturn。
- 不需要修改浏览器任何设置（保留 mDNS 隐私保护）。
- 不需要修改 WSL2 网络模式。
- 改动极小，仅在 SFU 服务端一行代码逻辑。

**缺点：**
- 属于应用层 workaround，仅适用于**SFU 和浏览器在同一台物理机**（或同一局域网直连）的场景。
- 如果 WebSocket 前端有反向代理导致 `RemoteAddr` 失真，会失效（生产环境应使用标准 STUN/TURN）。

**实现位置：**
`backend/internal/sfu/server.go:handleIceCandidate`

```go
if strings.Contains(candidateStr, ".local") {
    if tcpAddr, ok := peer.Conn.RemoteAddr().(*net.TCPAddr); ok {
        realIP := tcpAddr.IP.String()
        parts := strings.Split(candidateStr, " ")
        if len(parts) >= 6 {
            parts[4] = realIP  // 替换 candidate 中的地址字段
            candidateStr = strings.Join(parts, " ")
        }
    }
}
```

---

### 方案 B：启动 Coturn（TURN 中继）

**原理：**
让浏览器和 SFU 都向 coturn 申请 **relay 候选**，双方通过 coturn 中继 UDP 数据，绕开直连的 IP 发现障碍。

**优点：**
- 最符合 WebRTC 生产架构，无需任何 hack。
- 浏览器保留 mDNS，无需任何修改。
- 即使跨网络、跨 NAT 也能工作。

**缺点：**
- 本地测试需要额外启动一个 Docker 容器。
- 媒体数据走中继，增加了本地测试的复杂度。

**启动命令：**
```bash
docker run -d --name coturn \
  --network host \
  -p 3478:3478 -p 3478:3478/udp \
  -p 49152-49200:49152-49200/udp \
  instrumentisto/coturn:latest \
  turnserver --log-file=stdout \
  --external-ip=$(hostname -I | awk '{print $1}') \
  --realm=conference \
  --user=username:password \
  --lt-cred-mech
```

---

### 方案 C：禁用 Chrome mDNS（本地调试捷径）

**原理：**
强制 Chrome 暴露真实内网 IP 作为 ICE host 候选，SFU 直接通过 `172.25.24.1` 或 `192.168.x.x` 与浏览器建立 UDP 连接。

**操作：**
1. Chrome 地址栏输入 `chrome://flags/#enable-webrtc-hide-local-ips-with-mdns`
2. 设为 **Disabled**
3. 重启浏览器

**优点：**
- 无需启动 coturn，无需修改后端代码。

**缺点：**
- **仅适用于本地开发**，上线后不可能让每个用户都去改浏览器 flag。
- 如果 Windows 防火墙严格，依然可能阻断 WSL2 → Windows 的入站 UDP。

---

### 方案 D：WSL2 Mirror 模式

**原理：**
从 Windows 11 开始，WSL2 支持 `networkingMode=mirrored`。开启后 WSL2 与 Windows 主机共享网络命名空间，不再有独立的 `172.x.x.x` 虚拟网卡。

**操作：**
在 `C:\Users\<用户名>\.wslconfig` 中添加：
```ini
[wsl2]
networkingMode=mirrored
```
然后执行 `wsl --shutdown` 重启 WSL2。

**优点：**
- 网络模型更简洁，`localhost` 完全互通。
- 理论上减少了 NAT 带来的防火墙问题。

**缺点：**
- **单独使用不能解决 mDNS 问题**。Mirror 模式下 Chrome 仍然发送 `.local` 候选，SFU 依然解析不了。
- 需要配合方案 A（候选替换）或方案 C（禁用 mDNS）一起使用。
- 仅 Windows 11+ 支持。

---

### 方案 E：WSL2 内启用 Avahi mDNS 解析

**原理：**
在 WSL2 中安装 avahi-daemon，让 Linux 具备解析 `.local` 域名的能力。

**操作：**
```bash
sudo apt update && sudo apt install -y avahi-daemon
sudo service avahi-daemon start
```

**缺点：**
- WSL2 与 Windows 主机之间的 mDNS 多播广播**不一定能穿透 Hyper-V 虚拟交换机**，实验结果表明不可靠。
- 即使解析成功，Windows 防火墙仍可能阻挡来自 WSL2 的入站 UDP。
- **不推荐**，性价比低。

---

## 4. 推荐的本地最小测试配置

如果你希望**本地测试最轻量、最快速**，推荐以下组合：

1. **保持 WSL2 默认 NAT 模式**（无需改 `.wslconfig`）。
2. **保持 Chrome 默认设置**（不关闭 mDNS，不改动浏览器）。
3. **使用已实现的方案 A**：SFU 自动替换 mDNS 候选（代码已在 `backend/internal/sfu/server.go` 中）。
4. **确保防火墙允许本地子网 UDP**：Windows Defender Firewall 对"专用网络"的入站 UDP 通常是允许的。如果仍被拦截，可临时关闭防火墙验证：
   ```powershell
   # PowerShell 管理员权限（仅用于测试排查）
   Set-NetFirewallProfile -Profile Private -Enabled False
   ```

### 启动命令

终端 1（项目根目录）：
```bash
WSL2_COMPAT=1 go run backend/cmd/sfu/main.go
```

终端 2（frontend 目录）：
```bash
cd frontend && npm run dev
```

浏览器打开两个窗口：
- `http://localhost:3000/sfu-test-no-camera.html`
- 窗口 1 填写用户 ID = 1，窗口 2 填写用户 ID = 2，房间 ID 相同（如 1）。
- 依次点击"生成模拟媒体流" → "连接 SFU"。

---

## 5. 生产环境怎么办？

本文档讨论的所有问题（mDNS、WSL2 NAT、候选替换）**仅限于本地开发测试环境**。

生产环境的标准做法是：
1. **部署 Coturn 或类似 TURN 服务器**。
2. **前端配置真实的 STUN/TURN 地址**（通过 `.env` 中的 `VITE_SFU_HOST` / `VITE_TURN_URL` 等）。
3. **SFU 服务端不依赖 mDNS 解析**，因为公网用户的 srflx + relay 候选足以完成配对。
4. **无需任何候选替换 hack**，后端代码保持标准 WebRTC 逻辑。

---

## 6. 关键认知纠正

### Q: SFU 架构下用户不直接相连，为什么还需要 ICE "打洞"？

**A:** SFU 只是"转发逻辑"的架构。在传输层，浏览器和 SFU 之间仍然是 **WebRTC PeerConnection**。WebRTC 基于 UDP/RTP，建立这条传输通道必须经过 ICE 协议（收集候选、STUN 探测、配对），无论对方是另一个浏览器还是服务器。

### Q: WebSocket TCP 通了，为什么 UDP 不通？

**A:** TCP 连接是浏览器**单向主动**发起的（浏览器 → WSL2），WSL2 虚拟交换机对入站 TCP 做了端口转发。UDP ICE 是**双向**的：SFU 必须能**主动**向浏览器发 UDP STUN 包。当 SFU 不知道浏览器的真实 IP（因为 mDNS）时，这条反向路径就断了。

### Q: 启用 coturn 是为了帮助双方"发现"IP 吗？

**A:** 不是。coturn 不提供"发现"功能，它提供的是**双方都已知可达的公共中继地址**。浏览器和 SFU 各自向 coturn 注册后，双方只需向 coturn 的地址发数据，coturn 负责最后一公里投递，绕开了"SFU 必须知道浏览器真实 IP"这个障碍。
