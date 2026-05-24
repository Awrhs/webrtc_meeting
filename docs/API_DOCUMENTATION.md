# API 接口文档

## 基础信息

- Base URL: `/api/v1`
- 认证方式: `Authorization: Bearer <app_jwt>`
- 请求/响应格式: JSON
- 普通信令 WebSocket: `/ws?token=<app_jwt>`
- SFU 媒体 WebSocket: `SFU_SERVER/ws?token=<sfu_token>`

## 通用错误响应

```json
{
  "error": "permission denied"
}
```

## 认证接口

### 注册

`POST /auth/register`

```json
{
  "username": "user001",
  "email": "user@example.com",
  "phone": "13800138000",
  "password": "password123"
}
```

响应：

```json
{
  "message": "registration successful",
  "user": {}
}
```

### 登录

`POST /auth/login`

```json
{
  "account": "user001",
  "password": "password123",
  "remember": true
}
```

响应：

```json
{
  "token": "APP_JWT",
  "user": {}
}
```

## 用户接口

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/user/me` | 获取当前用户信息 |
| `PUT` | `/user/profile` | 更新昵称、头像等资料 |
| `PUT` | `/user/password` | 修改密码 |

## 房间接口

### 创建房间

`POST /rooms`

```json
{
  "name": "会议室",
  "topic": "周会",
  "max_members": 50,
  "is_permanent": false,
  "auth_type": "none",
  "password": "仅 auth_type=password 时需要"
}
```

响应：

```json
{
  "room": {
    "id": 1,
    "room_id": "123456",
    "name": "会议室",
    "owner_id": 1,
    "max_members": 50,
    "is_locked": false,
    "auth_type": "none",
    "invite_code": "abcd1234",
    "current_size": 1
  }
}
```

### 获取我的房间

`GET /rooms`

响应：

```json
{
  "rooms": []
}
```

### 获取房间详情

`GET /rooms/:id`

要求当前用户是该房间 active member。响应包含房间信息和成员列表。

```json
{
  "room": {},
  "members": [
    {
      "id": 1,
      "user_id": 1,
      "nickname": "用户昵称",
      "avatar": "",
      "role": "owner",
      "is_muted": false,
      "is_video_on": false,
      "is_audio_on": true
    }
  ]
}
```

### 按 6 位房间号查询

`GET /rooms/number/:number`

用于加入房间前的发现流程，只返回基本信息，不返回成员列表和完整敏感字段。

```json
{
  "room": {
    "id": 1,
    "room_id": "123456",
    "name": "会议室",
    "topic": "周会",
    "max_members": 50,
    "current_size": 1,
    "is_locked": false,
    "auth_type": "password"
  }
}
```

### 加入房间

`POST /rooms/:id/join`

`:id` 可以是数据库 ID 或 6 位房间号。

```json
{
  "verify_code": "房间密码或邀请码"
}
```

响应：

```json
{
  "room": {},
  "members": [],
  "my_permissions": ["kick_member", "mute_member"],
  "sfu_token": "SFU_JOIN_TOKEN"
}
```

### 刷新 SFU token

`POST /rooms/:id/sfu-token`

要求当前用户是 active room member 且未处于踢出封禁期。

```json
{
  "sfu_token": "SFU_JOIN_TOKEN"
}
```

### 房间管理

| 方法 | 路径 | 权限 | 响应摘要 |
|------|------|------|----------|
| `DELETE` | `/rooms/:id` | 房主 | `room deleted` |
| `POST` | `/rooms/:id/lock` | 房主 | `is_locked: true` |
| `POST` | `/rooms/:id/unlock` | 房主 | `is_locked: false` |

## 成员管理接口

| 方法 | 路径 | 权限 | 说明 |
|------|------|------|------|
| `POST` | `/rooms/:id/members/:userId/kick` | 管理员/房主 | 管理员只能踢普通成员；房主可踢管理员和普通成员；会通知 SFU 断开媒体连接 |
| `POST` | `/rooms/:id/members/:userId/mute` | 管理员/房主 | 禁言并通知 SFU 停止转发目标音频 |
| `POST` | `/rooms/:id/members/:userId/unmute` | 管理员/房主 | 解除禁言 |
| `POST` | `/rooms/:id/members/:userId/admin` | 房主 | 设置管理员 |
| `DELETE` | `/rooms/:id/members/:userId/admin` | 房主 | 取消管理员 |
| `POST` | `/rooms/:id/members/:userId/transfer` | 房主 | 转让房主 |

## 聊天接口

### 获取房间群聊消息

`GET /rooms/:id/messages?limit=50&offset=0`

要求当前用户是 active room member。

```json
{
  "messages": [
    {
      "id": 1,
      "room_id": 1,
      "sender_id": 1,
      "sender_nickname": "用户昵称",
      "sender_avatar": "",
      "receiver_id": null,
      "type": "text",
      "content": "明文或密文 JSON",
      "encrypted": true,
      "created_at": "2026-05-24T00:00:00Z"
    }
  ]
}
```

### 发送消息

`POST /rooms/:id/messages`

```json
{
  "receiver_id": null,
  "type": "text",
  "content": "明文或前端加密后的密文 JSON",
  "encrypted": true
}
```

- `receiver_id=null` 表示群聊。
- `receiver_id` 非空表示私聊，目标用户必须是同一房间 active member。
- 如果 `encrypted=true`，后端只保存和广播密文，不做解密。

响应：

```json
{
  "message": {
    "id": 1,
    "room_id": 1,
    "sender_id": 1,
    "receiver_id": null,
    "type": "text",
    "content": "...",
    "encrypted": true,
    "created_at": "2026-05-24T00:00:00Z"
  }
}
```

## 媒体状态接口

`PUT /rooms/:id/media-state`

要求当前用户是 active room member。请求中至少提供一个字段。

```json
{
  "video_enabled": true,
  "audio_enabled": false
}
```

响应：

```json
{
  "message": "media state updated",
  "video_enabled": true,
  "audio_enabled": false
}
```

## E2EE 公钥接口

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/e2ee/public-key` | 上传当前用户公钥 |
| `GET` | `/e2ee/public-key/:user_id` | 查询用户公钥 |
| `DELETE` | `/e2ee/public-key` | 删除当前用户公钥 |

> 当前会议内的即时公钥和 Sender Key 分发主要通过普通信令 WebSocket 的 `public_key` / `sender_key` 完成。

## 普通信令 WebSocket

连接：

```text
GET /ws?token=<app_jwt>
```

通用消息：

```json
{
  "type": "join_room",
  "room_id": 1,
  "target_id": 2,
  "data": {}
}
```

服务端以 JWT 中的用户身份为准，不信任客户端传入的 `user_id`。

### 客户端可发送类型

| 类型 | 说明 |
|------|------|
| `join_room` | 加入普通信令房间；必须已经是 active room member |
| `leave_room` | 离开普通信令房间 |
| `offer` / `answer` / `ice_candidate` | 普通信令透传，保留给非 SFU 或辅助协商场景 |
| `media_state` | 媒体状态信令，当前主要由 HTTP API 更新后服务端广播 |
| `public_key` | E2EE 身份公钥广播 |
| `sender_key` | E2EE Sender Key 单播，需带 `target_id`；`data.key_type` 缺省为聊天，`media` 表示媒体 E2EE key；payload 内包含对应 sender epoch |
| `key_rotation` | E2EE 成员变更密钥轮换通知，不携带明文或密钥材料 |

### 服务端广播类型

| 类型 | 说明 |
|------|------|
| `user_joined` | 用户加入 |
| `user_left` | 用户离开 |
| `kicked` | 被踢出 |
| `muted` | 禁言状态变化 |
| `role_changed` | 角色变化 |
| `media_state` | 媒体状态变化，payload 使用 `video_enabled/audio_enabled` |
| `chat_message` | 聊天消息 |
| `connection_failed` | SFU 回调通知 ICE 连接失败 |
| `public_key` | 房间成员公钥 |
| `sender_key` | 目标成员的 Sender Key |
| `key_rotation` | 成员变更后的 E2EE epoch 轮换通知 |

## SFU 媒体 WebSocket

连接：

```text
GET <SFU_HOST>/ws?token=<sfu_token>
```

`sfu_token` 只能由 API 在用户成功加入房间后签发。SFU 校验 token 的签名、过期时间、`purpose=sfu_join`、`room_id` 和 `user_id`。

消息类型：

| 类型 | 方向 | 说明 |
|------|------|------|
| `join` | 浏览器 → SFU | 加入 SFU 房间，实际 room/user 以 token claims 为准 |
| `joined` | SFU → 浏览器 | 加入成功 |
| `offer` | 双向 | 初次协商由浏览器发给 SFU；renegotiation 可由 SFU 发给浏览器 |
| `answer` | 双向 | 对 offer 的应答 |
| `ice-candidate` | 双向 | ICE 候选 |
| `ssrc_mapping` | SFU → 浏览器 | SSRC 到 userId/streamId 的映射 |
| `error` | SFU → 浏览器 | 错误信息 |

## 角色权限摘要

| 操作 | 普通成员 | 管理员 | 房主 |
|------|----------|--------|------|
| 加入/离开房间 | 是 | 是 | 是 |
| 音视频、屏幕共享 | 是 | 是 | 是 |
| 群聊消息 | 是 | 是 | 是 |
| 踢出成员 | 否 | 仅普通成员 | 管理员和普通成员 |
| 禁言/取消禁言 | 否 | 是 | 是 |
| 设置/取消管理员 | 否 | 否 | 是 |
| 转让房主 | 否 | 否 | 是 |
| 锁定/解锁房间 | 否 | 否 | 是 |
| 解散房间 | 否 | 否 | 是 |