# 技术设计文档

## 目录
1. [SFU 控制接口 - gRPC 实现](#1-sfu-控制接口---grpc-实现)
2. [SFU 集群部署方案](#2-sfu-集群部署方案)
3. [选择性订阅功能](#3-选择性订阅功能)
4. [QoS 机制设计](#4-qos-机制设计)
5. [Redis 热点数据缓存](#5-redis-热点数据缓存)

---

## 1. SFU 控制接口 - gRPC 实现

### 1.1 设计背景

当前项目中，API 服务器需要向 SFU 服务器发送控制指令（如踢人、静音等），但之前的实现使用的是 HTTP 回调方式，存在以下问题：

- 安全问题：SFU 直接向 API 暴露回调接口
- 耦合性高：SFU 需要知道 API 服务器的地址
- 类型不安全：HTTP + JSON 没有编译时类型检查
- 媒体层与信令层分离但控制不完整

### 1.2 gRPC 服务定义

**文件**: `backend/api/proto/sfu_control.proto`

```protobuf
service SFUControl {
  // Kick a user from the room
  rpc KickUser(KickUserRequest) returns (KickUserResponse);

  // Mute/Unmute a user's audio
  rpc MuteUser(MuteUserRequest) returns (MuteUserResponse);

  // Enable/Disable a user's video
  rpc SetVideoEnabled(SetVideoEnabledRequest) returns (SetVideoEnabledResponse);

  // Get room statistics
  rpc GetRoomStats(GetRoomStatsRequest) returns (GetRoomStatsResponse);

  // Get all rooms on this SFU node
  rpc ListRooms(ListRoomsRequest) returns (ListRoomsResponse);

  // Force close a specific peer connection
  rpc ClosePeerConnection(ClosePeerConnectionRequest) returns (ClosePeerConnectionResponse);
}
```

### 1.3 架构图

```
┌─────────────────────┐
│   API 服务端        │
│  (Gin + 业务逻辑)   │
└─────────┬───────────┘
          │ gRPC 客户端调用
          ▼
┌─────────────────────┐
│   SFU 媒体服务      │
│  (gRPC Server)      │
│  ├─ KickUser        │
│  ├─ MuteUser        │
│  └─ GetRoomStats    │
└─────────────────────┘
```

### 1.4 关键代码位置

| 文件 | 说明 |
|------|------|
| `backend/internal/sfu/grpc_server.go` | SFU gRPC 服务端实现 |
| `backend/internal/sfu/grpc_client.go` | API 端 gRPC 客户端实现 |
| `backend/api/proto/sfu_control.proto` | Protocol Buffer 定义 |
| `backend/internal/handlers/room.go` | KickMember 中调用 gRPC |

### 1.5 配置项

在环境变量中添加：
```bash
SFU_GRPC_PORT=50051      # SFU gRPC 服务端口
SFU_ADDR=localhost:50051  # API 连接 SFU 的地址
```

---

## 2. SFU 集群部署方案

### 2.1 问题背景

当前 SFU 是单节点架构，存在以下限制：
- 单节点资源上限：CPU/带宽无法支持大规模会议
- 单点故障：SFU 宕机会导致所有会议中断
- 扩展性差：无法根据负载动态扩容

### 2.2 架构设计

#### 整体架构

```
                              ┌──────────────┐
                              │   客户端      │
                              └──────┬───────┘
                                     │
                                     ▼
                              ┌──────────────┐
                              │   API 网关    │
                              └──────┬───────┘
                                     │
         ┌───────────────────────────┼───────────────────────────┐
         │                           │                           │
         ▼                           ▼                           ▼
┌──────────────────┐     ┌──────────────────┐     ┌──────────────────┐
│   SFU Node 1     │     │   SFU Node 2     │     │   SFU Node 3     │
│  (gRPC + WebRTC) │     │  (gRPC + WebRTC) │     │  (gRPC + WebRTC) │
└────────┬─────────┘     └────────┬─────────┘     └────────┬─────────┘
         │                         │                         │
         └─────────────────────────┼─────────────────────────┘
                                   │
                                   ▼
                        ┌───────────────────────┐
                        │ Redis 集群 (共享状态) │
                        │  - 房间路由表          │
                        │  - 用户-SFU映射        │
                        │  - 节点健康状态        │
                        └───────────────────────┘
```

### 2.3 核心组件

#### 2.3.1 房间路由管理器

**职责**: 分配房间到 SFU 节点，维护房间与 SFU 的映射关系

```go
// RoomRouter 房间路由器
type RoomRouter struct {
    redis *redis.Client
}

// 分配房间到 SFU 节点
func (r *RoomRouter) AssignRoomToSFU(roomID uint) (string, error) {
    // 策略: 选择负载最低的节点
    nodes, _ := r.GetHealthyNodes()
    return pickLeastLoadedNode(nodes)
}

// 获取房间所在的 SFU 节点
func (r *RoomRouter) GetSFUForRoom(roomID uint) (string, error) {
    return r.redis.Get(ctx, "room:sfu:"+string(roomID)).Result()
}
```

#### 2.3.2 节点健康检查

```go
// SFU 节点上报心跳
func (s *SFUServer) reportHeartbeat() {
    ticker := time.NewTicker(5 * time.Second)
    for range ticker.C {
        // 上报负载信息
        stats := map[string]interface{}{
            "cpu_usage":    getCPUUsage(),
            "memory_usage": getMemoryUsage(),
            "room_count":   len(s.rooms),
            "peer_count":   getTotalPeers(s.rooms),
        }
        s.redis.HSet(ctx, "sfu:nodes:"+s.nodeID, stats)
        s.redis.Expire(ctx, "sfu:nodes:"+s.nodeID, 15*time.Second)
    }
}
```

### 2.4 负载均衡策略

| 策略 | 适用场景 | 优点 | 缺点 |
|------|---------|------|------|
| **最少连接** | 通用场景 | 简单有效，负载均匀 | 未考虑服务器性能差异 |
| **加权轮询** | 服务器性能不均 | 可根据硬件配置分配 | 需要预先设置权重 |
| **资源利用率** | 大规模部署 | 基于真实负载分配 | 实现复杂，监控开销大 |

**推荐**: 项目初期使用 **最少连接** 策略，后期可扩展为资源利用率策略。

### 2.5 故障转移机制

#### 场景：SFU 节点宕机

1. **检测阶段**: Redis 心跳超时（>15秒），节点被标记为不健康
2. **通知阶段**: API 服务器收到房间迁移事件，通知所有客户端
3. **重连阶段**: 客户端断开 WebRTC 连接，重新连接新的 SFU 节点
4. **重建阶段**: 新 SFU 节点重建所有 PeerConnection 和媒体流

```go
// 故障转移流程伪代码
func handleNodeFailure(nodeID string) {
    // 1. 获取该节点上的所有房间
    rooms := getRoomsOnNode(nodeID)

    // 2. 重新分配房间到健康节点
    for _, roomID := range rooms {
        newNode := router.AssignRoomToSFU(roomID)
        // 通知 API 服务器触发客户端重连
        notifyClientsToReconnect(roomID, newNode)
    }
}
```

### 2.6 部署配置

**Docker Compose 集群配置示例**:
```yaml
services:
  sfu-1:
    build: ./backend
    command: ./sfu
    environment:
      - SFU_NODE_ID=sfu-1
      - REDIS_ADDR=redis:6379
      - SFU_GRPC_PORT=50051

  sfu-2:
    build: ./backend
    command: ./sfu
    environment:
      - SFU_NODE_ID=sfu-2
      - REDIS_ADDR=redis:6379
      - SFU_GRPC_PORT=50051

  redis:
    image: redis:7-alpine
```

---

## 3. 选择性订阅功能

### 3.1 设计背景

当前 SFU 实现中，用户加入房间后会收到所有其他用户的音视频流，存在以下问题：
- **带宽浪费**: 大屏幕模式只看主讲人，但收到所有人的视频
- **性能开销**: 解码过多视频占用 CPU/GPU 资源
- **灵活性差**: 无法根据网络状况动态调整

### 3.2 功能定义

选择性订阅（Selective Forwarding）允许客户端：
1. 指定订阅哪些用户的音视频流
2. 动态调整视频质量层级（Simulcast）
3. 暂停/恢复特定用户的流

### 3.3 架构设计

```
客户端 A                     SFU                      客户端 B
   │                          │                          │
   │  发布 3 层 simulcast     │                          │
   │─────────────────────────>│                          │
   │  (720p, 360p, 180p)     │                          │
   │                          │                          │
   │                          │ <─────────────────────────│
   │                          │      SubscribeRequest    │
   │                          │  (订阅 A 的 360p 流)    │
   │                          │                          │
   │                          │ ─────────────────────────>
   │                          │      转发 360p 流        │
```

### 3.4 核心数据结构

```go
// Subscription 订阅信息
type Subscription struct {
    UserID     uint   `json:"user_id"`
    StreamID   string `json:"stream_id"`
    TrackID    string `json:"track_id"`
    Quality    string `json:"quality"`  // "high", "medium", "low"
    Enabled    bool   `json:"enabled"`
    MutedAudio bool   `json:"muted_audio"`
    MutedVideo bool   `json:"muted_video"`
}

// SubscribeRequest 订阅请求
type SubscribeRequest struct {
    RoomID        uint           `json:"room_id"`
    SubscriberID  uint           `json:"subscriber_id"`
    Subscriptions []Subscription `json:"subscriptions"`
}
```

### 3.5 信令协议扩展

在现有的 WebSocket 信令中添加以下消息类型：

```json
// 客户端发送订阅请求
{
  "type": "subscribe",
  "data": {
    "room_id": 123,
    "subscriptions": [
      {
        "user_id": 456,
        "quality": "medium",
        "muted_video": false
      }
    ]
  }
}

// SFU 响应
{
  "type": "subscribed",
  "data": {
    "success": true,
    "active_subscriptions": [...]
  }
}
```

### 3.6 SFU 实现要点

#### 3.6.1 Simulcast 支持

```go
// 根据订阅的质量层级，选择对应的 RTP 包转发
func (s *SFUServer) forwardByQuality(
    subscriber *Peer,
    publisher *Peer,
    quality string,
) error {
    track := publisher.GetTrackByQuality(quality)
    if track == nil {
        track = publisher.GetLowestQualityTrack()
    }
    return subscriber.AddTrack(track)
}
```

#### 3.6.2 动态切换质量

当网络状况变化时，客户端可以请求降低/提升视频质量：

```go
func (s *SFUServer) handleQualityChange(
    roomID uint,
    subscriberID uint,
    publisherID uint,
    newQuality string,
) error {
    // 1. 停止当前质量的转发
    // 2. 启动新质量的转发
    // 3. 发送关键帧请求，让编码器立即生成关键帧
    // 4. 通知订阅者切换完成
}
```

### 3.7 带来的变化

| 指标 | 无选择性订阅 | 有选择性订阅 | 改进 |
|------|------------|------------|------|
| 10人会议带宽 | ~20 Mbps | ~2-5 Mbps | **减少 75-90%** |
| 客户端 CPU 占用 | 高（解码10路） | 低（解码1-3路） | **降低 70%+** |
| 弱网适应性 | 差（卡顿严重） | 好（自动降级） | **体验大幅提升** |

---

## 4. QoS 机制设计

### 4.1 当前问题

当前实现没有任何 QoS 保障：
- 丢包会导致视频花屏、卡顿
- 网络抖动会影响播放流畅性
- 没有拥塞控制，严重时会雪崩

### 4.2 QoS 机制分层

```
┌───────────────────────────────────────────────────────┐
│                   应用层 QoS 策略                       │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐ │
│  │  自适应码率  │  │  关键帧重传  │  │  拥塞控制    │ │
│  │   (ABR)      │  │    (RTX)     │  │   (GCC)      │ │
│  └──────────────┘  └──────────────┘  └──────────────┘ │
├───────────────────────────────────────────────────────┤
│                   WebRTC 传输层                        │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐ │
│  │  Jitter Buf  │  │   NACK       │  │  FEC         │ │
│  │  (抖动缓冲)  │  │  (丢包重传)  │  │  (前向纠错)  │ │
│  └──────────────┘  └──────────────┘  └──────────────┘ │
└───────────────────────────────────────────────────────┘
```

### 4.3 核心机制详解

#### 4.3.1 NACK (Negative Acknowledgment)

**作用**: 接收端检测到丢包时，向发送端请求重传丢失的包

```go
// NACK 处理逻辑
func handleNACK(peer *Peer, lostSeqNums []uint16) {
    for _, seq := range lostSeqNums {
        // 1. 在重传缓存中查找该数据包
        if pkt := peer.retransmitBuffer.Get(seq); pkt != nil {
            // 2. 重传数据包
            peer.sendRTP(pkt)
        } else {
            // 3. 缓存中不存在，请求关键帧
            peer.sendPLI()  // Picture Loss Indication
        }
    }
}
```

**配置参数**:
```go
nackConfig := webrtc.Configuration{
    ICEServers: []webrtc.ICEServer{...},
    // NACK 相关设置
    RTCPReports: true,
    NACKEnabled: true,
}
```

#### 4.3.2 FEC (Forward Error Correction)

**作用**: 发送冗余数据包，接收端可以在不重传的情况下恢复丢包

```go
// FEC 策略配置
type FECConfig struct {
    Enabled           bool
    ProtectionFactor  int    // 每 N 个包发送 1 个 FEC 包
    MaxOverhead       float64 // 最大带宽开销比例 (0.1 = 10%)
    Adaptive          bool   // 自适应调整保护级别
}

// 根据丢包率动态调整 FEC 级别
func adaptiveFEC(packetLossRate float64) FECConfig {
    switch {
    case packetLossRate < 0.01:
        return FECConfig{Enabled: false}
    case packetLossRate < 0.05:
        return FECConfig{Enabled: true, ProtectionFactor: 8} // 每 8 包 1 个 FEC
    default:
        return FECConfig{Enabled: true, ProtectionFactor: 4} // 每 4 包 1 个 FEC
    }
}
```

#### 4.3.3 RTCP 报告与拥塞控制

**发送端报告 (SR)**:
```
┌─────────────────────────────────────────────────────────┐
│ NTP Timestamp (64位) │ RTP Timestamp (32位) │ SSRC (32位) │
├─────────────────────────────────────────────────────────┤
│ 发送包数 │ 发送字节数 │ 报告块 1 │ ... │ 报告块 N        │
└─────────────────────────────────────────────────────────┘
```

**接收端报告 (RR)**:
- 丢包率 (Fraction Lost)
- 累计丢包数 (Cumulative Number of Packets Lost)
- 扩展最高序列号 (Extended Highest Sequence Number)
- 抖动 (Jitter)

### 4.4 实施优先级

| 优先级 | 机制 | 实施难度 | 收益 |
|-------|------|---------|------|
| P0 | NACK | 低 | 高（丢包时快速恢复） |
| P1 | Jitter Buffer | 低 | 中（消除抖动影响） |
| P2 | FEC (ULP/RED) | 中 | 高（弱网下大幅提升） |
| P3 | GCC 拥塞控制 | 高 | 高（防止拥塞雪崩） |
| P4 | 自适应码率 (ABR) | 高 | 中（动态匹配带宽） |

**推荐实施顺序**: 先实现 NACK + Jitter Buffer（P0-P1），这两个改进成本低、收益高。

### 4.5 配置建议

| 网络状况 | NACK | FEC | 目标比特率 | 缓冲时长 |
|---------|------|-----|----------|---------|
| 理想 (<1% 丢包) | ✅ | ❌ | 2-4 Mbps | 100ms |
| 一般 (1-5% 丢包) | ✅ | ✅ | 1-2 Mbps | 200ms |
| 较差 (5-15% 丢包) | ✅ | ✅✅ | 500-1000 kbps | 400ms |
| 恶劣 (>15% 丢包) | ✅ | ✅✅✅ | 200-500 kbps | 600ms |

---

## 5. Redis 热点数据缓存

### 5.1 设计目标

降低 PostgreSQL 数据库压力，提高高频查询的响应速度：
- 用户信息查询（每次 API 请求都需要）
- 房间信息查询（加入房间、获取房间信息）
- 用户房间列表（获取我的房间）

### 5.2 缓存策略

| 数据类型 | Key 格式 | TTL | 说明 |
|---------|---------|-----|------|
| 用户信息 | `user:{id}` | 15 分钟 | 个人资料不常变 |
| 房间信息 | `room:info:{id}` | 10 分钟 | 房间配置变化少 |
| 用户房间列表 | `user:rooms:{id}` | 5 分钟 | 查询频率极高 |
| 房间成员 | `room:{id}` | - | 已存在，实时更新 |
| 在线状态 | `online:{id}` | - | 已存在，实时更新 |

### 5.3 缓存读写模式

采用 **Cache-Aside Pattern**（旁路缓存）:

```go
// 读取流程
func GetUser(userID uint) (*User, error) {
    // 1. 先查缓存
    if cached := redis.GetUser(userID); cached != nil {
        return cached, nil
    }

    // 2. 缓存未命中，查数据库
    user, err := db.FindByID(userID)
    if err != nil {
        return nil, err
    }

    // 3. 写入缓存
    redis.SetUser(user)
    return user, nil
}

// 更新流程
func UpdateUser(user *User) error {
    // 1. 先更新数据库
    if err := db.Update(user); err != nil {
        return err
    }

    // 2. 再失效缓存
    return redis.DeleteUser(user.ID)
}
```

### 5.4 关键代码位置

| 文件 | 说明 |
|------|------|
| `backend/internal/repository/user.go` | Redis 缓存方法实现 |
| `backend/internal/service/user.go` | 用户服务集成缓存 |
| `backend/internal/service/room.go` | 房间服务集成缓存 |

### 5.5 预期效果

| 指标 | 无缓存 | 有缓存 | 改进 |
|------|--------|--------|------|
| GetUserByID 响应时间 | ~20-50ms | <1ms | **提升 20-50 倍** |
| GetRoomByID 响应时间 | ~20-50ms | <1ms | **提升 20-50 倍** |
| 数据库 QPS（1000 并发用户） | ~3000 | ~500 | **降低 83%** |

---

## 总结

本技术文档描述了以下关键改进：

1. ✅ **gRPC SFU 控制接口** - 解决安全、耦合、类型安全问题
2. 📋 **SFU 集群方案** - 支持横向扩展，解决单点故障
3. 📋 **选择性订阅** - 大幅降低带宽和 CPU 占用，支持 Simulcast
4. 📋 **QoS 机制** - NACK/FEC/GCC，提升弱网下的体验
5. ✅ **Redis 热点缓存** - 降低数据库压力，提升 API 响应速度

其中，标记为 ✅ 的已经实现，标记为 📋 的为设计方案，可在后续版本中逐步实施。
