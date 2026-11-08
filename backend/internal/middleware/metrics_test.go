package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"conference-sfu/backend/pkg/metrics"

	"github.com/gin-gonic/gin"
)

func TestMetricsMiddlewareRecordsRouteTemplateAndStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reg := metrics.NewRegistry()
	r := gin.New()
	r.Use(Metrics(reg))
	r.GET("/api/v1/rooms/:id", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.POST("/api/v1/rooms/:id/join", func(c *gin.Context) { c.Status(http.StatusForbidden) })

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/v1/rooms/1", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/rooms/2", nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/rooms/9/join", nil),
		httptest.NewRequest(http.MethodGet, "/nope", nil),
	} {
		r.ServeHTTP(httptest.NewRecorder(), req)
	}

	var b strings.Builder
	reg.Write(&b)
	out := b.String()
	for _, want := range []string{
		`http_requests_total{method="GET",route="/api/v1/rooms/:id",status="200"} 2`,
		`http_requests_total{method="POST",route="/api/v1/rooms/:id/join",status="403"} 1`,
		`http_requests_total{method="GET",route="unmatched",status="404"} 1`,
		`http_request_duration_seconds_count{method="GET",route="/api/v1/rooms/:id"} 2`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("metrics output missing %q:\n%s", want, out)
		}
	}
}
