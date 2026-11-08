#!/bin/sh
# 生成局域网试用的自签名 TLS 证书（server.crt / server.key，有效期 825 天）。
#
# 用法：deployment/tls/gen-self-signed.sh <局域网IP或域名> [更多IP/域名...]
#   例：deployment/tls/gen-self-signed.sh 192.168.1.10 meeting.lan
#
# 生成后：
#   - 服务端：docker compose -f deployment/docker-compose.yml -f deployment/docker-compose.tls.yml up -d --build
#   - 每台客户端电脑/手机信任 server.crt（否则浏览器会拦截，且 WebSocket 连不上）：
#       Windows：双击 server.crt → 安装证书 → 本地计算机 → "受信任的根证书颁发机构"
#       macOS：钥匙串访问 → 系统 → 导入 → 设为"始终信任"
#       iOS：AirDrop/邮件发送 server.crt → 设置 → 已下载描述文件 → 安装 → 通用 → 关于本机 → 证书信任设置
#       Android：设置 → 安全 → 加密与凭据 → 安装证书 → CA 证书
#   公网部署请改用 Let's Encrypt 等正式证书，把 fullchain/privkey 放到本目录同名文件即可。
set -eu

if [ $# -lt 1 ]; then
  echo "usage: $0 <ip-or-hostname> [more ip/hostnames...]" >&2
  exit 1
fi

DIR="$(cd "$(dirname "$0")" && pwd)"
PRIMARY="$1"
SAN="DNS:localhost,IP:127.0.0.1"
for name in "$@"; do
  case "$name" in
    *[!0-9.]*) SAN="$SAN,DNS:$name" ;;   # 含非数字/点 → 域名
    *)         SAN="$SAN,IP:$name" ;;     # 纯数字和点 → IPv4
  esac
done

openssl req -x509 -newkey rsa:2048 -sha256 -days 825 -nodes \
  -keyout "$DIR/server.key" -out "$DIR/server.crt" \
  -subj "/CN=$PRIMARY/O=SFU Conference (self-signed)" \
  -addext "subjectAltName=$SAN" \
  -addext "basicConstraints=critical,CA:TRUE" \
  -addext "keyUsage=critical,digitalSignature,keyEncipherment,keyCertSign" \
  -addext "extendedKeyUsage=serverAuth"

chmod 600 "$DIR/server.key"
echo "certificate: $DIR/server.crt"
echo "private key: $DIR/server.key"
echo "SAN: $SAN"
echo "现在运行: PUBLIC_HOST=$PRIMARY docker compose -f deployment/docker-compose.yml -f deployment/docker-compose.tls.yml up -d --build"
