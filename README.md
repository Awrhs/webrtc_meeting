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
docker-compose up -d
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

## 许可证

MIT
