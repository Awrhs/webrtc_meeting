# 局域网试用部署指南（一台电脑当服务端，同一局域网的几台电脑/手机入会）

本文覆盖"没有域名、没有云服务器"的试用场景：局域网里一台电脑运行全部服务端（Docker Compose），
其他电脑和手机通过浏览器访问。公网部署只是把地址和证书换掉，最后一节单独说明。

## 为什么必须是 HTTPS

浏览器只在 `localhost` 或 **HTTPS 页面**上开放摄像头/麦克风（`getUserMedia`），也只允许 HTTPS 页面
发起 `wss://`。所以除了在服务端电脑本机用 `http://localhost:3000` 自测之外，局域网里其他电脑访问
`http://192.168.x.x:3000` 一定会拿不到摄像头。`deployment/docker-compose.tls.yml` 叠加文件让 nginx 在
443 端口统一终结 TLS，前端、API、信令 WebSocket、SFU WebSocket 全部走同一个 `https://<宿主机IP>`。

## 前置条件

- 服务端电脑：Docker 与 Docker Compose **v2.24 及以上**（叠加文件用到了 `!reset` / `!override`）。
  查看版本：`docker compose version`。
- 服务端电脑有一个**固定的局域网 IP**（路由器里给它绑定 DHCP 静态分配最省事），下文记为 `192.168.1.10`。
- 服务端电脑防火墙放行：TCP 80/443、TCP+UDP 3478、UDP 49152-49200（SFU 媒体）、UDP 49300-49400（TURN 中继）。
- 客户端：Chrome / Edge / Firefox / Safari 均可；Chromium 系对 Simulcast 与媒体 E2EE 支持最完整。

## 第一步：准备环境变量

```bash
cp .env.example .env
```

至少修改这些项（其余保持默认即可）：

| 变量 | 说明 |
|------|------|
| `PUBLIC_HOST` | 服务端电脑的局域网 IP，例如 `192.168.1.10`。前端会用它拼 `https://192.168.1.10` 和 `wss://192.168.1.10/sfu/ws`，CORS 白名单、TURN 地址、SFU 对外 IP 也都默认取它 |
| `JWT_SECRET`、`SFU_JWT_SECRET`、`SFU_CONTROL_TOKEN`、`SFU_CALLBACK_TOKEN` | 各自换成随机串（`openssl rand -hex 32`） |
| `DB_PASSWORD`、`REDIS_PASSWORD`、`TURN_STATIC_AUTH_SECRET` | 同上。`REDIS_PASSWORD` 非空时 Redis 自动启用密码 |
| `GO_ENV` | 试用阶段保持 `development` 可以看到更多日志；`production` 会强制要求上面的 secret 不是默认值 |
| `MAIL_PROVIDER` | 试用阶段保持 `log`：找回密码 / 邮箱验证的验证码打印在 `api-server` 日志里（`docker compose logs api-server`）。真要发邮件就填 `smtp` 和 `SMTP_*` |
| `STUN_URLS` | 局域网能上网就保持默认；**完全没有外网**的内网把它设为 `none`，否则每次入会要白等几秒 STUN 超时 |

`SFU_PUBLIC_IP`、`TURN_URLS`、`TURN_EXTERNAL_IP`、`ALLOWED_ORIGINS` 在 TLS 叠加文件里都会从 `PUBLIC_HOST` 推导，不用手填。

## 第二步：生成自签名证书

```bash
deployment/tls/gen-self-signed.sh 192.168.1.10
```

脚本生成 `deployment/tls/server.crt` 与 `server.key`（已被 `.gitignore` 忽略，不会误提交），
证书的 SAN 里同时包含这个 IP 与 `localhost`。想同时用一个局域网域名（比如路由器里配了 `meeting.lan`）
就把它也加上：`gen-self-signed.sh 192.168.1.10 meeting.lan`。

## 第三步：启动

```bash
export PUBLIC_HOST=192.168.1.10     # 或者写进 .env
docker compose -f deployment/docker-compose.yml -f deployment/docker-compose.tls.yml up -d --build
docker compose -f deployment/docker-compose.yml -f deployment/docker-compose.tls.yml ps
```

所有服务 `healthy` 后，在服务端电脑上访问 `https://192.168.1.10`（会提示证书不受信任，先点"继续访问"）。
数据库表结构由 `api-server` 启动时的内嵌迁移自动创建，不需要手动导入 SQL。

## 第四步：让每台客户端信任证书

浏览器对自签名证书的"继续访问"只对页面本身有效，**WebSocket 往往仍会被拦**，所以要把 `server.crt`
装进每台客户端的系统信任库（把文件用微信/U 盘/局域网共享发过去）：

- **Windows**：双击 `server.crt` → 安装证书 → 本地计算机 → 将所有证书放入下列存储 → "受信任的根证书颁发机构"。重启浏览器。
- **macOS**：双击导入"钥匙串访问"→ 系统 → 找到证书 → 显示简介 → 信任 → "始终信任"。
- **iOS**：把 `server.crt` 用 AirDrop/邮件发到手机 → 设置 → 已下载描述文件 → 安装 → 再到 设置 → 通用 → 关于本机 → 证书信任设置 → 打开开关。
- **Android**：设置 → 安全 → 加密与凭据 → 安装证书 → CA 证书 → 选择文件（不同厂商路径略有差异）。
- **Firefox**（桌面）有自己的证书库：设置 → 隐私与安全 → 证书 → 查看证书 → 证书颁发机构 → 导入，勾选"信任由此证书颁发机构来标识网站"。

然后访问 `https://192.168.1.10`，注册两个账号在两台设备上登录，一方创建房间、另一方在首页输入房间号
或直接打开会议链接。顶部状态标签显示"媒体已连接"即表示 SFU 媒体链路建立成功。

## 常见问题

| 现象 | 原因与处理 |
|------|-----------|
| 页面能开，摄像头按钮点了没反应 / 提示权限失败 | 不是 HTTPS，或者证书没装到系统信任库。地址栏必须是 `https://` 且无红色警告 |
| "媒体连接中"一直不变 | 1）浏览器连不上 `wss://<IP>/sfu/ws`：证书没信任；2）UDP 49152-49200 被服务端防火墙拦了；3）`SFU_PUBLIC_IP`/`PUBLIC_HOST` 不是客户端能到达的 IP（比如写成了 Docker 内网地址）。看 `docker compose logs sfu-server` |
| 入会要等 5 秒左右才连上 | 服务端访问不到外网 STUN。把 `.env` 里 `STUN_URLS=none`，重新 `up -d` |
| 成员列表看得到对方，但画面一直"等待视频流" | 对方没开摄像头；或者媒体 UDP 端口没放行。两边都开摄像头，查看 `sfu-server` 日志里是否有 `Camera track pushed to interested subscriber` |
| 手机端连得上但另一台电脑连不上 | 客户端与服务端不在同一个二层网段（访客 Wi-Fi 常做隔离），或客户端自身防火墙拦 UDP。先让双方互相 `ping` 通 |
| 找回密码收不到邮件 | `MAIL_PROVIDER=log` 时验证码在 `api-server` 日志里；要发真邮件配置 `smtp` |
| 想看指标 | `curl -H "Authorization: Bearer $METRICS_TOKEN" http://127.0.0.1:8080/metrics`（API 容器内网端口未对外暴露，可 `docker compose exec api-server wget -qO- http://localhost:8080/metrics`）；SFU 同理在 8081 |

## 本地开发 / 跑完整 E2E

开发时需要宿主机直连 Postgres/Redis，用开发叠加文件把端口露出来（公网机器不要这么做）：

```bash
make e2e-up        # postgres(5433) + redis(6379)
make e2e-backend   # 前台启动 SFU + API（另开终端）
make e2e-full      # 构建前端并用 Chromium 跑完整双用户会议用例
```

GitHub Actions 的 `e2e-full` 任务每次推送都会跑同一套用例（注册 → 建房 → 入会 → 媒体连接 → 开摄像头 →
对方看到远端视频 → 端到端加密聊天）。

## 之后搬到公网服务器

1. 买一台云主机 + 一个域名，域名 A 记录指向公网 IP；安全组放行与上面相同的端口。
2. 把 `PUBLIC_HOST` 改成域名；用 Let's Encrypt（certbot/acme.sh）签发证书，把 `fullchain.pem`/`privkey.pem`
   放到 `deployment/tls/server.crt`/`server.key`（文件名不变即可，`gen-self-signed.sh` 不再需要）。
3. `SFU_PUBLIC_IP`、`TURN_EXTERNAL_IP` 设为**公网 IP**（域名也可以，SFU 会解析成 IPv4；但云主机网卡上通常只有内网 IP，这两项必须填，否则浏览器拿到的候选是内网地址，连不上）。
4. `GO_ENV=production`，所有 secret 换成随机值，`MAIL_PROVIDER=smtp`，按需打开 `REQUIRE_EMAIL_VERIFICATION=true`。
5. 多人同时在公网开会时 TURN 流量走服务器带宽，按 1 路 1080p ≈ 1.5~2.5 Mbps 估算上行带宽。
