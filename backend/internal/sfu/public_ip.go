package sfu

import (
	"net"

	"go.uber.org/zap"
)

// ResolvePublicIPs 把 SFU_PUBLIC_IP 的条目整理成可写进 ICE host 候选的 IP 列表：
// 纯 IP 原样保留；域名（docker-compose.tls.yml 默认沿用 PUBLIC_HOST，可能是域名）解析成
// IPv4；解析失败的条目跳过并告警。pion 的 SetNAT1To1IPs 只接受 IP，塞进域名会让 ICE 直接失败。
func ResolvePublicIPs(entries []string, logger *zap.Logger) []string {
	return resolvePublicIPs(entries, net.LookupIP, logger)
}

func resolvePublicIPs(entries []string, lookup func(string) ([]net.IP, error), logger *zap.Logger) []string {
	out := make([]string, 0, len(entries))
	seen := make(map[string]bool)
	add := func(ip string) {
		if !seen[ip] {
			seen[ip] = true
			out = append(out, ip)
		}
	}
	for _, entry := range entries {
		if ip := net.ParseIP(entry); ip != nil {
			add(ip.String())
			continue
		}
		ips, err := lookup(entry)
		if err != nil || len(ips) == 0 {
			logger.Warn("SFU_PUBLIC_IP entry is not an IP and could not be resolved; skipped",
				zap.String("entry", entry), zap.Error(err))
			continue
		}
		resolved := 0
		for _, ip := range ips {
			if v4 := ip.To4(); v4 != nil {
				add(v4.String())
				resolved++
			}
		}
		if resolved == 0 {
			logger.Warn("SFU_PUBLIC_IP entry resolved to no IPv4 address; skipped", zap.String("entry", entry))
		}
	}
	return out
}
