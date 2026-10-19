# 跨平台加密SFU音视频会议软件

基于Pion WebRTC的企业级音视频会议系统，采用SFU架构支持多人会议。

## 📋 技术栈

### 后端
- **控制面**: Go + Gin 框架
- **媒体面**: Pion WebRTC (纯Go SFU实现)
- **控制接口**: gRPC (API服务 → SFU服务通信)
- **信令**: gorilla/websocket
- **存储**: PostgreSQL + Redis
- **认证**: JWT

### 前端
- **框架**: React 18 + TypeScript
- **构建**: Vite
- **UI**: Ant Design
- **状态管理**: Redux Toolkit
- **目标平台**: 浏览器端（Web）

### 基础设施
- **STUN/TURN**: Coturn
- **容器化**: Docker + Docker Compose
- **反向代理**: Nginx（可选，用于生产环境统一入口）

---

## 📐 前端架构详解

### 核心目录结构
```
frontend/src/
├── pages/
│   ├── MeetingRoom.tsx    # 会议主页面 ✅ 所有SFU功能入口
│   ├── RoomList.tsx       # 房间列表/创建/加入
│   ├── Login/Register.tsx # 认证页面
│   └── Profile.tsx        # 用户资料
├── components/
│   ├── VideoGrid.tsx      # 视频网格，远程流分配逻辑
│   ├── MemberList.tsx     # 成员列表 + 权限操作菜单
│   └── Chat.tsx           # 聊天消息组件
├── store/slices/
│   ├── authSlice.ts       # 认证状态
│   └── roomSlice.ts       # 房间/成员/消息状态 + async thunks
└── utils/
    ├── webrtc.ts          # ✅ SFUConnection 核心类（WebSocket+WebRTC）
    ├── signaling.ts       # 信令WebSocket客户端（聊天/控制/E2EE密钥分发）
    ├── api.ts             # Axios实例 + 拦截器
    └── encryption.ts      # 文本消息 E2EE 工具
```

### 双 WebSocket 架构设计
```
浏览器
  ├─ Signaling WebSocket (端口 8080) → API Server
  │    └─ 处理：聊天消息、成员列表变更、权限变更通知
  │    └─ 事件：user_joined / user_left / chat_message / muted / role_changed / media_state
  │
  └─ SFU WebSocket (端口 8081，/ws?token=<sfu_token>) → SFU Server
       └─ 处理：SDP协商、ICE候选交换、媒体流传输
       └─ 事件：joined / offer / answer / ice-candidate
```

SFU 媒体连接默认不再接受裸连。用户必须先通过 API 登录并加入房间，由 API 在完成房间密码、锁房、成员状态、踢出封禁等校验后签发短期 `sfu_token`，浏览器再用该 token 连接 SFU `/ws`。

### SFUConnection 类核心功能
| 方法/回调 | 作用 |
|----------|------|
| `connect(localStream)` | 建立SFU WebSocket + 创建PeerConnection；重连后自动重新发布晚于入会的摄像头轨道 |
| `disconnect()` | 安全断开，清理所有资源（定时器/流/连接） |
| `muteAudio(isMuted)` | 基于PeerConnection实际发送轨道启停本地音频 |
| `muteVideo(isMuted)` | 基于PeerConnection实际发送轨道启停本地视频 |
| `replaceVideoTrack(newTrack, stream)` | 切换摄像头 ↔ 屏幕共享；首次开摄像头时若不存在视频 sender 会新建两层 simulcast transceiver 发布 |
| `onRemoteTrack(userId, track, stream)` | 远程流回调（优先按 msid 流ID归属用户，SSRC 为兜底），触发UI刷新 |
| `scheduleReconnect()` | 连接失败自动重试（最多5次），重连时保留本地媒体并重新同步视频订阅 |

### 状态管理数据流
```
用户操作 → Redux Thunk → API Request → Response → 更新 Store
                                          ↓
                                      触发组件重渲染
                                          ↓
                                    VideoGrid 显示流
                                    MemberList 显示状态
                                    Chat 显示新消息
```

## ✨ 功能特性

### ✅ 已实现并可正常工作
- **用户系统** - 手机号/邮箱注册登录，bcrypt加密存储，个人资料编辑
- **房间管理** - 创建/删除/加入房间，密码/邀请码验证，房主可锁房/解散房间
- **细粒度权限管理 (PBAC)** - 房主(全部权限) → 管理员(kick/禁言) → 普通成员
  - Permission枚举：kick_member, mute_member, set_admin, lock_room等
  - 加入房间返回myPermissions列表，前端动态渲染功能按钮
- **WebSocket信令连接** - 基于Hub的房间管理，消息广播/单播，断线重连机制
- **聊天消息** - 公聊文本消息，支持数据库持久化和WebSocket实时推送
- **审计日志系统** - 敏感操作记录，支持追溯
- **Redis热点缓存** - 用户/房间信息缓存，降低数据库压力
- **gRPC SFU控制通道** - 踢人/静音等管理操作通过gRPC调用，并使用共享控制 token 鉴权
- **SFU 入会鉴权** - API 签发短期 `sfu_token`，SFU 校验 `purpose=sfu_join`、`room_id`、`user_id` 后才允许媒体连接
- **安全与运维基础** - 支持 Allowed Origins、WebSocket Origin 校验、基础限流、`/healthz`、`/readyz`、`/metrics.json` 和优雅关闭
- **SFU音视频通信核心** - WebRTC 音视频会议主链路（前端 `SFUConnection` 类封装）
  - ✅ ICE候选处理 - 通过 SFU WebSocket 交换候选，支持NAT穿透，含有限自动重连
  - ✅ SDP Offer/Answer协商 - 支持初次协商和 SFU 发起的 renegotiation（重协商 offer 现携带已收集完成的 ICE 候选）
  - ✅ 媒体流转发 - Pion WebRTC SFU转发音视频流，onTrack回调触发远端渲染
  - ✅ 多轨支持 - 摄像头和屏幕共享可作为独立视频轨道同时发布
  - ✅ 远端流归属 - SFU 下发流映射消息，前端将远端流归属到对应用户。**以 msid（媒体流ID）为可靠依据**：Pion 转发时会给每个订阅者重写 SSRC，SFU 下发的发布者原始 SSRC 与订阅端收到的 SSRC 对不上，而 msid 被 SFU 原样保留，浏览器 `event.streams[0].id` 即发布者的流ID
  - ✅ 订阅清理 - 用户离开或被踢后清理其他 PeerConnection 中的旧 sender 并重新协商
- **文本消息 E2EE 基础链路** - 群聊文本支持前端加密、密钥信令分发、后端密文持久化和接收端解密显示，并接入基于滑动窗口的消息重放防护
- **SFU管理功能** - 通过gRPC强制踢人/静音，通过信令广播通知所有客户端
- **选择性订阅与 Simulcast** - 视频按需订阅（音频/无 RID 视频流（屏幕共享、非 simulcast 回退）自动全量订阅），摄像头视频默认两层 Simulcast 编码且不自动扇出，订阅请求由前端按成员媒体状态（前 6 路开视频用户）动态下发，重连后自动重同步；订阅端可按 quality 选层（协议已支持，前端暂无切换 UI，默认 high）
- **SFU token 防重放与登录设备记录** - SFU admission token 内嵌 jti 并通过 Redis 标记，登录设备会被指纹化记录并提供 API 管理
- **房间审计 API** - kick/mute/admin/transfer/lock/delete 等敏感操作落库，owner/admin 通过 `/rooms/:id/audit-logs` 分页查询


## 🏗️ 项目架构

### 整体架构
```
┌─────────────────────────────────────────────────────────────┐
│                     浏览器端 (Web)                            │
│  ┌─────────┐ ┌─────────┐ ┌─────────┐                        │
│  │ React UI│ │WebRTC采集│ │加密工具│                        │
│  └─────────┘ └─────────┘ └─────────┘                        │
└─────────────────────────────────────────────────────────────┘
                              │ HTTP / WebSocket
                              ▼
┌─────────────────────────────────────────────────────────────┐
│              API/信令服务器 (Go + Gin)                        │
│  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐          │
│  │ 用户认证 │ │ 房间管理 │ │ 权限控制 │ │ 信令Hub │          │
│  └─────────┘ └─────────┘ └─────────┘ └─────────┘          │
└─────────────────────────────────────────────────────────────┘
                              │ gRPC (控制面) / HTTP (媒体面)
                              ▼
┌─────────────────────────────────────────────────────────────┐
│              SFU 媒体服务器 (Pion WebRTC)                      │
│  SFU选择转发模型：每个用户上行一路流，SFU转发给订阅的用户        │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│                     存储层                                    │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐       │
│  │ PostgreSQL  │  │    Redis     │  │   Coturn    │       │
│  │ 持久化存储  │  │   在线状态   │  │ STUN/TURN   │       │
│  │ 用户/房间/消息│  │  房间成员   │  │  NAT穿透   │       │
│  └──────────────┘  └──────────────┘  └──────────────┘       │
└─────────────────────────────────────────────────────────────┘
```

## 📁 项目结构

```
conference-sfu/
├── backend/
│   ├── cmd/
│   │   ├── server/       # API/信令服务器入口
│   │   └── sfu/          # SFU媒体服务器入口（独立进程）
│   ├── internal/
│   │   ├── models/       # 数据模型定义
│   │   ├── handlers/     # HTTP API处理器
│   │   ├── service/      # 业务逻辑层
│   │   ├── repository/   # 数据库操作层
│   │   ├── sfu/          # SFU媒体服务 + gRPC服务
│   │   ├── signaling/    # WebSocket信令 Hub
│   │   └── middleware/   # 中间件
│   └── pkg/
│       ├── crypto/       # AES加密工具
│       └── logger/       # 日志封装
├── frontend/
│   ├── src/
│   │   ├── pages/        # 页面组件
│   │   ├── components/   # 可复用组件
│   │   ├── store/        # Redux状态管理
│   │   └── utils/        # 工具函数(webrtc/signaling/api)
│   └── public/
│       ├── sfu-test.html           # 旧版 SFU 直连调试页，仅开发逃生阀可用
│       └── sfu-test-no-camera.html # 旧版无摄像头调试页，仅开发逃生阀可用
├── deployment/
│   ├── docker-compose.yml  # Docker 服务编排
│   ├── nginx.conf          # Nginx 反向代理配置（生产环境用）
│   └── docker/             # Dockerfile 目录
│       ├── Dockerfile.frontend
│       ├── Dockerfile.server
│       └── Dockerfile.sfu
└── README.md
```

## 📐 后端架构详解

### 分层架构说明

后端采用经典的分层架构，职责边界清晰，便于独立测试和维护：

```
┌─────────────────────────────────────────────────────────────┐
│  入口层 (cmd/)          │  服务启动、依赖注入、HTTP/gRPC监听   │
├─────────────────────────────────────────────────────────────┤
│  处理器层 (handlers/)   │  HTTP请求解析、参数校验、响应封装    │
│                         │  JWT认证中间件                       │
├─────────────────────────────────────────────────────────────┤
│  服务层 (service/)      │  业务规则编排、权限检查、事务协调    │
│                         │  Redis缓存读写策略                   │
├─────────────────────────────────────────────────────────────┤
│  数据访问层 (repository/)│  SQL/Redis原始操作、事务管理         │
├─────────────────────────────────────────────────────────────┤
│  模型层 (models/)       │  结构体定义、GORM标签、DTO/请求校验  │
├─────────────────────────────────────────────────────────────┤
│  基础设施层 (pkg/)      │  日志、加密等横向工具                │
└─────────────────────────────────────────────────────────────┘
```

### 各模块职责与应实现逻辑

#### 1. 入口层 `cmd/`

| 文件 | 职责 | 应实现的具体逻辑 |
|------|------|-----------------|
| `cmd/server/main.go` | API/信令服务器入口 | ① 加载配置 `config.Load()`；② 初始化 PostgreSQL（GORM）和 Redis；③ 按依赖顺序实例化 Repository → Service → Handler → SignalingHub → SFU gRPC Client；④ 注册 Gin HTTP 路由（REST API + WebSocket `/ws`）；⑤ 启动 HTTP 服务器监听 `SERVER_PORT` |
| `cmd/sfu/main.go` | SFU 媒体服务器入口 | ① 加载配置；② 创建 `SFUServer`（含 Pion WebRTC SettingEngine）；③ 启动 gRPC 控制服务器（供 API 服务调用）；④ 启动 WebSocket HTTP 服务器监听 `SFU_PORT`，处理 `/ws` 信令 |

#### 2. 配置层 `internal/config/`

| 文件 | 职责 | 应实现的具体逻辑 |
|------|------|-----------------|
| `config.go` | 统一配置加载 | ① 非生产环境自动加载 `.env` 文件；② 从环境变量读取数据库、Redis、JWT、端口、TURN 等参数；③ 构建 PostgreSQL DSN 字符串；④ 提供默认值 fallback |

#### 3. 模型层 `internal/models/`

| 文件 | 职责 | 应实现的具体逻辑 |
|------|------|-----------------|
| `user.go` | 用户实体与认证 DTO | ① `User` 结构体（GORM 标签：`username/email/phone` 唯一索引，`password` 不序列化到 JSON）；② `Device` 设备管理；③ `RegisterRequest`/`LoginRequest` 等请求结构体（含 Gin binding 校验标签） |
| `room.go` | 房间实体与权限系统 | ① `Room` 结构体（`room_id` 为 6 位数字唯一索引，`password` 不序列化）；② `RoomMember` 结构体（`left_at` 软删除支持）；③ `Role` 枚举（owner/admin/member）；④ `Permission` 枚举 + `RolePermissions` 权限矩阵；⑤ `KickBan` 踢出封禁记录；⑥ `AuditLog` 审计日志 |
| `chat.go` | 消息实体与 E2EE DTO | ① `Message` 结构体（`encrypted` 标记端到端加密消息，`receiver_id` 为 nil 表示群聊）；② `MessageType` 枚举；③ `E2EEPublicKey` 公钥存储模型；④ E2EE 相关请求/响应 DTO |

#### 4. 处理器层 `internal/handlers/`

| 文件 | 职责 | 应实现的具体逻辑 |
|------|------|-----------------|
| `auth.go` | 用户认证 HTTP API | ① `Register`：解析请求 → 调用 `userService.Register` → 返回用户信息；② `Login`：解析账号密码 → 调用 `userService.Login` → 返回 JWT Token + 用户信息；③ `GetMe`/`UpdateProfile`/`UpdatePassword`：CRUD 当前用户；④ `AuthMiddleware`：提取 Bearer Token → 解析 JWT → 将 `user_id` 写入 Gin Context；⑤ `ValidateToken`：JWT 签名验证与 claims 解析 |
| `room.go` | 房间管理 HTTP API | ① `CreateRoom`/`GetRoom`/`GetMyRooms`：房间 CRUD；② `JoinRoom`：加入逻辑 + 广播 `user_joined` 事件 + 返回成员列表、权限和短期 `sfu_token`；③ `DeleteRoom`/`LockRoom`/`UnlockRoom`：房主操作（权限检查）；④ `KickMember`/`MuteMember`/`UnmuteMember`：管理员操作 + 调用 SFU gRPC `KickUser`/`MuteUser` + 广播信令事件；⑤ `SetAdmin`/`RemoveAdmin`/`TransferOwner`：权限变更 + 广播 `role_changed`；⑥ `UpdateMediaState`：校验成员身份、持久化媒体状态并广播 `video_enabled/audio_enabled`；⑦ `NotifyConnectionFailed`：SFU 回调接口，通知某用户 ICE 连接失败 |
| `chat.go` | 聊天消息 HTTP API | ① `GetMessages`：分页查询房间历史消息；② `SendMessage`：持久化消息 + 通过 Hub 广播（私聊则单播）；③ `UploadPublicKey`/`GetPublicKey`/`DeletePublicKey`：E2EE 公钥管理 |

#### 5. 服务层 `internal/service/`

| 文件 | 职责 | 应实现的具体逻辑 |
|------|------|-----------------|
| `user.go` | 用户业务逻辑 | ① `Register`：唯一性检查（email/phone/username）→ bcrypt 密码哈希 → 创建用户；② `Login`：账号类型自动识别（手机号/邮箱/用户名）→ bcrypt 密码比对 → 生成 JWT → Redis 标记在线；③ `GetUserByID`：先查 Redis 缓存，miss 再查 DB 并回写缓存；④ `UpdateProfile`/`UpdatePassword`：更新后清除 Redis 缓存 |
| `room.go` | 房间业务逻辑（核心） | ① `CreateRoom`：生成 6 位 `room_id` 和 8 位邀请码 → 密码 bcrypt 哈希 → 事务创建房间 + 添加房主成员 → Redis 缓存；② `JoinRoom`：按 `room_id` 或主键查找 → 检查房间锁定/踢出封禁/人数上限/密码或邀请码验证 → 软删除恢复或新成员记录 → 更新房间人数 → 清除缓存；③ `KickMember`：权限检查（不能踢房主/同级管理员）→ 软删除成员 → 创建 `KickBan`（10 分钟有效期）→ 事务提交 → 清除缓存；④ `MuteMember`：权限检查 → 更新 `RoomMember.IsMuted`；⑤ `SetAdmin`/`TransferOwner`：仅房主可操作 → 事务更新角色；⑥ `CheckPermission`/`HasPermission`：基于 `RolePermissions` 矩阵的细粒度权限检查；⑦ `UpdateMediaState`：更新 `IsVideoOn`/`IsAudioOn`；⑧ 所有写操作后联动 Redis 缓存失效 |
| `chat.go` | 聊天业务逻辑 | ① `SendMessage`：校验发送者是 active room member，私聊时校验 receiver 同房间，保存前端传入的 `Encrypted` 标记并持久化；② `GetMessages`：校验成员身份后分页查询并转换为 `MessageResponse`；③ `SavePublicKey`/`GetPublicKey`/`DeletePublicKey`：E2EE 公钥的 CRUD |

#### 6. 数据访问层 `internal/repository/`

| 文件 | 职责 | 应实现的具体逻辑 |
|------|------|-----------------|
| `user.go` | 用户数据访问 + Redis 缓存 | ① `UserRepository`：User CRUD（`FindByEmail`/`FindByPhone`/`FindByUsername`/`FindByID`）；② `RedisClient`：在线状态（`SetOnline`/`GetOnline`）、房间成员集合（`AddToRoom`/`RemoveFromRoom`/`GetRoomMembers`）、用户缓存（`SetUser`/`GetUser`/`DeleteUser`）、房间缓存（`SetRoom`/`GetRoom`/`DeleteRoom`）、用户房间列表缓存（`SetUserRooms`/`GetUserRooms`/`DeleteUserRooms`） |
| `room.go` | 房间数据访问 | ① `RoomRepository`：Room CRUD；② 成员管理：`AddMember`/`UpdateMember`/`GetMembers`/`GetMember`（仅未离开）/`GetAnyMember`（含已离开）/`RemoveMember`（软删除：更新 `left_at`）/`CountMembers`；③ 踢出封禁：`CreateKickBan`/`IsKicked`/`RemoveExpiredKicks`；④ 审计日志：`CreateAuditLog`/`GetAuditLogs`；⑤ 事务支持：`Begin()` 返回新 Repository（含 `*gorm.DB` 事务实例）/`Commit()`/`Rollback()` |
| `chat.go` | 聊天数据访问 | ① `CreateMessage`：插入消息；② `GetMessages`：按房间查询群聊消息（`receiver_id IS NULL`）并倒序分页，返回前 `reverseMessages` 恢复时间正序；③ `GetPrivateMessages`：双向查询私聊记录；④ `SavePublicKey`：upsert 逻辑（存在则更新，不存在则创建）；⑤ `GetPublicKey`/`DeletePublicKey` |

#### 7. SFU 核心层 `internal/sfu/`

| 文件 | 职责 | 应实现的具体逻辑 |
|------|------|-----------------|
| `server.go` | SFU 媒体引擎（最核心） | ① **数据结构**：`Room`、`Peer`、`TrackInfo`、订阅 sender 和 `addedTracks` 去重表；② **接入鉴权**：`HandleWebSocket` 校验 Origin 和短期 `sfu_token`，join 时以 token claims 为准；③ **WebRTC 协商**：创建 `PeerConnection`，处理初次 offer/answer、SFU renegotiation、ICE candidate 和流映射（`ssrc_mapping`）；④ **媒体转发**：`onTrack` 创建 `TrackLocalStaticRTP` 并转发 RTP；扇出规则由事件循环中的 `autoSubscribeTrack` 统一处理——音频与无 RID 视频（屏幕共享/非 simulcast 回退）自动 AddTrack+renegotiate，带 RID 的摄像头视频需前端 subscribe；⑤ **清理**：用户离开/被踢/连接失败时移除其他 peer 的订阅 sender、清理 addedTracks、RTCP 路由表并重新协商；⑥ **控制方法**：`KickUser`、`MuteUser`、`GetRoomStats`/`ListRooms` |
| `grpc_server.go` | gRPC 控制服务端 | ① `ControlServer` 实现 protobuf 生成的 `SFUControlServer` 接口；② 将 gRPC 请求参数转换为 `SFUServer` 方法调用：`KickUser`/`MuteUser`/`SetVideoEnabled`（占位）/`GetRoomStats`/`ListRooms`/`ClosePeerConnection`；③ `StartGRPCServer`：创建 TCP Listener → 注册 gRPC 服务 → 后台 goroutine 启动 |
| `grpc_client.go` | gRPC 控制客户端 | ① `ControlClient`：维护到 SFU gRPC 服务的 `grpc.ClientConn`；② 提供 `KickUser`/`MuteUser`/`ClosePeerConnection`/`GetRoomStats`/`ListRooms` 的同步调用封装（5 秒超时 + 错误日志） |

#### 8. 信令层 `internal/signaling/`

| 文件 | 职责 | 应实现的具体逻辑 |
|------|------|-----------------|
| `hub.go` | WebSocket 信令 Hub（广播中心） | ① **核心设计**：基于 Go channel 的无锁并发模型，`Run()` 是单 goroutine 事件循环，所有操作通过 channel 串行化；② **Client 管理**：`register`/`unregister`（全局客户端映射 `clients`）；③ **房间管理**：`addToRoom`/`removeFromRoom`（房间→客户端集合映射 `rooms`）；④ **消息分发**：`broadcast`（房间广播，支持 `ExcludeUserID`）、`sendToUser`（单播）；⑤ **业务广播方法**：`BroadcastUserJoined`/`BroadcastUserLeft`/`BroadcastKick`/`BroadcastMute`/`BroadcastRoleChanged`/`BroadcastMediaState`/`SendConnectionFailed`/`BroadcastMessage`；⑥ **Client 生命周期**：`ReadPump`（读取 WebSocket 消息 → 解析 JSON → 按类型分发：`join_room` 加房间、`leave_room` 离开、`offer/answer/ice_candidate` 转发给 TargetID 或广播、`media_state` 广播）、`WritePump`（从 `send` channel 写消息到 WebSocket） |

#### 9. 工具包 `pkg/`

| 文件 | 职责 | 应实现的具体逻辑 |
|------|------|-----------------|
| `logger/logger.go` | 结构化日志 | 基于 Zap 封装：生产环境用 `NewProductionConfig`（JSON 格式）+ ISO8601 时间编码；开发环境用 `NewDevelopmentConfig`（彩色控制台输出） |
| `crypto/aes.go` | AES-GCM 加密 | ① `Encrypt`：SHA256 派生 32 字节密钥 → AES-GCM 加密 → base64 编码（nonce 附加在密文前）；② `Decrypt`：base64 解码 → 分离 nonce → AES-GCM 解密；③ `GenerateKey`：生成随机 32 字节密钥 |

#### 10. API 定义 `api/gen/go/`

| 文件 | 职责 | 应实现的具体逻辑 |
|------|------|-----------------|
| `sfu_control.pb.go` | Protobuf 消息 | 由 `.proto` 文件通过 `protoc` 生成，定义 `KickUserRequest`/`MuteUserRequest`/`GetRoomStatsResponse` 等消息结构体 |
| `sfu_control_grpc.pb.go` | gRPC 服务接口 | 由 `.proto` 文件生成，定义 `SFUControl` 服务接口和客户端/服务端桩代码 |

### 后端核心数据流

```
浏览器 ──HTTP 登录/加入房间──→ API Server ──校验房间权限──→ 签发短期 sfu_token
   │
   ├──WebSocket──→ signaling.Hub ──广播──→ 同房间其他浏览器
   │                 └─ 纯控制信令（聊天/成员变更/权限变更）
   │
   └──WebSocket /ws?token=<sfu_token>──→ SFU Server ──WebRTC──→ 浏览器
          │
          ├─ 校验 purpose=sfu_join、room_id、user_id
          ├─ join/offer/answer/ice-candidate 信令
          ├─ RTP 媒体流接收与转发
          └─ gRPC ←── API Server (控制命令: kick/mute/stats，带控制 token)
```

## 🚀 快速开始

### 前置要求
- Go 1.21+
- Node.js 18+
- Docker + Docker Compose

### 使用 Docker Compose 启动

```bash
# 复制环境变量并按需修改 secret、数据库密码、TURN 凭据和 ALLOWED_ORIGINS
cp .env.example .env

# 检查 Compose 配置是否能正确展开
docker compose -f deployment/docker-compose.yml config

# 启动所有服务（包括数据库、Redis、Coturn、API、SFU、前端）
docker compose -f deployment/docker-compose.yml up -d --build
```

访问：
- 前端: http://localhost:3000
- API: http://localhost:8080
- SFU: http://localhost:8081

> 当前 Docker Compose 默认只提供 HTTP，本地开发可用；生产环境需要在外层配置 HTTPS/WSS 反向代理和正式证书。

### 本地开发

#### 方式一：Docker 方式（推荐，最简单）
```bash
docker compose -f deployment/docker-compose.yml up -d postgres redis coturn
docker compose -f deployment/docker-compose.yml up --build api-server sfu-server frontend
```

访问 http://localhost:3000

---

#### 方式二：原生运行（适合开发调试）

##### 1. 启动基础设施
```bash
docker compose -f deployment/docker-compose.yml up -d postgres redis coturn
```

##### 2. 编译 gRPC 代码（首次运行）
```bash
cd backend
make install-tools  # 安装protoc等工具
make proto          # 生成gRPC代码
```

##### 3. 启动API服务（终端1）
```bash
cd backend
go run cmd/server/main.go
```

##### 4. 启动SFU服务（终端2）
```bash
cd backend
go run cmd/sfu/main.go
```

##### 5. 启动前端（终端3）
```bash
cd frontend
npm install
npm run dev
```

**访问：http://localhost:3000**

---

### 🔬 当前项目的正确测试方式

#### 推荐方式：走完整认证和入会流程

当前默认配置下，`frontend/public/sfu-test.html` 和 `frontend/public/sfu-test-no-camera.html` 不能再作为正常测试入口。它们仍然直接连接 `SFU_URL/ws` 并手动发送 `{ type: "join", room_id, user_id }`，没有携带 API 签发的 `sfu_token`，因此会被 SFU 以 401 拒绝。

正确测试路径是使用正常产品流程：

1. 启动 PostgreSQL、Redis、API Server、SFU Server 和前端。
2. 打开 `http://localhost:3000`。
3. 注册或登录用户 A，创建房间或加入已有房间。
4. 另开一个浏览器窗口、无痕窗口或另一台设备，登录用户 B，加入同一个房间。
5. 进入会议页后，前端会从 `POST /api/v1/rooms/:id/join` 的响应中读取 `sfu_token`。
6. `SFUConnection` 会连接 `ws://localhost:8081/ws?token=<sfu_token>`，SFU 校验 token 后才允许媒体协商。
7. 验证两端是否能互相看到视频、听到音频，并在浏览器 DevTools / `chrome://webrtc-internals/` 中检查 ICE 与 WebRTC 状态。

#### 旧版极简测试页的定位

`/sfu-test.html` 和 `/sfu-test-no-camera.html` 现在只适合在本地开发中临时调试 SFU 媒体协商，不适合用来验证真实权限、安全边界或生产行为。

如果确实需要使用这两个页面做隔离调试，必须在非生产环境显式开启开发逃生阀：

```env
GO_ENV=development
SFU_ALLOW_UNAUTHENTICATED_DEV=true
```

开启后，SFU 会允许不带 `sfu_token` 的旧式直连测试。这个配置只能用于本地调试，不能用于生产，也不能作为权限/安全测试依据。生产环境下如果启用 `SFU_ALLOW_UNAUTHENTICATED_DEV=true`，服务会直接启动失败。

#### 当前 SFU 入会时序

```
用户浏览器                              API Server                              SFU Server
   │                                        │                                      │
   │── 登录 / 注册 ───────────────────────→ │                                      │
   │←─ app JWT ──────────────────────────── │                                      │
   │                                        │                                      │
   │── 创建/加入房间 ─────────────────────→ │                                      │
   │   Authorization: Bearer <app_jwt>       │                                      │
   │                                        │── 校验房间密码/锁房/成员/封禁 ──────│
   │←─ room + members + permissions          │                                      │
   │   + sfu_token ─────────────────────────│                                      │
   │                                        │                                      │
   │── WebSocket /ws?token=<sfu_token> ─────────────────────────────────────────→ │
   │                                        │                                      │── 校验签名、过期时间、purpose、room_id、user_id
   │── {type:"join"} ─────────────────────────────────────────────────────────→ │
   │←─ {type:"joined"} ─────────────────────────────────────────────────────── │
   │── offer / ice-candidate ─────────────────────────────────────────────────→ │
   │←─ answer / ice-candidate ───────────────────────────────────────────────── │
   │←════════════════════ WebRTC DTLS-SRTP 媒体连接 ════════════════════════════→│
```

#### 验证重点

| 验证项 | 预期结果 |
|--------|----------|
| 不带 token 访问 SFU `/ws` | 默认返回 401 |
| 使用普通 app JWT 当作 SFU token | 被拒绝 |
| 使用其他房间或其他用户的 SFU token | 被拒绝 |
| 正常加入房间后进入会议页 | 能连接 SFU 并完成 WebRTC 协商 |
| 被踢出用户重新获取 SFU token | 被 API 拒绝 |
| 非允许 Origin 连接 API/SFU WebSocket | 被拒绝 |
| `/healthz` | 服务存活时返回 200 |
| `/readyz` | 依赖可用时返回 200，不可用时返回失败 |

> 调试技巧：媒体问题优先看 MeetingRoom 控制台中的 `[WebRTC]` 日志、Network 面板里的 SFU WebSocket、以及 `chrome://webrtc-internals/` 中的 ICE candidate pair 和 connection state。

---

### 🧪 功能冒烟手册（覆盖 Phase 6/7/8 新功能）

下面这套清单覆盖核心会议链路 + 选择性订阅 + Simulcast + jti 防重放 + 设备记录 + 审计 API。**每项 5–10 分钟**，全部通过即可认为项目处于"已知良好"状态。

**前置准备：**

```bash
# 1. 启动基础设施 + 后端 + 前端（任选 Docker 或原生方式）
cp .env.example .env
docker compose -f deployment/docker-compose.yml up -d --build

# 2. 健康检查
curl -s http://localhost:8080/healthz   # 期望: ok
curl -s http://localhost:8080/readyz    # 期望: {"ready":true}
curl -s http://localhost:8081/healthz 2>/dev/null || echo "SFU 没暴露 healthz 是预期"

# 3. 准备两个浏览器/无痕窗口（下面记为 浏览器A、浏览器B）
#    访问 http://localhost:3000 各注册一个账号 alice / bob
```

> 💡 整套手册会用到的 token 通过登录响应拿到。建议在浏览器 DevTools → Application → Local Storage 找到 `token` 字段，或直接用 `curl` 登录后复制。下面 curl 示例统一记为 `$ALICE_TOKEN` / `$BOB_TOKEN`。

```bash
# curl 登录拿 token 的范例
ALICE_TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"account":"alice","password":"alicepass"}' | jq -r .token)
echo $ALICE_TOKEN
```

#### 冒烟项 1 — 基础音视频互通

> 注意：默认以“纯音频”入会（不开摄像头），双方都需要点一次摄像头按钮开启视频。

1. 浏览器A：登录 alice → 创建房间（记下 6 位 `room_id`）→ 进入会议页，允许摄像头/麦克风
2. 浏览器B：登录 bob → "加入房间"输入 `room_id` → 进入同一会议
3. A、B 各自点击摄像头按钮开启视频
4. 预期：
   - A、B 互相看到对方视频画面（画面右下角昵称应为对方用户，不再串人）
   - 互相听到声音（可让一方说话观察音量条）
5. 失败定位：
   - 黑屏：`chrome://webrtc-internals/` 看 `iceConnectionState` 是否 `connected`
   - 串人/占位：MeetingRoom 控制台搜 `[WebRTC]` 看 `Received remote track ... StreamID:` 是否能通过 `ssrc_mapping` 消息里的 `stream_id` 归属到正确用户
   - 单向：MeetingRoom 控制台搜 `[WebRTC]` 看 onTrack / onRemoteTrack 是否触发

#### 冒烟项 2 — 选择性订阅（Phase 6）

1. 在冒烟项 1 的会议中，**A 关闭摄像头**
2. 预期：
   - B 端 A 的 video tile 切换为占位符（"等待视频流..."）
   - B 端 DevTools Network 面板 → SFU WebSocket → 应看到 B 发出的 `unsubscribe` 消息
3. **A 重新开启摄像头**
4. 预期：B 端恢复显示 A 的视频；B 应发出 `subscribe` 消息

#### 冒烟项 3 — Simulcast 两层编码（Phase 7）

1. 在冒烟项 1 的会议中，浏览器A 打开 `chrome://webrtc-internals/`
2. 找到 A 的 PeerConnection → 展开 `outbound-rtp (video)`
3. 预期：**应看到至少 2 条 outbound-rtp 视频条目**（不是 1 条），各自带 `[rid:high]` / `[rid:low]` 标识
4. 同时检查 `framesEncoded` 都在持续递增（说明两层都在编码）
5. 失败说明：浏览器降级回单层（Safari/Firefox 可能发生），观察控制台是否有 `[WebRTC] simulcast unsupported` 日志

#### 冒烟项 4 — SFU token 一次性使用（Phase 8.2）

```bash
# 用 alice 加入房间，从响应里抽出 sfu_token
ROOM_ID=<上面创建的 room 主键 id，不是 6 位 room_id>
SFU_TOKEN=$(curl -s -X POST "http://localhost:8080/api/v1/rooms/$ROOM_ID/join" \
  -H "Authorization: Bearer $ALICE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{}' | jq -r .sfu_token)
echo $SFU_TOKEN

# 用同一个 token 连接 SFU 两次（第二次应被拒绝）
# 简便方式：在浏览器 DevTools Console 里跑：
new WebSocket("ws://localhost:8081/ws?token=" + "<粘贴 SFU_TOKEN>")
# 第一次会建立连接（前端已经在用了，可能直接看到 close）
# 第二次预期：close code 1008/4401 或类似，SFU 日志里出现 "sfu token already used"
```

更直接的验证：
- 检查 SFU server 日志，应看到 `validateAdmission ... ok=false reason=sfu_token_already_used` 类信息
- Redis 中应有 `sfu_jti:<uuid>` 键存在（`docker exec -it <redis容器> redis-cli KEYS 'sfu_jti:*'`）

#### 冒烟项 5 — 登录设备记录（Phase 8.3）

> 前提：数据库 devices 表必须含 `device_id` / `user_agent` 列与 `idx_user_device` 唯一索引。
> 全新部署由 `backend/migrations/init.sql` 自动创建；**存量库**需手动执行 init.sql 中
> `ALTER TABLE devices ADD COLUMN IF NOT EXISTS ...` 与 `CREATE UNIQUE INDEX IF NOT EXISTS ...`
> 三句（或重建 postgres 卷），否则本项会因 SQL 报错而失败（已在本轮修复中补齐迁移）。

```bash
# 用 alice 的 token 查设备列表
curl -s http://localhost:8080/api/v1/user/devices \
  -H "Authorization: Bearer $ALICE_TOKEN" | jq .

# 预期: devices 数组至少 1 条，包含 user_agent / ip / last_active
```

可选：换一个浏览器（比如 Edge）再登录 alice 一次，再调上面接口，应看到 2 条设备记录。

```bash
# 删除某条设备记录
DEVICE_ID=<上面返回某条 id>
curl -s -X DELETE "http://localhost:8080/api/v1/user/devices/$DEVICE_ID" \
  -H "Authorization: Bearer $ALICE_TOKEN"
# 再查一次，应少一条
```

#### 冒烟项 6 — 房间审计日志 API（Phase 8.4）

1. 浏览器A（alice，房主）在会议中**踢掉 bob**
2. 浏览器A 访问审计接口：

```bash
curl -s "http://localhost:8080/api/v1/rooms/$ROOM_ID/audit-logs?limit=20" \
  -H "Authorization: Bearer $ALICE_TOKEN" | jq .
```

3. 预期：`audit_logs` 数组包含一条 `action="kick"` 记录，`user_id=alice.id`、`target_id=bob.id`、有 `ip_address` / `user_agent`
4. 用 bob 的 token 调同一接口：

```bash
curl -s "http://localhost:8080/api/v1/rooms/$ROOM_ID/audit-logs" \
  -H "Authorization: Bearer $BOB_TOKEN"
# 预期: 403 permission denied（bob 不是 owner/admin）
```

#### 冒烟项 7 — 文本 E2EE 重放防护（Phase 8.1，自动化已覆盖）

`encryption.test.ts` 中 6 个用例已覆盖：首次接受、重复拒绝、乱序窗口内、窗口外、跨 sender 隔离、大跳跃重置。

```bash
npm --prefix frontend test -- --run src/utils/encryption.test.ts
# 预期: 6/6 passed
```

如需手动验证，可在 MeetingRoom 控制台手动构造重放消息，但成本高于自动化用例的覆盖度，**通常跳过**。

#### 冒烟项 8 — 媒体 E2EE 状态显示

1. Chrome/Edge 打开会议页：媒体 E2EE 标签应显示 **enabled**
2. Safari/Firefox 打开会议页：标签应显示 **unsupported**（因为不支持 `RTCRtpScriptTransform`）
3. Worker 应有解密成功计数：MeetingRoom 控制台搜 `mediaE2EE.*decrypted=` 关键字

#### 冒烟项汇总判定

| 序号 | 项目 | 通过判定 |
|------|------|----------|
| 1 | 基础音视频互通 | 双向能看能听 |
| 2 | 选择性订阅 | 关摄像头另一端 tile 变占位符 |
| 3 | Simulcast 两层 | webrtc-internals 看到 ≥2 条 outbound-rtp |
| 4 | jti 一次性使用 | 同 token 第二次连接被拒 |
| 5 | 设备记录 | `/user/devices` 返回当前设备 |
| 6 | 审计 API | 踢人后 owner 能查到 `kick` 记录，普通成员被 403 |
| 7 | E2EE 重放 | `encryption.test.ts` 6/6 |
| 8 | 媒体 E2EE | Chrome 端显示 enabled |

**全部通过** → 可以放心进入 Phase 9。
**任意失败** → 告知对应序号 + 看到的现象/日志，先修后再推进。

---

## 🔗 当前前后端配合的整体工作流程

### 正常会议流程

1. 用户通过 API 登录，获得 app JWT。
2. 用户创建或加入房间，API 完成业务权限检查。
3. API 返回房间信息、成员列表、当前用户权限和短期 `sfu_token`。
4. 前端会议页创建 `SFUConnection(roomId, userId, sfuToken, ...)`。
5. `SFUConnection` 连接 SFU `/ws?token=<sfu_token>`。
6. SFU 验证 token 后，以 token 中的 `room_id/user_id` 为准，不信任客户端伪造的身份字段。
7. WebRTC offer/answer 和 ICE candidate 通过 SFU WebSocket 交换。
8. 音视频媒体通过 WebRTC DTLS-SRTP 在浏览器和 SFU 之间传输。

### 与 SFU 测试相关的主要文件

| 文件路径 | 说明 |
|----------|------|
| `frontend/src/pages/MeetingRoom.tsx` | 当前推荐的会议测试入口，负责加入房间后把 `sfu_token` 传给 `SFUConnection` |
| `frontend/src/utils/webrtc.ts` | SFU WebSocket 和 WebRTC PeerConnection 封装，连接 `/ws?token=<sfu_token>` |
| `backend/internal/service/room.go` | 加入房间和刷新 SFU token 的业务校验入口 |
| `backend/internal/handlers/room.go` | `POST /api/v1/rooms/:id/join` 和 `POST /api/v1/rooms/:id/sfu-token` HTTP 处理 |
| `backend/internal/auth/token.go` | app JWT 和 SFU admission token 的生成与校验 |
| `backend/internal/sfu/server.go` | SFU WebSocket 鉴权、WebRTC 协商和媒体转发核心 |
| `frontend/public/sfu-test.html` | 旧版直连 SFU 调试页，默认不可用，仅开发逃生阀可用 |
| `frontend/public/sfu-test-no-camera.html` | 旧版无摄像头直连调试页，默认不可用，仅开发逃生阀可用 |

---

## 🔧 本轮修复记录（2026-10 代码审查）

本轮对后端（Go）与前端（TS）逐文件审查，发现并修复了以下缺陷。修复后：`go build ./...`、`go vet ./...`、`go test -race ./...` 全部通过；前端 `tsc --noEmit`、`vitest`（18 用例）、`vite build` 全部通过。

### 后端（Go）

| # | 严重度 | 问题 | 修复 |
|---|--------|------|------|
| B1 | 🔴 致命 | **信令 Hub 慢客户端残留 → 整进程 panic**：广播时发现某客户端 send 队列满，只把它从当前房间移除就 `close(send)`；客户端仍残留其他房间映射，后续向其他房间广播会向已关闭通道发送，`Run()` 事件循环 panic，全部信令下线 | 新增 `removeClientNoLock`：从所有房间+全局表完整移除；单播路径同样处理；配套回归测试（hub_test.go） |
| B2 | 🔴 高 | **同账号二次登录顶替后误删新连接**：`h.clients[userID]` 被覆盖后，旧连接断开时 `Unregister` 按 userID 删除会把新连接踢下线；旧连接残留房间映射泄漏 | Register 时踢掉同 userID 旧连接（完整清理+关闭）；Unregister 按对象身份匹配，幂等；配套回归测试 |
| B3 | 🔴 高 | **SFU onTrack 无锁遍历 room.Peers**：加锁拍了 `peersToNotify` 快照却从未使用，末尾直接裸遍历 map，与 handleJoin/removePeer 并发写入竞争，可触发 "concurrent map iteration and map write" 致命错误 | 改用快照遍历 |
| B4 | 🔴 高 | **autoSubscribeTrack（事件循环）无锁遍历 room.Peers**：同上数据竞争 | 加锁拍快照后再扇出 |
| B5 | 🟡 中 | **handleSubscribe 无锁读 room.Peers** | 读前持锁 |
| B6 | 🟡 中 | **peer.Tracks 并发读写**：事件循环写 map 不持锁，handleJoin/handleSubscribe/HandleMetrics 读 map（读者持锁但写者不持，锁形同虚设） | 事件循环写入时持 peer.mu；handleJoin/HandleMetrics 读取同样持锁 |
| B7 | 🟡 中 | **MuteUser 持 room.mu 同步等待事件循环**：事件循环处理 trackAdded 需要获取 room.mu，两者互相等待会造成死锁/长时间卡顿 | 快照后先释放 room.mu 再等待，广播基于快照 |
| B8 | 🔴 高 | **"camera"/"screen" 字面量与 msid GUID 比较恒为假**：浏览器 SDP 的 StreamID 是 GUID，`StreamID == "camera"`/`StreamID != "screen"` 永不成立 → 摄像头视频被无条件全量扇出（选择性订阅名存实亡），且 onTrack 内存在两条互相矛盾的订阅扇出路径 | 规则改为按 RID 区分：音频与无 RID 视频（屏幕共享/非 simulcast 回退）自动扇出，带 RID 的摄像头视频按需订阅；删除重复扇出路径 |
| B9 | 🟡 中 | **RTCP 路由表泄漏**：订阅侧注册的条目只按发布者清理，订阅者离开后条目永不删除（无限增长+悬挂 Peer 指针）；KickUser/removePeer 也不清理 | RTCPTarget 增加 Subscriber；UnregisterByUser 同时按发布者/订阅者清理；KickUser/removePeer 补满清理 |
| B10 | 🟢 低 | **rtpPool 死代码**：只有 Put 没有 Get，注释宣称的复用不存在 | 删除 |
| B11 | 🟡 中 | **重协商 offer 丢 ICE 候选**：Pion `CreateOffer` 返回的 SDP 不含候选（只在 Pending/CurrentLocalDescription 注入） | 改发 `PendingLocalDescription` 的 SDP |
| B12 | 🔴 高 | **devices 表缺列**：迁移缺 `device_id`/`user_agent` 列与唯一索引，`UpsertDevice` 运行时必然 SQL 报错，登录设备记录功能完全不可用 | init.sql 补列+唯一索引，并附 `ALTER ... IF NOT EXISTS` 兼容存量库 |
| B13 | 🟡 中 | **房间号碰撞无重试**：6 位数字空间有限，撞唯一索引直接报数据库错误；且使用已废弃的 `rand.Seed` | 生成前查重+重试 5 次；移除 rand.Seed |
| B14 | 🔴 高 | **禁言不生效**：`SendMessage` 不检查 `IsMuted`，被禁言者仍可发消息（UI 却提示“禁言后无法发送”） | SendMessage 拒绝被禁言成员 |

### 前端（TypeScript）

| # | 严重度 | 问题 | 修复 |
|---|--------|------|------|
| F1 | 🔴 致命 | **远端流归属机制从不起作用**：Pion 转发时给每个订阅者重写 SSRC，SFU 下发的发布者原始 SSRC 与本地收到的永远对不上；且 ontrack 触发时 RTP 尚未到达，`getSynchronizationSources()` 恒为空（ssrc=0），pendingRemoteTracks 的重试永远失败 → 所有远端流落入 userId=0 池，靠下标盲猜归属；只要有成员不开摄像头（默认入会即不开），就出现画面串人/黑屏 | 改为 msid 归属：SFU 映射消息携带 StreamID（被原样保留），前端优先按 `event.streams[0].id` 归属，SSRC 兜底；归属后从 0 号池移除避免双渲染 |
| F2 | 🔴 致命 | **开摄像头不发布**：默认音频入会（无视频 sender），首次开摄像头走 `replaceVideoTrack`，`getSenders().find(track?.kind==='video')` 找不到 → 静默 no-op，远端永远看不到摄像头；`muteVideo` 操作旧的 connect 时 stream（无视频轨）同样 no-op | 无视频 sender 时新建两层 simulcast transceiver 发布并携带 msid；静音/取消静音改为基于 PC 实际发送轨道 |
| F3 | 🔴 高 | **媒体重连必然崩溃**：`closeTransport()` 先把 `localStream` 置空，再 `connect(this.localStream!)` → connect(null) 空引用，重连 100% 失败 | 先捕获 stream；`closeTransport(stopLocalMedia=false)` 保留本地媒体；重连后自动重新发布摄像头轨道 |
| F4 | 🟡 中 | **订阅请求丢失**：订阅/退订消息在 WS 未 open 时被静默丢弃；订阅 effect 在连接建立前执行时整个集合丢失；重连后订阅集不恢复 | SFUConnection 维护期望订阅集（跨重连保留），`joined` 后统一重同步；useSFUMedia 在连接创建时注入此前计算的订阅意图 |
| F5 | 🔴 致命 | **前端 API 全部不可达**：`api.ts` baseURL 为空（相对路径），Vite dev server 与 Docker 前端容器都没有 /api 反向代理，登录等所有 HTTP 调用 404 | baseURL 使用 `VITE_API_HOST`（默认 `http://localhost:8080`，与 signaling.ts 一致）；Chat 的裸 fetch 改用统一 api 实例；ALLOWED_ORIGINS 默认值补 `http://localhost(:80)`（nginx 统一入口） |
| F6 | 🟡 高 | **自己发的加密消息永远显示“正在解密”**：Sender Key 只分发给他人，自己的链无法自解；回显消息卡在 pending | 发送时记录密文→明文映射，自己消息直接回显明文 |
| F7 | 🟡 中 | **聊天无发送者昵称**：后端广播用 `models.Message`（无昵称字段），历史接口的 MessageResponse 也从不填 SenderNickname | 前端从成员列表/当前用户解析昵称 |
| F8 | 🟡 中 | **VideoGrid 盲取 userStreams[0]**：同一用户多路流时可能拿到纯音频流（黑屏） | 优先选带活跃视频轨道的流；未归属池也过滤纯音频流 |

---

## 🔧 历史修复记录（旧版极简测试页面）

### 问题1：已连接用户看不到后加入用户的视频（核心逻辑错误）

**现象**：用户 A 先连接能看到自己，用户 B 后加入能看到 A，但 A 看不到 B。

**根因**：`sfu/server.go` 的 `onTrack` 中为新轨道调用 `otherPeer.PC.AddTrack(localTrack)` 后，没有触发新的 Offer/Answer 交换。WebRTC 连接建立后，PC 的本地描述和远程描述已经固定。如果不重新协商，浏览器不知道有新的接收轨道。

**修复**：
- `sfu/server.go`：`onTrack` 中为已连接用户 `AddTrack` 后，异步调用 `renegotiate(otherPeer)`
- `sfu/server.go`：新增 `renegotiate()` 方法，创建新 offer 并通过 WebSocket 发送给浏览器
- `sfu/server.go`：新增 `handleAnswer()` 方法，处理浏览器回复的 answer
- `sfu/server.go`：`HandleWebSocket` 的 switch 中增加 `case "answer"`
- `frontend/public/sfu-test.html`：`sfuWs.onmessage` 中增加 `case 'offer'`，处理 SFU 发来的重新协商 offer

### 问题2：锁顺序不一致导致的潜在死锁

**现象**：`handleJoin` 中先获取 `room.mu` 再获取 `existingPeer.mu`，而 `onTrack` 中原先先获取 `peer.mu` 再获取 `room.mu`。如果两者并发执行且涉及同一个 peer，可能死锁。

**修复**：将 `onTrack` 中的 `peer.Tracks` 写入操作移到 `room.mu.Lock()` 之后，统一锁顺序为 `room.mu` → `peer.mu`。

### 问题3：缺少 `time` 包导入

**修复**：在 `sfu/server.go` 的 import 中增加 `"time"`。

### 注释补充

为以下文件添加了详细中文注释（说明当前实现逻辑 + 预期正确逻辑）：
- `backend/internal/sfu/server.go`
- `backend/cmd/sfu/main.go`
- `backend/internal/config/config.go`
- `backend/pkg/logger/logger.go`
- `frontend/public/sfu-test.html`

---

## 🔍 当前状态与后续改进

核心会议链路、房间权限、SFU 入会鉴权、普通信令成员校验、聊天成员校验和文本消息 E2EE 基础链路已经实现。文本 E2EE 已支持成员变更后的 Sender Chain epoch 轮换以及基于滑动窗口的消息重放防护；媒体 E2EE 已提供基于浏览器 Encoded Transform 的可选高安全模式。摄像头视频已实现按需选择性订阅与两层 Simulcast 编码（订阅层固定为 high，自动切层未实现），SFU 入会 token 通过 `jti + Redis` 阻止单 token 重放，登录设备会被记录，房间敏感操作会写审计日志并通过 API 提供查询。

2026-10 本轮代码审查修复了一批关键缺陷（见下方“🔧 本轮修复记录”），其中包括：远端流归属改为可靠的 msid 映射（原 SSRC 映射因 Pion 重写 SSRC 永远匹配不上）、开摄像头不发布（无视频 sender 时静默 no-op）、媒体重连必然崩溃、信令 Hub 慢客户端残留导致整进程 panic、SFU 多处并发数据竞争、devices 表缺列导致设备记录功能不可用等。

仍建议继续补充网络自适应（TWCC）、Simulcast 自动切层、跨浏览器自动化 E2E 测试和录制策略；完整缺口见下方“📋 未完全实现功能清单”。

---

### ✨ 核心功能实现

| 优先级 | 功能 | 实现说明 |
|-------|------|---------|
| 🟡 中 | **群聊端到端加密基础链路 (Sender Keys)** | 支持多用户群聊文本加密的基础流程<br>• ECDH P-256 两两密钥协商<br>• HKDF 棘轮算法派生消息密钥<br>• Sender Key 通过成员校验后的信令分发<br>• 成员加入/离开/被踢后轮换 per-sender epoch<br>• 后端只持久化密文和 `encrypted` 标记<br>• 已接入基于 `senderId\|epoch` 的滑动窗口消息重放防护 |
| 🔴 高 | **媒体流端到端加密高安全模式** | 基于 WebRTC Encoded Transform / `RTCRtpScriptTransform` 在浏览器端加密 encoded audio/video frame，SFU 只转发密文 payload；不支持的浏览器回退到 DTLS-SRTP 并显示状态提示 |
| 🟡 部分实现 | **公钥指纹验证** | SHA-256 指纹，8位可读格式；UI 弹窗目前只显示自己的指纹，其他成员指纹列表未接入（`getMemberFingerprint` 为桩），安全号码/二维码比对待实现 |
| 🟢 低 | **密钥持久化（当前未启用）** | PBKDF2 存储工具已保留，但出于安全考虑改为仅内存保存（页面刷新后重新协商密钥），旧持久化数据会被主动清除；刷新后历史消息不可解密 |
| 🟡 中 | **屏幕共享大屏模式（本地）** | 本地开启屏幕共享时自动切换布局，大屏显示共享内容，侧边栏显示摄像头；远端成员的屏幕共享暂无对应大屏展示（见未完全实现清单） |
| 🟡 中 | **全局错误边界** | React ErrorBoundary 捕获渲染错误，友好提示用户 |
| 🟡 中 | **TURN 服务器支持** | 支持环境变量配置 STUN/TURN 服务器，用于 NAT 穿透 |
| 🟡 中 | **消息发送错误处理** | 发送失败时显示用户友好的错误提示 |
| 🟢 高 | **远端流归属（msid 流映射）** | SFU 下发流映射消息（含 StreamID/RID/SSRC），前端优先按 msid（`event.streams[0].id`）将远端流归属到 userId；SSRC 仅作兜底（Pion 转发会重写订阅端 SSRC） |
| 🟢 高 | **多路流支持（SFU 层）** | 同一用户可同时发送摄像头和屏幕共享流，SFU 独立转发；远端 UI 目前每个成员只展示一路带视频的流（见未完全实现清单） |
| 🟡 中 | **管理员强制静音/禁言** | 音频轨道转发实际控制 + 后端拒绝被禁言者发送消息 + 静音状态广播给所有客户端 |
| 🟢 高 | **选择性订阅** | 摄像头视频按需订阅（默认前 6 路开视频用户），音频/无 RID 视频（屏幕共享、非 simulcast 回退）自动全量订阅，订阅集随成员媒体状态动态调整，重连后自动重同步 |
| 🟢 中 | **Simulcast 两层编码** | 摄像头视频默认发送 `high` / `low` 两层（不支持的浏览器回退单层）；订阅端按 `quality` 选层（协议已支持，前端暂无切换 UI，默认 high）；屏幕共享保持单层 |
| 🟢 高 | **SFU token 一次性使用** | SFU admission token 内嵌 `jti`，SFU 接入时通过 Redis `SetNX` 标记，重复使用同一 token 会被拒绝 |
| 🟡 中 | **登录设备记录** | 登录时记录设备指纹（`X-Device-Id` 或 UA 哈希）、UA、IP、最近活跃时间，提供 `/user/devices` 查询/删除 API；devices 表结构已补齐 `device_id`/`user_agent` 列与唯一索引（存量库需执行 init.sql 中的 ALTER 语句或重建卷） |
| 🟡 中 | **房间审计日志 API** | kick/mute/unmute/set_admin/remove_admin/transfer_owner/lock/unlock/delete 等敏感操作落库，owner/admin 通过 `/rooms/:id/audit-logs` 分页查询 |

### 📋 未完全实现功能清单

以下是对当前代码逐项核对后总结的**未完全实现/仅部分实现**的功能，按模块归类。
核心会议链路（登录、建房/入会、双向音视频、公聊、踢人/禁言、审计、设备记录、SFU token 鉴权）可用；以下缺口不影响主流程，但均未达到“完整产品”水平：

#### 媒体质量与网络自适应
| 功能 | 现状 | 缺口 |
|------|------|------|
| TWCC / 自适应码率 | 未实现 | SFU 不做带宽估计；REMB 包收到后不路由（代码注释明确“留给带宽估计算法”） |
| Simulcast 自动切层 | 协议层支持手动选层 | 无带宽驱动；前端无 quality 切换 UI，固定订阅 high 层 |
| NACK 重传完整链路 | 部分 | SFU 只把 NACK 转发给发布者浏览器；自身无重传缓存，跨订阅者的丢包无法补偿 |
| RTCP 统计利用 | 部分 | RR 丢包/抖动只打日志，无 QoS 决策 |
| 摄像头中途关闭 | 部分 | 仅置 `track.enabled=false`（发黑帧），不停止发布；SFU 仍在转发该轨道 |
| 中途换摄像头设备 | 部分 | `replaceVideoTrack` 换轨道后 SFU 会收到新轨道，但其他订阅者的旧订阅 sender 不会自动迁移，需退订/重订 |

#### 会议功能
| 功能 | 现状 | 缺口 |
|------|------|------|
| 远端屏幕共享展示 | SFU 会转发 | UI 每个远端成员只渲染一路带视频的流：摄像头+屏幕共享同开时只显示其一；没有远端共享的大屏布局；视频关闭但共享屏幕中的成员不渲染 tile，其共享内容不可见；退订摄像头视频时会连带退订其屏幕流 |
| 私聊 | 后端部分 | `receiver_id` 支持久化+定向单播，但 `GetPrivateMessages` 未暴露 HTTP 路由，前端无私聊 UI |
| 图片/文件消息 | 未实现 | 模型有 `image`/`file` 枚举，无上传/发送能力 |
| 离开会议的服务端记录 | 未实现 | 无 leave HTTP 路由（service 层有 `LeaveRoom` 但未接线）；前端断线/退出后 DB 成员记录仍为 active，房间 `current_size` 不减少，成员列表可能含已离线用户（重新加入靠 join 幂等恢复） |
| 实时在线状态 | 部分 | Redis `online:<uid>` 键只写不读；成员列表来自 DB `room_members`，与实际 WebSocket 连接不同步 |
| 同账号多端同时入会同房间 | 不可靠 | SFU `room.Peers[userID]` 直接覆盖：先连的连接断开时会误删后连的 peer；信令 Hub 已支持同 uid 新连接踢旧连接，但 SFU 侧未对齐 |

#### E2EE 相关
| 功能 | 现状 | 缺口 |
|------|------|------|
| 公钥指纹比对 UI | 部分 | 弹窗只显示自己的指纹，成员指纹列表永远为空（`getMemberFingerprint` 返回 `'pending'` 桩）；无 safety number/二维码比对 |
| 密钥持久化 | 已改为不启用 | 出于安全考虑仅内存保存，刷新后重新协商；刷新后本会话历史消息无法解密，聊天窗口显示“[已加密消息 - 正在解密...]” |
| 媒体密钥轮换 | 部分 | 成员加入/离开会轮换媒体 sender key（epoch +1），但接收端媒体帧解密无重放窗口/序号验证 |

#### 管理与运维
| 功能 | 现状 | 缺口 |
|------|------|------|
| gRPC `SetVideoEnabled` | 占位 | 直接返回成功，未实现视频转发控制 |
| 房间统计 `GetRoomStats` | 部分 | PeerStats 的 `AudioEnabled/VideoEnabled/ConnectionState` 是硬编码值，不反映真实状态 |
| 审计日志 UI | API 完整 | 前端无查询界面，仅 `/rooms/:id/audit-logs` 接口 |
| 设备管理 UI | API 完整 | 前端无 `/user/devices` 入口 |
| 踢出封禁清理 | 未实现 | `RemoveExpiredKicks` 从未被调用，`kick_bans` 表只增不删（功能上靠 `expires_at` 过滤，但表会增长） |
| 优雅停机下的信令通知 | 部分 | 服务停机不会通知房间内成员（客户端只会看到 WS 断开重连失败） |

#### 测试与工程化
| 功能 | 现状 | 缺口 |
|------|------|------|
| 跨浏览器自动化 E2E | 未实现 | 无 Playwright/Puppeteer 矩阵，多端互通靠手工冒烟 |
| 录制回放 | 未实现 | SFU 不落盘媒体（且与媒体 E2EE 互斥） |
| SFU 集群 | 未实现 | 单实例，房间/订阅不跨节点同步 |
| 移动端原生客户端 | 未实现 | 仅浏览器端 |

### ✨ 待实现的高级功能（建议优先级）

| 优先级 | 功能 | 技术方案 | 预期收益 |
|-------|------|---------|---------|
| 🔴 高 | TWCC + 自适应 Simulcast 切层 | 接入 transport-cc，按下行带宽估计自动选择 high/low 层 | 弱网下不再固定 high，避免卡顿/掉帧 |
| 🔴 高 | 离开会议服务端记录 | 暴露 leave 路由 + WS 断开时软删成员（带超时缓冲），同步 current_size | 成员列表/人数反映真实在线状态 |
| 🟡 中 | NACK/FEC 完整链路 | SFU 启用重传缓存和前向纠错 | 弱网下视频流畅度提升 |
| 🟡 中 | 跨浏览器自动化 E2E | Playwright matrix（Chrome/Edge/Firefox/Safari） | 用真实多端冒烟代替手测假设 |
| 🟡 中 | 公钥/设备身份比对 UI | safety number / 二维码线下比对 | 防止信令通道下的中间人替换公钥 |
| 🟡 中 | 远端多路流/屏幕共享大屏 | VideoGrid 支持每成员多流展示 + 远端共享大屏布局 | 完整兑现多路流能力 |
| 🟢 低 | 录制回放功能 | SFU 将媒体流写入存储，提供回放接口 | 支持会议录播（与媒体 E2EE 互斥，需要产品权衡） |
| 🟢 低 | SFU 集群部署 | 多 SFU 实例 + 跨实例订阅同步 | 支持万人级会议 |
| 🟢 低 | 移动端原生客户端 | iOS / Android 原生封装 | 真正多端覆盖 |

---

## 💡 开发建议

### 前端开发建议
1. **先跑通核心** - 用 `npm run dev` 启动前端，先验证登录 + 房间创建功能
2. **SFU连接调试** - 在 MeetingRoom 页面查看控制台的 `[WebRTC]` 前缀日志
3. **Chrome调试技巧** - 访问 `chrome://webrtc-internals/` 查看ICE连接状态和流统计
4. **信令调试** - Network面板筛选WS协议，查看两个WebSocket消息格式
5. **Redux DevTools** - 安装Redux DevTools插件，追踪状态变更

### 建议开发顺序
1. **基础连通性** - 两个浏览器窗口能互相看到对方视频（局域网内测试）
2. **音频互通** - 验证能听到对方声音
3. **屏幕共享** - 验证本地开启共享后大屏布局切换，以及 SFU 层多路流转发（注：远端 UI 目前每个成员只展示一路视频流，摄像头+屏幕共享同开时只显示其一，见未完全实现清单）
4. **成员列表同步** - 静音/角色变更能实时广播
5. **远端流归属** - 验证远端流能按 msid 正确归属到对应用户（控制台看 ssrc_mapping 消息与 ontrack 日志）

> **注**：文本 E2EE 已在前端实现，无需 SFU 参与

### 后端服务启动顺序
```bash
# 1. 启动基础设施（PostgreSQL + Redis）
docker compose -f deployment/docker-compose.yml up -d postgres redis

# 2. 启动API Server (端口8080)
cd backend && go run cmd/server/main.go

# 3. 启动SFU Server (端口8081 + gRPC 50051)
cd backend && go run cmd/sfu/main.go

# 4. 启动前端 (端口3000)
cd frontend && npm install && npm run dev
```

### 调试输出前缀
| 前缀 | 来源 | 说明 |
|------|------|------|
| `[Signaling]` | signaling.ts | 控制信令WebSocket |
| `[WebRTC]` | webrtc.ts | SFU媒体连接 |
| `[Meeting]` | MeetingRoom.tsx | 会议页面逻辑 |

> **关于 E2EE 和媒体加密的特别说明**：
>
> - 文本消息侧包含 E2EE 相关模型和前端加密工具，已支持 Sender Keys 基础链路、成员变更后的 per-sender epoch 轮换以及基于 `senderId|epoch` 的滑动窗口消息重放防护；下一步建议补齐设备身份比对 UI（safety number / 二维码线下校验）。
> - 音视频媒体使用 WebRTC 标准的 DTLS-SRTP 传输加密：浏览器到 SFU、SFU 到浏览器每一段链路都是加密传输。
> - 项目提供可选媒体 E2EE 高安全模式：支持 `RTCRtpScriptTransform` 的浏览器会在端侧加密 encoded audio/video frame，SFU 只转发密文 RTP payload。
> - 媒体 E2EE 受浏览器支持限制；不支持的浏览器会回退到 DTLS-SRTP 传输加密并在 UI 中显示状态。
> - 媒体 E2EE 开启后，服务端录制、转码、转写和智能摘要等依赖明文媒体的能力不可用，除非引入持钥客户端/录制机器人并改变信任模型。
> - 媒体 E2EE Worker 线程会收集加密/解密帧计数、失败计数等诊断指标并通过 `postMessage` 回传主线程，UI 据此显示加密状态（enabled/negotiating/warning/unsupported/disabled）。
> - 详细的浏览器支持矩阵、功能限制、诊断信息和手动验证步骤见 `docs/E2EE_LIMITATIONS.md`。

## 📄 许可证

MIT
