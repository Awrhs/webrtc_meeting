# 媒体 E2EE 限制说明

## 加密层级对比

### DTLS-SRTP（默认传输加密）

音视频媒体默认使用 WebRTC 标准 DTLS-SRTP。这是 hop-by-hop 传输加密：

- 浏览器 → SFU：DTLS-SRTP 加密
- SFU → 浏览器：DTLS-SRTP 加密
- SFU 内部持有明文媒体帧，可以完成转发、录制、转码等操作

### 媒体 E2EE（可选高安全模式）

在支持 `RTCRtpScriptTransform` 的浏览器中，可选开启媒体端到端加密：

- 发送端浏览器在 encoded frame 层面使用 AES-GCM 加密每一帧
- SFU 只看到加密后的 RTP payload，按 RTP header/SSRC 转发，不持有媒体密钥
- 接收端浏览器使用对应密钥解密后解码播放
- 密钥通过文本 E2EE 的 Sender Key 通道在成员间分发，SFU 无法解密

## 浏览器支持

媒体 E2EE 依赖 WebRTC Encoded Transform API（`RTCRtpScriptTransform`），该 API 在以下浏览器中可用：

| 浏览器 | 支持状态 |
|--------|---------|
| Chrome 88+ | 支持 |
| Edge 88+ | 支持 |
| Firefox 117+ | 支持（需启用 `media.webrtc.scripttransform.enabled`） |
| Safari | 不支持 |
| iOS Safari / WKWebView | 不支持 |
| Android Chrome | 支持 |

不支持的浏览器会自动回退到 DTLS-SRTP 传输加密，并在会议页面显示状态标签。

## 开启媒体 E2EE 后的功能限制

媒体 E2EE 开启后，SFU 无法访问明文媒体帧，以下功能不可用：

- **服务端录制**：SFU 只能录制加密后的 RTP payload，无法生成可播放的媒体文件
- **服务端转码**：无法在不同编码格式间转换
- **服务端转写/语音识别**：无法提取明文音频
- **智能摘要/内容审核**：无法分析媒体内容
- **服务端合流/混流**：无法进行多路视频合成

如果需要这些能力，可选方案包括：

- 引入持密钥的录制客户端/机器人，以成员身份加入房间
- 改变信任模型，在特定场景下不使用媒体 E2EE

## 与标准的关系

当前实现不是完整的 SFrame（RFC 9605）或 MLS（RFC 9420）标准实现：

- 加密格式：自定义 header（magic + version + epoch + iv）+ AES-GCM ciphertext
- 密钥分发：复用文本 E2EE 的 Sender Key 通道（ECDH P-256 + HKDF ratchet）
- 密钥轮换：成员变更时 sender 递增 epoch 并分发新 media key

未来可考虑迁移到标准 SFrame 格式以支持跨厂商互操作。

## 诊断信息

媒体 E2EE 在 Worker 线程中收集以下诊断指标（不含密钥材料）：

- 加密帧计数 / 解密帧计数
- 解密失败计数
- 缺失接收密钥计数
- 未知 epoch 计数
- send key / receive key 设置状态

Worker 通过 `postMessage` 将诊断事件回传给主线程，主线程根据诊断数据更新 UI 状态：

- `enabled`：正常加密/解密中
- `negotiating`：密钥协商中
- `warning`：出现解密失败或密钥缺失，需检查
- `unsupported`：浏览器不支持，已回退 DTLS-SRTP
- `disabled`：用户或配置关闭了媒体 E2EE

## 手动验证步骤

1. 准备两个支持 Encoded Transform 的浏览器（Chrome/Edge），分别登录不同用户
2. 两个用户加入同一房间
3. 检查会议页面顶部的加密状态标签，应显示"媒体 E2EE 已启用"
4. 双方开启摄像头和麦克风，确认能互相看到视频、听到音频
5. 开启屏幕共享，确认远端能看到共享内容
6. 第三个用户加入，确认现有用户的媒体不中断
7. 在 `chrome://webrtc-internals/` 中检查连接状态和统计信息
8. 用不支持浏览器（Safari）加入，确认显示"媒体 E2EE 不支持，已回退 DTLS-SRTP"

## 安全考虑

- 媒体 E2EE 保护的是 SFU 不能访问媒体内容，但 SFU 仍然可以看到 RTP header（SSRC、序列号、时间戳等元数据）
- 密钥通过文本 E2EE 通道分发，其安全性取决于文本 E2EE 的密钥交换和成员管理
- 当前不提供 Perfect Forward Secrecy（PFS）：如果长期密钥泄露，历史媒体帧可被解密
- 没有实现发送方身份认证：接收方无法密码学验证媒体帧确实来自声称的发送方
