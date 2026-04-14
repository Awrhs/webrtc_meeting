# 跨平台加密SFU音视频会议软件

基于Pion WebRTC的企业级跨平台音视频会议系统，支持Web/桌面/移动端，全链路加密，完整权限管理。

## 技术栈

### 后端
- **控制面**: Go + Gin 框架
- **媒体面**: Pion WebRTC (SFU)
- **信令**: gorilla/websocket
- **存储**: PostgreSQL + Redis
- **认证**: JWT

### 前端
- **框架**: React 18 + TypeScript
- **构建**: Vite
- **UI**: Ant Design Pro
- **状态管理**: Redux Toolkit
- **跨端**: Electron (桌面) + Capacitor (移动端)

### 基础设施
- **STUN/TURN**: Coturn
- **容器化**: Docker + Docker Compose

## 核心功能

- ✅ 用户系统（注册/登录/信息管理）
- ✅ 房间管理（创建/加入/验证/状态管理）
- ✅ 三级权限管理（房主/管理员/普通成员）
- ✅ SFU音视频通信（基于Pion WebRTC）
- ✅ 文本聊天（公聊+端到端加密私聊）
- ✅ 屏幕共享、举手发言、会议录制
- ✅ 全链路加密（DTLS/SRTP + AES + HTTPS/WSS）
- ✅ 跨平台适配（Web/桌面/移动端）

## 快速开始

### 使用 Docker Compose 启动

```bash
cp .env.example .env
docker-compose -f deployment/docker-compose.yml up -d --build
```

或者使用 Makefile：

```bash
cp .env.example .env
make docker-up
```

### 本地开发

#### 后端编译运行

```bash
cd backend/cmd/server
go build -o server
./server
```

#### SFU媒体服务

```bash
cd backend/cmd/sfu
go build -o sfu
./sfu
```

#### 前端开发

```bash
cd frontend
npm install
npm run dev
```

## 项目结构

```
conference-basted_on_the-SFU model/
├── backend/
│   ├── cmd/              # 入口程序
│   │   ├── server/       # 信令/API服务器
│   │   └── sfu/          # SFU媒体服务器
│   ├── internal/         # 内部模块
│   │   ├── models/       # 数据模型
│   │   ├── handlers/     # API处理器
│   │   ├── service/      # 业务逻辑
│   │   ├── repository/   # 数据访问
│   │   ├── sfu/          # SFU媒体处理
│   │   ├── signaling/    # 信令处理
│   │   └── middleware/   # 中间件
│   └── pkg/              # 公共包
├── frontend/             # React前端
├── deployment/           # 部署配置
│   ├── docker/           # Dockerfile
│   └── docker-compose.yml
└── README.md
```

## 项目架构

### 整体架构图

```
┌─────────────────────────────────────────────────────────────┐
│                     用户端 (Web/桌面/移动端)                    │
│  ┌─────────┐ ┌─────────┐ ┌─────────┐                        │
│  │ React  │ │ WebRTC  │ │ 加密   │                        │
│  │  UI    │ │ 采集   │ │ 工具   │                        │
│  └─────────┘ └─────────┘ └─────────┘                        │
└─────────────────────────────────────────────────────────────┘
                              │ HTTP / WebSocket
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                 Nginx 反向代理 / 负载均衡                      │
└─────────────────────────────────────────────────────────────┘
                              │
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
┌─────────────────────────────────────────────────────────────┐
│              API/信令服务器 (Go + Gin)                        │
│  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐          │
│  │ 用户认证 │ │ 房间管理 │ │ 权限控制 │ │ 信令Hub │          │
│  └─────────┘ └─────────┘ └─────────┘ └─────────┘          │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│              SFU 媒体服务器 (Pion WebRTC)                      │
│  SFU选择转发模型：每个用户上行一路流，SFU转发给订阅的用户        │
│  相比Mesh：带宽降低 N 倍，支持更多人同时会议                    │
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

### 分层说明

1. **用户端层**：React 18 + TypeScript 开发，一套代码通过 Electron/Capacitor 打包到全平台
2. **接入层**：Nginx 反向代理，支持 SSL / WSS，负载均衡
3. **控制面**：Go + Gin，处理 HTTP API、WebSocket 信令、业务逻辑
4. **媒体面**：Pion WebRTC (纯Go) SFU服务器，处理音视频流转发
5. **基础设施**：PostgreSQL 持久化，Redis 缓存在线状态，Coturn 做 NAT 穿透

## 功能特性

### 核心功能

✅ **用户系统** - 手机号/邮箱注册登录，bcrypt加密存储，个人资料编辑，设备管理  
✅ **房间管理** - 支持临时/永久房间，密码/邀请码验证，房主可锁房/解散房间  
✅ **三级权限管理** - 房主(全部权限) → 管理员(kick/禁言) → 普通成员，踢人10分钟禁入  
✅ **SFU音视频通信** - 基于Pion WebRTC，支持摄像头/麦克风开关，回声消除/噪音抑制，自适应码率  
✅ **屏幕共享** - 支持全屏/指定窗口共享，可同时开启摄像头  
✅ **文本聊天** - 公聊+私聊，私聊AES端到端加密，支持图片/文件  
✅ **会议控制** - 举手发言、成员列表、@提醒、断线重连  
✅ **全链路加密** - DTLS/SRTP(音视频) + AES(聊天) + HTTPS/WSS(信令)  
✅ **跨平台** - Web/桌面/移动端一套代码，适配不同屏幕  

### 技术优势

- **完全匹配Go团队**：后端全Go，媒体面也是纯Go(Pion)，不用学Node.js/其他语言
- **SFU模型优势**：相比Mesh，上行带宽从 N* 降到 1*，支持500人同时会议
- **全链路安全**：端到端加密，服务器不存私聊明文，密码bcrypt哈希
- **容器化一键部署**：Docker Compose 启动所有服务，不用手动配置环境

## 开发过程中的难点与解决方案

### 1. SFU 模型理解和流转发问题

**难点**：
- SFU 相比 Mesh 概念理解难，不知道如何处理多用户之间的流转发
- 每个用户的轨道如何转发给其他用户，订阅机制怎么设计

**解决方案**：
- 当新用户加入，SFU 把已在房间中所有用户的轨道都添加给新用户
- 新用户发布自己的轨道后，SFU 把这个轨道添加给所有已在线用户
- 使用 Pion WebRTC 的 `OnTrack` 回调，创建本地 Track 转发，这样就是最简单有效的选择转发

### 2. WebRTC 信令同步问题

**难点**：
- offer/answer/candidate 这些信令怎么路由到正确的用户
- 多个房间多个用户情况下，如何高效广播

**解决方案**：
- 设计 Hub 结构维护每个房间中在线用户的 WebSocket 连接
- 通过 WebSocket 传输所有信令消息，每条消息带上 `target_id`
- 如果有目标ID就单发，否则广播给房间所有人
- 这样实现简单，逻辑清晰，不容易出问题

### 3. 权限控制设计问题

**难点**：
- 三级权限(房主/管理员/成员)的边界怎么划分
- 越权操作如何防止，比如普通成员能不能踢人

**解决方案**：
- 在数据库每个房间成员都存 role 字段
- 每个写操作接口都先检查权限：`CheckPermission(roomID, userID, requiredRole)`
- 管理员不能动房主和其他管理员，只有房主能设置管理员/转让房主，层级清晰
- 在前端也根据角色禁用操作按钮，双重校验

### 4. 跨平台打包问题

**难点**：
- Web/桌面/移动端都要支持，分别开发成本太高

**解决方案**：
- 前端一套 React 代码，通过 Vite 打包出 Web
- 通过 Electron 把 Web 打包成桌面端(Windows/Mac/Linux)
- 通过 Capacitor 把 Web 打包成移动端(iOS/Android)
- 不用写原生代码，一套代码跑全端，成本最低

### 5. 私聊端到端加密问题

**难点**：
- 要求服务器不存储明文，怎么加密才能保证安全性
- 密钥怎么交换

**解决方案**：
- 私聊消息发送前，客户端用 AES 加密，密钥双方协商
- 服务器只转发/存储密文，无法解密
- 公聊不加密(因为所有人都能看)，节省性能

## 二次开发建议

### 如何开始修改

1. **环境准备**
   ```bash
   # 安装 Go 1.21+、Node.js 18+、Docker
   cp .env.example .env
   # 启动所有基础设施（postgres、redis）
   docker-compose -f deployment/docker-compose.yml up -d postgres redis coturn
   ```

2. **后端开发**
   - 入口：[backend/cmd/server/main.go](file:///c:/Users/My_PC/myproject/conference_based_on_the-SFU%20model/backend/cmd/server/main.go)
   - 添加新 API：在 `backend/internal/handlers/` 加 handler，在 `main.go` 注册路由
   - 修改SFU逻辑：[backend/internal/sfu/server.go](file:///c:/Users/My_PC/myproject/conference_based_on_the-SFU%20model/backend/internal/sfu/server.go)
   - 运行：`go run backend/cmd/server/main.go` + `go run backend/cmd/sfu/main.go`

3. **前端开发**
   - 入口：[frontend/src/main.tsx](file:///c:/Users/My_PC/myproject/conference_based_on_the-SFU%20model/frontend/src/main.tsx)
   - 添加新页面：在 `frontend/src/pages/` 新建页面，在 `App.tsx` 加路由
   - 修改组件：在 `frontend/src/components/` 改
   - 运行：`cd frontend && npm run dev`

### 新增功能建议顺序

1. **先懂核心流程**：用户登录 → 创建房间 → 加入房间 → WebSocket连接 → WebRTC连接 → SFU转发
2. **从小改到大**：先改UI文案、样式，再加新功能
3. **P0 之后加P1**：现在核心功能齐了，可以加会议预约、邀请链接、虚拟背景这些非核心功能

### 代码结构保持

- `internal/models` → 放数据模型定义
- `internal/repository` → 放数据库操作
- `internal/service` → 放业务逻辑
- `internal/handlers` → 放HTTP API
- `internal/signaling` → 放WebSocket信令
- `internal/sfu` → 放媒体处理
- 保持这个分层，后续好维护

### 部署

- 修改完代码，重新构建镜像：`make docker-up`
- 查看日志：`make docker-logs`

## 许可证

MIT
