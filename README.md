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
- **SFU音视频通信核心** - 完整的WebRTC实现
  - ✅ ICE候选处理 - 支持双向ICE候选交换，真实NAT穿透
  - ✅ SDP Offer/Answer交换 - 通过WebSocket完成WebRTC协商
  - ✅ 媒体流转发 - Pion WebRTC SFU转发所有用户音视频流
  - ✅ TURN服务集成 - 已集成Coturn支持复杂网络穿透
- **SFU管理功能** - 通过gRPC强制静音，通过信令通知客户端

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

### 核心功能（高优先级）
| 功能 | 状态 | 说明 |
|------|------|------|
| ICE候选处理 | ❌ 未实现 | 前端onicecandidate是空函数，SFU也没有处理接口 |
| 远程流用户关联 | ❌ 未实现 | 所有远程流混在一起，无法区分是谁的视频 |
| 聊天消息WebSocket推送 | ❌ 未完成 | 消息通过HTTP发送，但WebSocket推送未实现 |
| E2EE私聊加密 | ❌ 未完成 | 发送/接收时没有实际调用加密函数 |

### 次要功能
| 功能 | 状态 | 说明 |
|------|------|------|
| SFU强制静音 | ⚠️ 空实现 | gRPC接口只是打log，没有操作媒体轨道 |
| 断线后WebRTC重连 | ⚠️ 缺失 | WebSocket重连后没有重建PeerConnection |
| TURN服务集成 | ⚠️ 未配置 | docker-compose有Coturn但WebRTC没使用 |
| 屏幕共享多轨道 | ⚠️ 未测试 | SFU转发逻辑需要验证 |

## 💡 开发建议

### 建议开发顺序
1. **先跑通核心** - 用 `sfu-test.html` 验证SFU媒体转发基本功能
2. **实现ICE候选** - 这是WebRTC在真实网络工作的关键
3. **修复远程流关联** - 能区分每个用户的视频流
4. **完善聊天推送** - 公聊能实时收到消息
5. **再考虑E2EE** - 核心功能通了再加加密

### 调试技巧
- 打开浏览器控制台查看WebRTC日志（chrome://webrtc-internals/）
- SFU服务日志会打印PeerConnection状态变化
- 建议先在简单网络环境测试（同一台机器），再测试跨机器

## 📄 许可证

MIT
