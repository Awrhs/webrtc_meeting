package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type visitor struct {
	windowStart time.Time
	count       int
}

type IPLimiter struct {
	limit    int
	window   time.Duration
	visitors map[string]*visitor
	mu       sync.Mutex
}

func NewIPLimiter(limit int, window time.Duration) *IPLimiter {
	return &IPLimiter{
		limit:    limit,
		window:   window,
		visitors: make(map[string]*visitor),
	}
}

func (l *IPLimiter) Allow(ip string) bool {
	if l.limit <= 0 {
		return true
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	v, ok := l.visitors[ip]
	if !ok || now.Sub(v.windowStart) >= l.window {
		l.visitors[ip] = &visitor{windowStart: now, count: 1}
		return true
	}
	if v.count >= l.limit {
		return false
	}
	v.count++
	return true
}

func (l *IPLimiter) Gin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !l.Allow(c.ClientIP()) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limited"})
			return
		}
		c.Next()
	}
}

func (l *IPLimiter) HTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		if !l.Allow(ip) {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
