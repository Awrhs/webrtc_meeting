package middleware

import (
	"strconv"
	"time"

	"conference-sfu/backend/pkg/metrics"

	"github.com/gin-gonic/gin"
)

// Metrics 记录每个 HTTP 请求的数量与耗时（按方法、路由模板、状态码）。
// 路由使用 gin 的模板（如 /api/v1/rooms/:id/join）而不是真实路径，避免标签基数爆炸。
func Metrics(reg *metrics.Registry) gin.HandlerFunc {
	requests := reg.NewCounter("http_requests_total", "HTTP requests by method, route and status code.", "method", "route", "status")
	duration := reg.NewHistogram("http_request_duration_seconds", "HTTP request latency in seconds.", metrics.DefBuckets, "method", "route")
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		requests.Inc(c.Request.Method, route, strconv.Itoa(c.Writer.Status()))
		duration.Observe(time.Since(start).Seconds(), c.Request.Method, route)
	}
}
