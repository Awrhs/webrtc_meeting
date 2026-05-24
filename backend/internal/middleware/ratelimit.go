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
	stopCh   chan struct{}
	once     sync.Once
}

func NewIPLimiter(limit int, window time.Duration) *IPLimiter {
	if window <= 0 {
		window = time.Minute
	}
	limiter := &IPLimiter{
		limit:    limit,
		window:   window,
		visitors: make(map[string]*visitor),
		stopCh:   make(chan struct{}),
	}
	if limit > 0 {
		go limiter.cleanupLoop()
	}
	return limiter
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

func (l *IPLimiter) Stop() {
	l.once.Do(func() {
		close(l.stopCh)
	})
}

func (l *IPLimiter) cleanupLoop() {
	ticker := time.NewTicker(l.window)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			l.cleanupStaleVisitors(time.Now())
		case <-l.stopCh:
			return
		}
	}
}

func (l *IPLimiter) cleanupStaleVisitors(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	maxAge := l.window * 2
	for ip, visitor := range l.visitors {
		if now.Sub(visitor.windowStart) >= maxAge {
			delete(l.visitors, ip)
		}
	}
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
