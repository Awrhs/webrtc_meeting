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
│   ├── docker-compose.yml  # Docker 服务编排
│   ├── nginx.conf          # Nginx 反向代理配置（生产环境用）
│   └── docker/             # Dockerfile 目录
│       ├── Dockerfile.frontend
│       ├── Dockerfile.server
│       └── Dockerfile.sfu
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

#### 方式一：Docker 方式（推荐，最简单）
```bash
cd deployment
docker-compose up -d postgres redis coturn
docker-compose up --build api-server sfu-server frontend
```

访问 http://localhost:3000

---

#### 方式二：WSL2 原生运行（适合开发调试）

##### 1. 启动基础设施
```bash
cd deployment
docker-compose up -d postgres redis coturn
```

##### 2. 获取 WSL2 的真实 IP
在 Windows PowerShell 中：
```powershell
wsl.exe hostname -I
```
记下这个 IP（例如 `172.25.24.77`）

##### 3. 配置前端环境变量
编辑 `frontend/.env`：
```env
VITE_API_HOST=http://172.25.24.77:8080
VITE_SFU_HOST=http://172.25.24.77:8081
```

##### 4. 编译 gRPC 代码（首次运行）
```bash
cd backend
make install-tools  # 安装protoc等工具
make proto          # 生成gRPC代码
```

##### 5. 启动API服务（终端1）
```bash
cd backend
go run cmd/server/main.go
```

##### 6. 启动SFU服务（终端2，**必须加 WSL2_COMPAT**）
```bash
cd backend
WSL2_COMPAT=1 go run cmd/sfu/main.go
```

##### 7. 启动前端（终端3）
```bash
cd frontend
npm install
npm run dev
```

**访问：http://localhost:3000**（如果前端在 Windows 运行）
或 **http://172.25.24.77:3000**（如果前端在 WSL2 运行）

---

### 🔬 SFU功能测试（重要！）

**推荐先使用极简测试页面验证SFU核心功能：**

```
访问: http://localhost:3000/sfu-test.html
```

测试步骤：
1. 打开两个浏览器窗口，都访问测试页面
2. 两个窗口都点击 **"1. 开启本地媒体"**
3. 第一个窗口用户ID填 `1`，第二个窗口用户ID填 `2`
4. 两个窗口房间ID都填相同数字（默认 `1`）
5. 依次点击 **"2. 连接SFU"**
6. 观察状态面板和视频流，验证是否能互相看到对方

---

## 🐛 WSL2 环境 SFU ICE 连接问题诊断

### 📌 问题背景与核心原理

**WSL2 网络架构：**
```
Windows 主机 (浏览器)
    │
    │ 虚拟网络交换机 (WSL Virtual Switch)
    ├─ Windows IP: 172.25.16.1
    │
    │ 路由可达 (不是端口映射NAT！)
    │
    └─ WSL2 虚拟机
         └─ eth0 IP: 172.25.24.77 (例子)
```

**关键事实（通过实验验证）：**
- ✅ Windows 能直接访问 WSL2 的真实 IP（172.x.x.x），包括 TCP **和 UDP**
- ❌ `127.0.0.1` 在 Windows 和 WSL2 里是不同的网络命名空间，互不相通
- ⚠️ 微软的 localhost 转发**只针对 TCP**，**不包括 UDP**（WebRTC用UDP）

**之前的错误理解（已修正）：**
- ❌ 旧代码让 SFU 公布 127.0.0.1 → 浏览器连自己去了，根本到不了 WSL2
- ✅ 正确做法：让 SFU 公布 WSL2 的真实 IP（172.x.x.x）

---

### ✅ 正确的解决方案

#### 步骤 1：获取 WSL2 的真实 IP

在 **Windows PowerShell** 中运行（注意是大写 i）：
```powershell
wsl.exe hostname -I
```

输出示例：
```
172.25.24.77
```

**注意：**
- `wsl hostname -i`（小写 i）返回 127.0.1.1，这是错的！
- `wsl hostname -I`（大写 i）返回真实的 172.x.x.x，这才是对的！

---

#### 步骤 2：配置前端环境变量

编辑 `frontend/.env`：
```env
# 用你刚才获取到的 WSL2 IP
VITE_API_HOST=http://172.25.24.77:8080
VITE_SFU_HOST=http://172.25.24.77:8081
```

---

#### 步骤 3：启用 WSL2 兼容模式启动 SFU

在 WSL2 中：
```bash
cd backend
WSL2_COMPAT=1 go run cmd/sfu/main.go
```

✅ **确认日志中出现类似这行：**
```
{"level":"info", ... ,"msg":"WSL2 compatibility mode enabled",
    "wsl2_ip":"172.25.24.77",
    "note":"use this IP in frontend VITE_SFU_HOST"}
```

**WSL2_COMPAT 会自动：**
- 检测 WSL2 的 eth0 IP（172.x.x.x）
- 通过 `SetNAT1To1IPs` 让 SFU 公布这个真实 IP
- 允许所有网络接口

---

#### 步骤 4：验证 ICE 候选是否正确

当有用户连接时，SFU 日志中应该看到：
```
{"level":"debug", ... ,"msg":"ICE candidate generated",
    "userID":1,
    "candidate":"candidate:1 1 udp 1234567 172.25.24.77 50000 typ host",
    "address":"172.25.24.77",
    "port":50000}
```

**关键点：候选 IP 是 WSL2 的真实 IP（172.x.x.x），不是 127.0.0.1！**

---

#### 步骤 5：检查浏览器端

打开 `edge://webrtc-internals/` 或 `chrome://webrtc-internals/`：
- ✅ **正常情况**：`ICE candidate grid` 中应该看到 172.x.x.x 的候选
- ✅ **连接状态**：应该变成 `connected` 或 `completed`
- ❌ **异常情况**：候选是 127.0.0.1 或连接卡住

---

### 🐛 如果还是连不上，排查清单

按以下顺序检查：

1. [ ] 确认你用 `wsl hostname -I`（大写 i）获取的 IP
2. [ ] 确认 frontend/.env 里的 IP 和 SFU 日志显示的一致
3. [ ] 确认启动 SFU 时加了 `WSL2_COMPAT=1`
4. [ ] 确认浏览器访问的不是 `localhost:3000`，而是 `http://172.25.24.77:3000`（如果Vite也在WSL2）
5. [ ] 检查 WSL2 防火墙有没有阻止 UDP 端口（通常不需要）
6. [ ] 用简单的 UDP 测试程序验证：
   ```go
   // 在WSL2运行，Windows发UDP到172.x.x.x:49152测试
   ```

---

### 📋 两种开发模式对比

| 模式 | 优点 | 缺点 | 推荐度 |
|------|------|------|--------|
| **WSL2 原生运行** | 调试方便，编译快 | 需要手动配置 IP | ⭐⭐⭐⭐ |
| **Docker Desktop** | IP 固定（localhost），端口映射处理好了 | 编译稍慢 | ⭐⭐⭐⭐⭐ |

---

### 🔧 Docker 模式的配置

如果不想处理 WSL2 IP，直接用 Docker：

```bash
cd deployment
docker-compose up -d postgres redis coturn
docker-compose up --build api-server sfu-server frontend
```

然后浏览器访问 `http://localhost:3000`，不需要改任何 IP！

**Docker 模式的原理：**
- Docker Desktop 做了真正的全协议端口映射（TCP+UDP）
- SFU 用 `FORCE_LOOPBACK_CANDIDATE=1` 公布 127.0.0.1
- 浏览器连 localhost 的 UDP 端口能被转发到容器里
6. [ ] `webrtc-internals` 中的候选对状态是什么？
7. [ ] 是否有 `STUN binding request` 相关的错误？

**将以上 7 点的检查结果附在 Issue 中，可以大大加速问题定位。**

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

> **关于 E2EE 的特别说明**：
>
> ✅ **已实现 Sender Keys E2EE（仅文本消息）：
> - 每个会话在浏览器独立生成 ECDH P-256 密钥对
> - 通过 WebSocket 广播公钥交换
> - 两两密钥仅保存在浏览器，后端无法计算
> - 消息发送前本地加密，接收后本地解密
>
> ⚠️ **与理想群聊 E2EE 的差距：
> 1. **媒体流未加密：音频/视频仍由 SFU 明文转发
> 2. **无成员变更密钥轮换：加入/退出时不重新生成组密钥
> 3. **元数据可见**：服务器可进行流量分析，可阻止/篡改信令
> 4. **公钥无信任根**：依赖线下比对指纹，无证书机制
> 5. **无双棘轮缺失**：仅支持 Sender Keys 单棘轮，不支持 MLS 全双工棘轮

## 📄 许可证

MIT
