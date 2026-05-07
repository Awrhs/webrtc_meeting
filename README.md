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
  └─ SFU WebSocket (端口 8081) → SFU Server
       └─ 处理：SDP协商、ICE候选交换、媒体流传输
       └─ 事件：joined / offer / answer / ice-candidate
```

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
- **gRPC SFU控制通道** - 踢人/静音等管理操作通过gRPC调用
- **SFU音视频通信核心** - 完整的WebRTC实现（前端 `SFUConnection` 类封装）
  - ✅ ICE候选处理 - 双向WebSocket交换，支持NAT穿透，含断线自动重连
  - ✅ SDP Offer/Answer协商 - 连接SFU专用WebSocket端口完成握手
  - ✅ 媒体流转发 - Pion WebRTC SFU转发音视频流，onTrack回调正确触发
  - ✅ 多轨支持 - 摄像头/屏幕共享通过 replaceTrack 无缝切换
  - ✅ 自动重连 - WebSocket/ICE连接失败自动重试5次，间隔3秒
- **SFU管理功能** - 通过gRPC强制静音，通过信令广播通知所有客户端

### ⚠️ 部分实现（高级功能待完善）
- **远程流用户ID关联** - 能收到视频流，但需要根据track ID映射到具体用户
- **屏幕共享** - 前端有切换代码，但SFU多轨道处理未完整测试
- **E2EE端到端加密** - 有crypto工具包和公钥交换接口，但消息发送/接收未实际调用加密
- **断线重连后WebRTC重建** - WebSocket会自动重连，但需重新建立PeerConnection

### ❌ 不在当前范围（已移除）
- **Electron桌面端打包** - 仅支持浏览器端
- **Capacitor移动端打包** - 仅支持浏览器端

### 🚀 规划中的高级功能
- **选择性订阅 + Simulcast** - 降低带宽占用
- **QoS拥塞控制** - NACK/FEC丢包恢复
- **录制回放功能**
- **SFU集群部署**

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
│       └── sfu-test.html # SFU功能极简测试页面 ⭐
├── deployment/
│   └── docker-compose.yml
└── README.md
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

#### 1. 启动基础设施
```bash
cd deployment
docker-compose up -d postgres redis coturn
```

#### 2. 编译 gRPC 代码（首次运行）
```bash
cd backend
make install-tools  # 安装protoc等工具
make proto          # 生成gRPC代码
```

#### 3. 启动API服务（终端1）
```bash
cd backend
go run cmd/server/main.go
```

#### 4. 启动SFU服务（终端2）
```bash
cd backend
go run cmd/sfu/main.go
```

#### 5. 启动前端（终端3）
```bash
cd frontend
npm install
npm run dev
```

访问 http://localhost:5173

### 🔬 SFU功能测试（重要！）

**推荐先使用极简测试页面验证SFU核心功能：**

```
访问: http://localhost:5173/sfu-test.html
```

测试步骤：
1. 打开两个浏览器窗口，都访问测试页面
2. 两个窗口都点击 **"1. 开启本地媒体"**
3. 第一个窗口用户ID填 `1`，第二个窗口用户ID填 `2`
4. 两个窗口房间ID都填相同数字（默认 `1`）
5. 依次点击 **"2. 连接SFU"**
6. 观察状态面板和视频流，验证是否能互相看到对方

---

## 🔍 已知问题与待开发清单

### ✅ 已修复的核心功能
| 功能 | 状态 | 说明 |
|------|------|------|
| SFU WebSocket连接 | ✅ 已修复 | 前端 MeetingRoom 正确初始化 SFUConnection 类 |
| ICE候选处理 | ✅ 已修复 | SFUConnection 完整实现 onicecandidate 双向交换 |
| 远程流回调处理 | ✅ 已修复 | onTrack 回调正确存储 remoteStreams 并刷新 UI |
| 媒体状态信令广播 | ✅ 已修复 | 静音/开关摄像头通过 WebSocket 广播通知房间成员 |
| SignalingClient资源清理 | ✅ 已修复 | disconnect() 正确清空 handlers、定时器、重置状态 |
| RoomList删除房间API | ✅ 已修复 | 改用 Redux thunk 而非原生 fetch，统一错误处理 |
| 断线后WebRTC自动重连 | ✅ 已实现 | SFUConnection 内置 scheduleReconnect，最多重试5次 |
| 聊天消息WebSocket推送 | ✅ 已实现 | Chat组件已完整实现收发逻辑 |

### 🔄 待完善功能
| 功能 | 状态 | 说明 |
|------|------|------|
| 远程流用户ID关联 | ⚠️ 依赖后端 | SFU需在SDP/信令中携带SSRC→User映射，当前只能按索引分配 |
| E2EE端到端加密 | ⚠️ 待完善 | encryption工具存在但未整合到消息发送/接收流程 |
| SFU强制静音 | ⚠️ 空实现 | gRPC接口只是打log，没有操作媒体轨道 |
| TURN服务集成 | ⚠️ 未配置 | docker-compose有Coturn但WebRTC config未加入 |
| 屏幕共享多轨道 | ⚠️ 未测试 | SFU转发逻辑需要在真实网络验证 |

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
3. **屏幕共享** - 验证 replaceTrack 切换画面正常
4. **成员列表同步** - 静音/角色变更能实时广播
5. **E2EE加密** - 最后再考虑端到端加密（需SFU支持加密流转发）

### 后端服务启动顺序
```bash
# 1. 启动基础设施（PostgreSQL + Redis）
docker-compose -f deployment/docker-compose.yml up -d postgres redis

# 2. 启动API Server (端口8080)
cd backend && go run cmd/server/main.go

# 3. 启动SFU Server (端口8081 + gRPC 8082)
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

## 📄 许可证

MIT
