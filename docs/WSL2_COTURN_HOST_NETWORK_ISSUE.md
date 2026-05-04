# WSL2 环境下 Coturn Host 网络模式崩溃问题记录

## 1. 问题现象

### 发生环境
- Windows 10/11 + WSL2 (Ubuntu)
- Docker Desktop + WSL2 集成
- 项目：conference_based_on_the-sfu-model

### 具体表现

执行以下命令启动基础设施：
```bash
docker compose -f deployment/docker-compose.yml up -d postgres redis coturn
```

触发以下连锁反应：

1. **第一阶段：拉取镜像时开始异常**
   ```
   Error response from daemon: failed to resolve reference "docker.io/instrumentisto/coturn:latest":
   dialing registry-1.docker.io:443 container via direct connection because Docker Desktop has no HTTPS proxy:
   connecting to registry-1.docker.io:443: dial tcp [2a03:2880:f129:83:face:b00c:0:25de]:443: connectex:
   A connection attempt failed because the connected party did not properly respond after a period of time
   ```

2. **第二阶段：Docker Desktop 弹出错误提示**
   ```
   wsl distro proxy in Ubuntu distro has exited with an error:
   running wslexec: An error occurred while running the command.
   DockerDesktop/Wsl/ExecError: c:\windows\system32\wsl.exe -d ubuntu -u root -e
   /mnt/wsl/docker-desktop/docker-desktop-user-distro proxy --distro-name ubuntu
   --docker-desktop-root /mnt/wsl/docker-desktop c:\program files\docker\docker\resources:
   exit status 9 (stderr: , wslErrorCode: DockerDesktop/Wsl/ExecError)
   ```

3. **第三阶段：WSL2 完全失联**
   - VSCode WSL 连接断开
   - Windows 主机无法与 WSL2 建立任何新连接
   - 已存在的 SSH/终端会话全部断开
   - Docker Desktop 无法管理容器（启停、删除全部失败）
   - Docker Desktop 反复尝试重启 WSL2 集成，持续失败

4. **第四阶段：恢复方法**
   - 只能在 Windows 任务管理器中强制杀死 WSL2 相关进程
   - 或者直接重启 Windows 系统

---

## 2. 排查过程

### 第一步：排除网络问题

最初以为是 Docker Hub 无法访问，配置了镜像加速器后问题依然存在，排除单纯网络问题。

### 第二步：定位到具体服务

分别单独启动每个服务：
- `docker compose up -d postgres` → ✅ 正常
- `docker compose up -d redis` → ✅ 正常
- `docker compose up -d coturn` → ❌ 触发崩溃

**结论：问题与 coturn 服务直接相关。**

### 第三步：分析 coturn 配置差异

对比 coturn 与其他容器的配置：

| 配置项 | postgres/redis | coturn |
|--------|---------------|--------|
| networks | conference-net | 无 |
| network_mode | 无 | **host** |
| ports | 明确映射 | 有映射但被 host 模式忽略 |

**关键差异：coturn 使用了 `network_mode: host`。**

### 第四步：验证猜想

注释掉 `network_mode: host`，将 coturn 加入 `conference-net` 网络，重新启动：

```bash
docker compose up -d coturn
```

✅ 一切正常，不再崩溃。

---

## 3. Docker 网络模式详解

### 3.1 bridge 模式（桥接模式，默认）

**工作原理：**
```
┌─────────────────────────────────────────────────┐
│                     WSL2                        │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐      │
│  │ coturn   │  │ postgres │  │ redis    │      │
│  │ 容器     │  │ 容器     │  │ 容器     │      │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘      │
│       │             │             │             │
│       └─────────────┼─────────────┘             │
│                     │                           │
│            docker0 网桥 (172.17.0.1)           │
│                     │                           │
│            iptables NAT/端口映射                │
│                     │                           │
└─────────────────────┼───────────────────────────┘
                      │
              WSL2 eth0 网卡
```

**特点：**
- 每个容器有独立的网络命名空间
- 容器通过 docker0 虚拟网桥通信
- 外部访问需通过端口映射 (`-p 3478:3478`)
- 隔离性好，安全性高
- 性能略有损失（NAT 转发开销）

### 3.2 host 模式（主机模式）

**工作原理：**
```
┌─────────────────────────────────────────────────┐
│                     WSL2                        │
│                                                 │
│  coturn 容器直接使用 WSL2 的整个网络栈          │
│  ┌──────────────────────────────────────────┐   │
│  │ 监听 WSL2 所有网卡的所有端口              │   │
│  │ 直接操作 iptables 规则                    │   │
│  │ 创建大量网络连接/套接字                   │   │
│  └──────────────────────────────────────────┘   │
│                                                 │
│            没有隔离，没有 NAT 开销              │
└─────────────────────────────────────────────────┘
```

**特点：**
- 容器与主机共享同一个网络命名空间
- 容器直接使用主机的所有网卡
- 不需要端口映射，容器监听的端口直接就是主机端口
- 网络性能最好（零开销）
- 完全没有网络隔离，安全性低
- 可以直接操作主机的 iptables、路由表等

### 3.3 其他网络模式简述

| 模式 | 适用场景 |
|-----|---------|
| none | 容器完全没有网络，极致安全 |
| container | 与另一个容器共享网络命名空间 |
| macvlan | 容器有独立 MAC 地址，像物理机一样 |

---

## 4. 为什么 host 模式与 WSL2 不能很好配合

### 4.1 WSL2 的网络架构特殊性

WSL2 不是完整的虚拟机，它是一个**轻量级的虚拟化环境**，网络部分有特殊设计：

```
┌─────────────────────────────────────────────────────────────────┐
│                        Windows 主机                             │
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐  │
│  │                  Hyper-V 虚拟交换机                      │  │
│  │     (WSL2 与 Windows 通信的桥梁，由 Windows 管理)        │  │
│  └────────────────────┬────────────────────────────────────┘  │
│                       │                                         │
│  ┌────────────────────▼────────────────────────────────────┐  │
│  │                      WSL2 虚拟机                         │  │
│  │                                                         │  │
│  │  eth0 (虚拟网卡)  ←→  docker-desktop-user-distro 代理   │  │
│  │                           ↑                             │  │
│  │                Docker Desktop 注入的进程                │  │
│  │                负责 Windows ↔ WSL2 通信                 │  │
│  └─────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

### 4.2 冲突发生的具体机制

当 coturn 以 `network_mode: host` 启动时：

1. **大量端口监听**
   - Coturn 需要监听 UDP 端口范围：`49152-65535`（约 16000 个端口）
   - host 模式下，这些端口直接在 WSL2 的 eth0 上打开

2. **iptables 规则风暴**
   - Coturn 会添加大量 iptables NAT 和转发规则
   - 这些规则直接修改 WSL2 的内核网络栈

3. **触发 Docker Desktop 代理 Bug**
   - `docker-desktop-user-distro-proxy` 是 Windows 开发的进程，运行在 WSL2 中
   - 它负责转发 Windows ↔ WSL2 的所有网络流量
   - 当 Coturn 突然创建上万个套接字并修改大量 iptables 规则时
   - **这个代理进程遇到了未处理的边界情况，直接崩溃（exit status 9）**

4. **雪崩效应**
   - 代理进程崩溃 → Windows 与 WSL2 之间的通信通道中断
   - VSCode 连接断开、Docker Desktop 无法管理容器
   - 由于通信通道断了，Docker Desktop 也无法正常关闭或重启 WSL2

---

## 5. 解决方法

### 5.1 方案一：桥接模式（推荐，开发环境）

**修改 `deployment/docker-compose.yml`：**

```yaml
coturn:
  image: instrumentisto/coturn:latest
  container_name: conference-coturn
  ports:
    - "3478:3478"
    - "3478:3478/udp"
    # 缩小端口范围，桥接模式下不需要那么多
    - "49152-49200:49152-49200/udp"
  environment:
    - TURN_STATIC_AUTH_SECRET=your-turn-secret-change-in-production
    - TURN_USER=username
    - TURN_PWD=password
  # 移除 network_mode: host
  networks:
    - conference-net  # 加入统一网络
  restart: unless-stopped
```

**为什么能解决问题：**
- coturn 现在有独立的网络命名空间，不再直接操作 WSL2 内核网络栈
- 所有网络操作都被 Docker 的桥接网络抽象层隔离
- 不会再触发 `docker-desktop-user-distro-proxy` 的 Bug
- 开发环境下，几十人的小规模测试完全够用

**权衡：**
- ✅ 稳定，不会崩溃
- ✅ 开发测试完全够用
- ❌ 极端 NAT 环境下穿透率可能略有降低
- ❌ 网络性能有可忽略的微小损失

### 5.2 方案二：WSL2 原生安装 Coturn（生产级稳定）

不在 Docker 中跑 coturn，直接在 WSL2 系统中安装：

```bash
sudo apt update
sudo apt install -y coturn

# 配置
sudo tee /etc/turnserver.conf << EOF
listening-port=3478
fingerprint
user=username:password
realm=conference
EOF

# 启动
sudo systemctl enable coturn
sudo systemctl start coturn
```

**为什么能解决问题：**
- 没有 Docker 层，不会触发 Docker Desktop WSL 集成的 Bug
- 依然能享受到 host 网络的最佳性能

### 5.3 方案三：原生 Linux 环境（生产部署推荐）

如果是生产环境，直接在原生 Linux 服务器上部署：
- 不会有 WSL2 的这些问题
- `network_mode: host` 可以正常使用，性能最佳

---

## 总结

| 环境 | 推荐网络模式 |
|------|------------|
| WSL2 + Docker Desktop 开发 | bridge 模式（稳定优先） |
| 原生 Linux 开发/生产 | host 模式（性能优先） |
| Windows 原生 Docker | bridge 模式 |

**这个问题的本质是：Docker Desktop for Windows 的 WSL2 集成组件在面对 host 网络模式下 Coturn 大规模网络操作时存在稳定性缺陷，不是 Coturn 本身的问题，也不是 WSL2 的问题。**
