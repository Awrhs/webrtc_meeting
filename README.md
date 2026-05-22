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
    ├── signaling.ts       # 信令WebSocket客户端（聊天/控制）
    ├── api.ts             # Axios实例 + 拦截器
    └── encryption.ts      # AES加密工具（待整合）
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
| `connect(localStream)` | 建立SFU WebSocket + 创建PeerConnection |
| `disconnect()` | 安全断开，清理所有资源（定时器/流/连接） |
| `muteAudio(isMuted)` | 本地音频轨道启用/禁用 |
| `muteVideo(isMuted)` | 本地视频轨道启用/禁用 |
| `replaceVideoTrack(newTrack)` | 切换摄像头 ↔ 屏幕共享 |
| `onRemoteTrack(userId, track, stream)` | 远程流回调，触发UI刷新 |
| `scheduleReconnect()` | 连接失败自动重试（最多5次） |

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
- **SFU音视频通信核心** - 完整的WebRTC实现（前端 `SFUConnection` 类封装）
  - ✅ ICE候选处理 - 双向WebSocket交换，支持NAT穿透，含断线自动重连
  - ✅ SDP Offer/Answer协商 - 连接SFU专用WebSocket端口完成握手
  - ✅ 媒体流转发 - Pion WebRTC SFU转发音视频流，onTrack回调正确触发
  - ✅ 多轨支持 - 摄像头/屏幕共享通过 replaceTrack 无缝切换
  - ✅ 自动重连 - WebSocket/ICE连接失败自动重试5次，间隔3秒
- **SFU管理功能** - 通过gRPC强制静音，通过信令广播通知所有客户端


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
| `room.go` | 房间管理 HTTP API | ① `CreateRoom`/`GetRoom`/`GetMyRooms`：房间 CRUD；② `JoinRoom`：加入逻辑 + 广播 `user_joined` 事件 + 返回成员列表和权限；③ `DeleteRoom`/`LockRoom`/`UnlockRoom`：房主操作（权限检查）；④ `KickMember`/`MuteMember`/`UnmuteMember`：管理员操作 + 调用 SFU gRPC `KickUser`/`MuteUser` + 广播信令事件；⑤ `SetAdmin`/`RemoveAdmin`/`TransferOwner`：权限变更 + 广播 `role_changed`；⑥ `UpdateMediaState`：广播媒体状态变更；⑦ `NotifyConnectionFailed`：SFU 回调接口，通知某用户 ICE 连接失败 |
| `chat.go` | 聊天消息 HTTP API | ① `GetMessages`：分页查询房间历史消息；② `SendMessage`：持久化消息 + 通过 Hub 广播（私聊则单播）；③ `UploadPublicKey`/`GetPublicKey`/`DeletePublicKey`：E2EE 公钥管理 |

#### 5. 服务层 `internal/service/`

| 文件 | 职责 | 应实现的具体逻辑 |
|------|------|-----------------|
| `user.go` | 用户业务逻辑 | ① `Register`：唯一性检查（email/phone/username）→ bcrypt 密码哈希 → 创建用户；② `Login`：账号类型自动识别（手机号/邮箱/用户名）→ bcrypt 密码比对 → 生成 JWT → Redis 标记在线；③ `GetUserByID`：先查 Redis 缓存，miss 再查 DB 并回写缓存；④ `UpdateProfile`/`UpdatePassword`：更新后清除 Redis 缓存 |
| `room.go` | 房间业务逻辑（核心） | ① `CreateRoom`：生成 6 位 `room_id` 和 8 位邀请码 → 密码 bcrypt 哈希 → 事务创建房间 + 添加房主成员 → Redis 缓存；② `JoinRoom`：按 `room_id` 或主键查找 → 检查房间锁定/踢出封禁/人数上限/密码或邀请码验证 → 软删除恢复或新成员记录 → 更新房间人数 → 清除缓存；③ `KickMember`：权限检查（不能踢房主/同级管理员）→ 软删除成员 → 创建 `KickBan`（10 分钟有效期）→ 事务提交 → 清除缓存；④ `MuteMember`：权限检查 → 更新 `RoomMember.IsMuted`；⑤ `SetAdmin`/`TransferOwner`：仅房主可操作 → 事务更新角色；⑥ `CheckPermission`/`HasPermission`：基于 `RolePermissions` 矩阵的细粒度权限检查；⑦ `UpdateMediaState`：更新 `IsVideoOn`/`IsAudioOn`；⑧ 所有写操作后联动 Redis 缓存失效 |
| `chat.go` | 聊天业务逻辑 | ① `SendMessage`：组装 `Message` 实体（私聊设置 `Encrypted=true`）→ 调用 Repository 持久化；② `GetMessages`：分页查询并转换为 `MessageResponse`；③ `SavePublicKey`/`GetPublicKey`/`DeletePublicKey`：E2EE 公钥的 CRUD |

#### 6. 数据访问层 `internal/repository/`

| 文件 | 职责 | 应实现的具体逻辑 |
|------|------|-----------------|
| `user.go` | 用户数据访问 + Redis 缓存 | ① `UserRepository`：User CRUD（`FindByEmail`/`FindByPhone`/`FindByUsername`/`FindByID`）；② `RedisClient`：在线状态（`SetOnline`/`GetOnline`）、房间成员集合（`AddToRoom`/`RemoveFromRoom`/`GetRoomMembers`）、用户缓存（`SetUser`/`GetUser`/`DeleteUser`）、房间缓存（`SetRoom`/`GetRoom`/`DeleteRoom`）、用户房间列表缓存（`SetUserRooms`/`GetUserRooms`/`DeleteUserRooms`） |
| `room.go` | 房间数据访问 | ① `RoomRepository`：Room CRUD；② 成员管理：`AddMember`/`UpdateMember`/`GetMembers`/`GetMember`（仅未离开）/`GetAnyMember`（含已离开）/`RemoveMember`（软删除：更新 `left_at`）/`CountMembers`；③ 踢出封禁：`CreateKickBan`/`IsKicked`/`RemoveExpiredKicks`；④ 审计日志：`CreateAuditLog`/`GetAuditLogs`；⑤ 事务支持：`Begin()` 返回新 Repository（含 `*gorm.DB` 事务实例）/`Commit()`/`Rollback()` |
| `chat.go` | 聊天数据访问 | ① `CreateMessage`：插入消息；② `GetMessages`：按房间查询群聊消息（`receiver_id IS NULL`）并倒序分页，返回前 `reverseMessages` 恢复时间正序；③ `GetPrivateMessages`：双向查询私聊记录；④ `SavePublicKey`：upsert 逻辑（存在则更新，不存在则创建）；⑤ `GetPublicKey`/`DeletePublicKey` |

#### 7. SFU 核心层 `internal/sfu/`

| 文件 | 职责 | 应实现的具体逻辑 |
|------|------|-----------------|
| `server.go` | SFU 媒体引擎（最核心） | ① **数据结构**：`Room`（房间，含 `Peers` 映射）、`Peer`（用户，含 `PC` PeerConnection、`Conn` WebSocket、`Tracks` 本地轨道映射、`Subscriptions` RTPSender 列表）、`TrackInfo`（轨道元数据：SSRC/Kind/StreamID/启用状态）；② **生命周期**：`HandleWebSocket`（WebSocket 升级 → 消息循环：`join`→`handleJoin`、`offer`→`handleOffer`、`ice-candidate`→`handleIceCandidate`）；③ **WebRTC 协商**：`handleJoin` 创建 `PeerConnection`（Pion API + 标准 ICE 配置）→ 注册 `OnTrack`/`OnICECandidate`/`OnConnectionStateChange` → 发送 `joined` → 为新用户订阅房间内已有轨道 → 发送 `ssrc_mapping`；④ **媒体转发**：`onTrack` 收到远程轨道 → 创建 `TrackLocalStaticRTP` → 启动 goroutine `Read/Write` 转发 RTP 包 → 为房间内所有其他用户 `AddTrack` → 广播 `ssrc_mapping`；⑤ **ICE 交换**：双向通过 WebSocket 透传 `ice-candidate` 消息；⑥ **控制方法**：`KickUser`（关闭 PC + 清理房间）、`MuteUser`（设置 `TrackInfo.IsEnabled` + 广播 `muted` 信令）、`GetRoomStats`/`ListRooms` |
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
# 复制环境变量（如需要）
cp .env.example .env

# 启动所有服务（包括数据库、Redis、Coturn、API、SFU、前端）
docker-compose -f deployment/docker-compose.yml up -d --build
```

访问：
- 前端: http://localhost:3000
- API: http://localhost:8080
- SFU: http://localhost:8081

### 本地开发

#### 方式一：Docker 方式（推荐，最简单）
```bash
cd deployment
docker-compose up -d postgres redis coturn
docker-compose up --build api-server sfu-server frontend
```

访问 http://localhost:3000

---

#### 方式二：原生运行（适合开发调试）

##### 1. 启动基础设施
```bash
cd deployment
docker-compose up -d postgres redis coturn
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

## 🔍 已知问题

> 📌 **所有之前列出的 Bug 均已全部修复完成**

---

### ✨ 核心功能实现

| 优先级 | 功能 | 实现说明 |
|-------|------|---------|
| 🟢 中 | **群聊端到端加密 (Sender Keys)** | 基于 Signal Sender Keys 架构，支持多用户群聊加密<br>• ECDH P-256 两两密钥协商<br>• HKDF 棘轮算法派生消息密钥<br>• Sender Key 通过加密信令分发<br>• 消息序号追赶 + 缓存自动解密 |
| 🟢 中 | **公钥指纹验证** | SHA-256 指纹，8位可读格式，UI 弹窗支持线下比对 |
| 🟢 低 | **密钥持久化** | PBKDF2 加密存储到 localStorage，刷新页面恢复密钥状态 |
| 🟡 中 | **屏幕共享大屏模式** | 开启屏幕共享自动切换布局，大屏显示共享内容，侧边栏显示摄像头 |
| 🟡 中 | **全局错误边界** | React ErrorBoundary 捕获渲染错误，友好提示用户 |
| 🟡 中 | **TURN 服务器支持** | 支持环境变量配置 STUN/TURN 服务器，用于 NAT 穿透 |
| 🟡 中 | **消息发送错误处理** | 发送失败时显示用户友好的错误提示 |
| 🔴 高 | **SSRC 流映射** | SFU 精确按 userId 匹配远程流，解决流归属问题 |
| 🔴 高 | **多路流支持** | 同一用户可同时发送摄像头和屏幕共享流，SFU 独立转发 |
| 🟡 中 | **管理员强制静音** | 音频轨道转发实际控制，静音状态广播给所有客户端 |

### ✨ 待实现的高级功能
> 功能基本可用，不影响核心流程，但可以增强体验或性能

| 优先级 | 功能 | 技术方案 | 预期收益 |
|-------|------|---------|---------|
| 🔴 高 | **媒体流端到端加密** | WebRTC Insertable Streams / Frame Encryption | 真正的全流程 E2EE |
| 🔴 高 | **成员变更密钥轮换** | MLS 协议 TreeKEM 组密钥管理 | 加入/退出后向保密 |
| 🟡 中 | **消息重放防护** | 消息序号验证 + 滑动窗口 | 防止消息重放攻击 |
| 🟡 中 | 选择性订阅 | 前端通知 SFU 只订阅特定用户的流 | 降低带宽占用，支持大型会议 |
| 🟢 低 | Simulcast 多层编码 | 发送方编码多分辨率，SFU 按需转发 | 适应不同网络环境 |
| 🟢 低 | NACK/FEC 拥塞控制 | SFU 实现重传和前向纠错 | 弱网下视频流畅度提升 |
| 🟢 低 | 录制回放功能 | SFU 将媒体流写入存储，提供回放接口 | 支持会议录播 |
| 🟢 低 | SFU 集群部署 | 多 SFU 实例负载均衡，信令服务协调 | 支持万人级会议 |

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
3. **屏幕共享** - 验证摄像头和屏幕共享能同时显示（需 SFU 支持多路流）
4. **成员列表同步** - 静音/角色变更能实时广播
5. **SSRC 映射** - SFU 实现流与用户的正确关联

> **注**：文本 E2EE 已在前端实现，无需 SFU 参与

### 后端服务启动顺序
```bash
# 1. 启动基础设施（PostgreSQL + Redis）
docker-compose -f deployment/docker-compose.yml up -d postgres redis

# 2. 启动API Server (端口8080)
cd backend && go run cmd/server/main.go

# 3. 启动SFU Server (端口8081 + gRPC 50051)
cd backend && go run cmd/sfu/main.go

# 4. 启动前端 (端口5173)
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
> - 文本消息侧包含 E2EE 相关模型和前端加密工具，但仍需要结合实际产品流程验证密钥交换、指纹校验、成员变更密钥轮换等完整安全语义。
> - 音视频媒体使用 WebRTC 标准的 DTLS-SRTP 传输加密：浏览器到 SFU、SFU 到浏览器每一段链路都是加密传输。
> - 这不等同于真正的参会者到参会者媒体 E2EE。当前 SFU 需要接收并转发 RTP 载荷，理论上 SFU 侧仍可访问媒体内容。
> - 如果要实现真正的媒体 E2EE，需要在浏览器端引入 WebRTC Insertable Streams / Encoded Transform，对 encoded audio/video frame 做端侧加密，SFU 只转发密文。

## 📄 许可证

MIT
