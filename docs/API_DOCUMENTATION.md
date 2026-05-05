# API 接口文档

本项目基于 RESTful API 设计，使用 Bearer Token 进行身份验证。所有请求均使用 `application/json` 格式。

## 基础信息

- **Base URL**: `/api/v1`
- **认证方式**: HTTP Header 中添加 `Authorization: Bearer <token>`
- **响应格式**: JSON

## 通用响应

### 成功响应
```json
{
  "data": {},
  "message": "操作成功"
}
```

### 错误响应
```json
{
  "error": "错误信息描述"
}
```

## 认证接口 (Auth)

### 1. 用户注册

**POST** `/auth/register`

**请求体**:
```json
{
  "username": "用户名 (3-20字符)",
  "email": "邮箱地址",
  "phone": "手机号",
  "password": "密码 (8-20字符)"
}
```

**响应**:
```json
{
  "message": "registration successful",
  "user": {
    "id": 1,
    "username": "username",
    "nickname": "nickname",
    "email": "email@example.com",
    "phone": "13800138000",
    "avatar": "avatar_url"
  }
}
```

### 2. 用户登录

**POST** `/auth/login`

**请求体**:
```json
{
  "account": "用户名/邮箱/手机号",
  "password": "密码",
  "remember": true
}
```

**响应**:
```json
{
  "token": "JWT_TOKEN",
  "user": {
    "id": 1,
    "username": "username",
    "nickname": "nickname",
    "email": "email@example.com",
    "phone": "13800138000",
    "avatar": "avatar_url"
  }
}
```

---

## 用户接口 (User)

> 需要认证

### 1. 获取当前用户信息

**GET** `/user/me`

**响应**:
```json
{
  "user": {
    "id": 1,
    "username": "username",
    "nickname": "nickname",
    "email": "email@example.com",
    "phone": "13800138000",
    "avatar": "avatar_url",
    "status": "active"
  }
}
```

### 2. 更新用户资料

**PUT** `/user/profile`

**请求体**:
```json
{
  "nickname": "新昵称 (2-10字符)",
  "avatar": "头像URL"
}
```

**响应**:
```json
{
  "message": "profile updated"
}
```

### 3. 更新密码

**PUT** `/user/password`

**请求体**:
```json
{
  "old_password": "原密码",
  "new_password": "新密码 (8-20字符)"
}
```

**响应**:
```json
{
  "message": "password updated"
}
```

---

## 房间接口 (Room)

> 需要认证

### 1. 创建房间

**POST** `/rooms`

**请求体**:
```json
{
  "name": "房间名称 (2-20字符)",
  "topic": "房间主题",
  "max_members": 50,
  "is_permanent": false,
  "auth_type": "none | password | invite_code",
  "password": "房间密码 (当 auth_type 为 password 时)"
}
```

**响应**:
```json
{
  "room": {
    "id": 1,
    "room_id": "123456",
    "name": "房间名称",
    "topic": "房间主题",
    "owner_id": 1,
    "max_members": 50,
    "is_permanent": false,
    "is_locked": false,
    "auth_type": "none",
    "invite_code": "abc123",
    "current_size": 1,
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-01T00:00:00Z"
  }
}
```

### 2. 获取我的房间列表

**GET** `/rooms`

**响应**:
```json
{
  "rooms": [
    {
      "id": 1,
      "room_id": "123456",
      "name": "房间名称",
      "topic": "房间主题",
      "owner_id": 1,
      "max_members": 50,
      "is_permanent": false,
      "is_locked": false,
      "auth_type": "none",
      "current_size": 3,
      "created_at": "2024-01-01T00:00:00Z"
    }
  ]
}
```

### 3. 获取房间详情（按数据库ID）

**GET** `/rooms/:id`

**响应**:
```json
{
  "room": {
    "id": 1,
    "room_id": "123456",
    "name": "房间名称",
    "topic": "房间主题",
    "owner_id": 1,
    "max_members": 50,
    "is_permanent": false,
    "is_locked": false,
    "auth_type": "none",
    "current_size": 3
  },
  "members": [
    {
      "id": 1,
      "user_id": 1,
      "nickname": "用户昵称",
      "avatar": "头像URL",
      "role": "owner | admin | member",
      "is_muted": false,
      "is_video_on": false,
      "is_audio_on": true
    }
  ]
}
```

### 4. 获取房间详情（按6位房间号）

**GET** `/rooms/number/:number`

**响应**: 同上

### 5. 加入房间

**POST** `/rooms/:id/join`

> 注意：`:id` 可以是数据库ID或6位房间ID

**请求体** (可选):
```json
{
  "verify_code": "密码或邀请码"
}
```

**响应**:
```json
{
  "room": {
    "id": 1,
    "room_id": "123456",
    "name": "房间名称"
  },
  "members": [
    {
      "id": 1,
      "user_id": 1,
      "nickname": "用户昵称",
      "avatar": "头像URL",
      "role": "owner",
      "is_muted": false,
      "is_video_on": false,
      "is_audio_on": true
    }
  ]
}
```

### 6. 删除/解散房间

**DELETE** `/rooms/:id`

> **权限要求**: 房主 (owner)

**响应**:
```json
{
  "message": "room deleted"
}
```

### 7. 锁定房间

**POST** `/rooms/:id/lock`

> **权限要求**: 房主 (owner)

**响应**:
```json
{
  "message": "room locked",
  "is_locked": true
}
```

### 8. 解锁房间

**POST** `/rooms/:id/unlock`

> **权限要求**: 房主 (owner)

**响应**:
```json
{
  "message": "room unlocked",
  "is_locked": false
}
```

---

## 房间成员接口 (Room Members)

> 需要认证

### 1. 踢出成员

**POST** `/rooms/:id/members/:userId/kick`

> **权限要求**: 管理员或房主 (admin/owner)
> 管理员不能踢出房主或其他管理员

**响应**:
```json
{
  "message": "member kicked"
}
```

### 2. 禁言成员

**POST** `/rooms/:id/members/:userId/mute`

> **权限要求**: 管理员或房主 (admin/owner)

**响应**:
```json
{
  "message": "member muted",
  "is_muted": true
}
```

### 3. 取消禁言

**POST** `/rooms/:id/members/:userId/unmute`

> **权限要求**: 管理员或房主 (admin/owner)

**响应**:
```json
{
  "message": "member unmuted",
  "is_muted": false
}
```

### 4. 设置管理员

**POST** `/rooms/:id/members/:userId/admin`

> **权限要求**: 房主 (owner)

**响应**:
```json
{
  "message": "admin set",
  "role": "admin"
}
```

### 5. 取消管理员

**DELETE** `/rooms/:id/members/:userId/admin`

> **权限要求**: 房主 (owner)

**响应**:
```json
{
  "message": "admin removed",
  "role": "member"
}
```

### 6. 转让房主

**POST** `/rooms/:id/members/:userId/transfer`

> **权限要求**: 房主 (owner)

**响应**:
```json
{
  "message": "ownership transferred"
}
```

---

## 聊天接口 (Chat)

> 需要认证

### 1. 获取房间消息

**GET** `/rooms/:id/messages`

**响应**:
```json
{
  "messages": [
    {
      "id": 1,
      "room_id": 1,
      "sender_id": 1,
      "receiver_id": null,
      "content": "消息内容",
      "type": "text | image | file",
      "created_at": "2024-01-01T00:00:00Z"
    }
  ]
}
```

### 2. 发送消息

**POST** `/rooms/:id/messages`

**请求体**:
```json
{
  "receiver_id": null,
  "content": "消息内容",
  "type": "text"
}
```

> 当 `receiver_id` 不为 null 时为私聊消息

**响应**:
```json
{
  "id": 1,
  "room_id": 1,
  "sender_id": 1,
  "receiver_id": null,
  "content": "消息内容",
  "type": "text",
  "created_at": "2024-01-01T00:00:00Z"
}
```

---

## 媒体状态接口

### 更新媒体状态

**PUT** `/rooms/:id/media-state`

**请求体**:
```json
{
  "video_enabled": true,
  "audio_enabled": false
}
```

**响应**:
```json
{
  "message": "media state updated"
}
```

---

## WebSocket 信令接口

### 连接 WebSocket

```
GET /ws?token=<JWT_TOKEN>
```

### 消息格式

所有消息均使用 JSON 格式，通用结构：
```json
{
  "type": "消息类型",
  "room_id": 1,
  "user_id": 1,
  "target_id": 0,
  "data": {}
}
```

### 客户端发送消息类型

| 类型 | 说明 | 数据结构 |
|------|------|----------|
| `join_room` | 加入房间 | `{ "room_id": 1 }` |
| `leave_room` | 离开房间 | `{ "room_id": 1 }` |
| `offer` | WebRTC Offer SDP | `{ "room_id": 1, "target_id": 2, "data": { "sdp": "..." } }` |
| `answer` | WebRTC Answer SDP | `{ "room_id": 1, "target_id": 2, "data": { "sdp": "..." } }` |
| `ice_candidate` | ICE 候选 | `{ "room_id": 1, "target_id": 2, "data": { ... } }` |
| `media_state` | 媒体状态变化 | `{ "room_id": 1, "data": { "video_on": true, "audio_on": false } }` |

### 服务端广播消息类型

| 类型 | 说明 | 数据结构 |
|------|------|----------|
| `user_joined` | 用户加入房间 | `{ "room_id": 1, "user_id": 2 }` |
| `user_left` | 用户离开房间 | `{ "room_id": 1, "user_id": 2 }` |
| `kicked` | 被踢出房间 | `{ "room_id": 1, "target_id": 2 }` |
| `muted` | 禁言状态变化 | `{ "room_id": 1, "user_id": 2, "data": { "is_muted": true } }` |
| `role_changed` | 角色变化 | `{ "room_id": 1, "user_id": 2, "data": { "role": "admin" } }` |
| `media_state` | 媒体状态变化 | `{ "room_id": 1, "user_id": 2, "data": { "video_enabled": true, "audio_enabled": true } }` |
| `chat_message` | 聊天消息 | `{ "room_id": 1, "user_id": 2, "data": { ... } }` |
| `connection_failed` | 连接失败 | `{ "room_id": 1, "user_id": 2 }` |

---

## SFU 媒体接口

### WebRTC 连接建立

**POST** `/offer`

**请求体**:
```json
{
  "room_id": 1,
  "user_id": 1,
  "sdp": "v=0\r\no=- 1234567890 2 IN IP4 127.0.0.1..."
}
```

**响应**:
```json
{
  "sdp": "v=0\r\no=- 0987654321 2 IN IP4 127.0.0.1..."
}
```

---

## 角色权限说明

| 操作 | 普通成员 | 管理员 | 房主 |
|------|---------|--------|------|
| 加入/离开房间 | ✓ | ✓ | ✓ |
| 开启/关闭摄像头/麦克风 | ✓ | ✓ | ✓ |
| 发送公聊消息 | ✓ | ✓ | ✓ |
| 发送私聊消息 | ✓ | ✓ | ✓ |
| 踢出成员 | ✗ | ✓ (只能踢普通成员) | ✓ |
| 禁言/取消禁言 | ✗ | ✓ | ✓ |
| 设置/取消管理员 | ✗ | ✗ | ✓ |
| 转让房主 | ✗ | ✗ | ✓ |
| 锁定/解锁房间 | ✗ | ✗ | ✓ |
| 解散房间 | ✗ | ✗ | ✓ |
