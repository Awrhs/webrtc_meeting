# 前端架构说明

## 当前技术栈

- React 18 + TypeScript
- Vite
- Ant Design
- Redux Toolkit
- React Router v6

## 目录结构

```text
frontend/src/
├── main.tsx
├── App.tsx
├── pages/
│   ├── Login.tsx
│   ├── Register.tsx
│   ├── RoomList.tsx
│   ├── MeetingRoom.tsx
│   └── Profile.tsx
├── components/
│   ├── ProtectedRoute.tsx
│   ├── VideoGrid.tsx
│   ├── MemberList.tsx
│   └── Chat.tsx
├── store/
│   ├── index.ts
│   └── slices/
│       ├── authSlice.ts
│       └── roomSlice.ts
├── types/
└── utils/
    ├── api.ts
    ├── signaling.ts
    ├── webrtc.ts
    └── encryption.ts
```

## 核心页面与组件

### `RoomList.tsx`

负责房间列表、创建房间、按房间号加入房间、展示房主的房间管理入口。房间加入会调用后端 `POST /api/v1/rooms/:id/join`，由后端返回房间信息、成员列表、当前用户权限和短期 `sfu_token`。

### `MeetingRoom.tsx`

会议页是音视频、成员、聊天和房间操作的组合入口：

1. 通过 Redux thunk 调用 `joinRoom`。
2. 使用 API 返回的 `sfu_token` 创建 `SFUConnection`。
3. 同时建立普通信令 WebSocket，用于成员事件、聊天、权限事件、媒体状态和 E2EE 密钥分发。
4. 使用 `VideoGrid` 渲染本地流、远端流和屏幕共享流。
5. 使用 `MemberList` 根据 `myPermissions` 渲染踢人、禁言、管理员等操作。
6. 使用 `Chat` 发送和接收群聊消息。

### `VideoGrid.tsx`

负责按成员和 `remoteStreams` 映射渲染媒体流。远端流归属由 SFU 下发的 `ssrc_mapping` 和 `SFUConnection` 的 pending track 归属逻辑共同完成。

### `Chat.tsx`

负责群聊 UI、消息发送、加密消息解密显示和安全指纹弹窗。发送消息时：

- 如果 E2EE ready，先用 `groupE2EEManager.encryptMessage()` 加密正文，再把密文 JSON 作为消息内容提交给后端。
- 后端保存 `encrypted` 标记并通过普通信令广播消息。
- 收到加密消息后，前端按 sender epoch 异步解密并更新 UI。
- 成员加入、离开或被踢后，前端轮换自己的 Sender Chain epoch 并向当前成员重发 Sender Key。

## 双 WebSocket 架构

```text
浏览器
├── 普通信令 WebSocket: API Server /ws?token=<app_jwt>
│   ├── join_room / leave_room
│   ├── chat_message
│   ├── public_key / sender_key / key_rotation
│   ├── media_state
│   └── muted / role_changed / kicked
└── SFU WebSocket: SFU Server /ws?token=<sfu_token>
    ├── join
    ├── offer / answer
    ├── ice-candidate
    └── ssrc_mapping
```

普通信令和媒体信令分离：普通信令使用 app JWT，SFU 媒体连接使用 API 在成功加入房间后签发的短期 `sfu_token`。

## WebRTC / SFU 前端流程

`frontend/src/utils/webrtc.ts` 中的 `SFUConnection` 封装了媒体连接：

1. 创建 `RTCPeerConnection`。
2. 添加本地音频/视频 track。
3. 连接 SFU WebSocket `/ws?token=<sfu_token>`。
4. 发送 `join`，收到 `joined` 后创建 offer。
5. 处理 SFU answer。
6. 处理 SFU 发起的 renegotiation offer。
7. 收到远端 track 后按 SSRC mapping 归属到用户。
8. 支持断线后刷新 `sfu_token` 并重建旧 WebSocket/PeerConnection。
9. 支持可选媒体 E2EE：在支持 `RTCRtpScriptTransform` 的浏览器中为 sender/receiver 安装 encoded frame transform，Worker 线程收集加密/解密帧计数、失败计数等诊断指标并通过 `postMessage` 回传主线程，主线程据此更新 UI 加密状态（enabled/negotiating/warning/unsupported/disabled）。

已支持的媒体能力：

- 麦克风开关。
- 摄像头开关。
- 屏幕共享作为独立视频 track 发布，不替换摄像头 track。
- SFU renegotiation。
- SSRC 到 userId 的远端流归属。
- ICE/WebSocket 失败后的有限重连。
- 可选媒体 E2EE 高安全模式；浏览器不支持 Encoded Transform 时回退到 DTLS-SRTP。

## 权限展示

前端不自行决定真实权限，只根据后端 `joinRoom` 返回的 `my_permissions` 渲染按钮。真实权限边界由后端 API、普通信令 Hub 和 SFU token 校验共同保证。

## 当前仍可改进的前端事项

- `MeetingRoom.tsx` 仍然承担较多职责，可后续拆为 `useMeetingRoom`、`useMediaConnection`、`useRoomSignaling` 等 hooks。
- E2EE 文本消息已具备基本密钥交换、成员变更 Sender Chain 轮换和加解密链路，但还缺少消息重放防护、设备身份验证和完整安全审计。
- 生产构建存在 Vite chunk size 警告，可通过路由懒加载或 manual chunks 优化。
- 媒体 E2EE 的浏览器支持限制、功能影响和验证步骤详见 `docs/E2EE_LIMITATIONS.md`。