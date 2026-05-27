# 用户权限与安全边界说明

## 权限模型

系统采用三类房间角色：

- `owner`：房主，拥有房间管理全部权限。
- `admin`：管理员，可执行踢人、禁言等管理操作。
- `member`：普通成员，可参与会议、收发消息、更新自己的媒体状态。

后端在 `backend/internal/models/room.go` 中定义角色、权限枚举和 `RolePermissions` 权限矩阵。前端只使用后端返回的 `my_permissions` 控制按钮展示，不能作为安全边界。

## 已实现的权限边界

### 1. HTTP API

| 功能 | 当前校验 |
|------|----------|
| 获取房间详情 `GET /rooms/:id` | 只有 active room member 可读取成员列表 |
| 房间号查询 `GET /rooms/number/:number` | 只返回加入房间前必要的基本信息，不返回成员列表和邀请码等完整对象 |
| 加入房间 `POST /rooms/:id/join` | 校验锁房、人数上限、密码/邀请码、踢出封禁 |
| 获取 SFU token `POST /rooms/:id/sfu-token` | 必须是 active room member，且未处于踢出封禁期 |
| 删除/锁定/解锁房间 | 房主权限 |
| 踢人 | 管理员或房主；管理员不能踢房主或其他管理员；不能踢自己 |
| 禁言/解除禁言 | 管理员或房主 |
| 设置/取消管理员 | 房主权限 |
| 转让房主 | 房主权限，目标必须是 active member |
| 更新媒体状态 | 必须是 active room member，只能更新自己的状态 |
| 读取/发送聊天消息 | 必须是 active room member |
| 私聊 receiver | 必须是同一房间 active member |

### 2. 普通信令 WebSocket

普通信令入口是 API Server `/ws?token=<app_jwt>`。

后端 Hub 不信任客户端传入的 `user_id`，会用 JWT 中的用户身份覆盖消息发送者。以下消息在转发前都会校验发送者已加入对应 Hub 房间且仍是 active member：

- `offer`
- `answer`
- `ice_candidate`
- `media_state`
- `public_key`
- `sender_key`

如果消息带 `target_id`，后端还会校验目标用户是同一房间 active member。非成员无法通过普通信令订阅或注入该房间消息。

### 3. SFU 媒体 WebSocket

SFU 入口是 SFU Server `/ws?token=<sfu_token>`。

默认情况下 SFU 不接受裸连。用户必须先通过 API 成功加入房间，由 API 签发短期 `sfu_token`。SFU 校验：

- token 签名；
- `purpose=sfu_join`；
- `room_id`；
- `user_id`；
- 过期时间。

连接后，SFU join 使用 token claims 覆盖客户端传入的 room/user 字段，避免伪造身份进入其他房间。

## 传输加密状态

### 音视频

音视频媒体使用 WebRTC 标准 DTLS-SRTP 传输加密。浏览器到 SFU、SFU 到浏览器每一跳都是加密传输。

项目已提供可选媒体 E2EE 高安全模式：支持 `RTCRtpScriptTransform` 的浏览器会在端侧加密 encoded audio/video frame，SFU 只按 RTP header/SSRC 转发密文 payload，不持有媒体密钥。Worker 线程收集加密/解密帧计数和失败计数等诊断指标，主线程据此更新 UI 状态。浏览器不支持该 API 时会回退到 DTLS-SRTP 传输加密。开启媒体 E2EE 后，服务端录制、转码、转写和智能摘要等依赖明文媒体的能力不可用。详见 `docs/E2EE_LIMITATIONS.md`。

### 群聊文本

群聊消息已具备基本的前端 E2EE 链路：

- `public_key` 通过普通信令在房间内转发；
- 前端用 ECDH P-256 建立成员间共享密钥；
- Sender Key 通过 `sender_key` 单播给目标成员；
- 成员加入、离开或被踢后，前端轮换 per-sender epoch 并只向当前 active members 分发新 Sender Key；
- 文本内容用前端加密后提交给后端；
- 后端保存 `encrypted` 标记并广播密文；
- 接收端前端解密后显示。

仍需注意：当前文本 E2EE 是 Sender Keys 基础实现，还不是完整生产级 MLS。消息重放防护、设备级身份验证和完整审计仍需后续补齐。

## 当前仍建议改进

1. 为关键权限路径补自动化测试：非成员不能 join signaling room、不能读/发聊天、不能更新媒体状态、不能获取 SFU token。
2. 操作审计目前有模型和 service 方法，但关键管理操作尚未系统化写入审计日志。
3. 将权限错误细分为更明确的错误码，便于前端展示和审计。
4. 为普通信令和 SFU token 增加更细的安全测试覆盖。