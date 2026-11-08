package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func render(r *Registry) string {
	var b strings.Builder
	r.Write(&b)
	return b.String()
}

func TestCounterAndGaugeTextFormat(t *testing.T) {
	r := NewRegistry()
	requests := r.NewCounter("http_requests_total", "Total HTTP requests.", "method", "status")
	requests.Inc("GET", "200")
	requests.Inc("GET", "200")
	requests.Add(3, "POST", "500")
	peers := r.NewGauge("sfu_peers", "Connected peers.")
	peers.Set(4)
	peers.Dec()

	out := render(r)
	for _, want := range []string{
		"# HELP http_requests_total Total HTTP requests.\n# TYPE http_requests_total counter\n",
		`http_requests_total{method="GET",status="200"} 2` + "\n",
		`http_requests_total{method="POST",status="500"} 3` + "\n",
		"# TYPE sfu_peers gauge\n",
		"sfu_peers 3\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestLabelValueEscaping(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounter("c", "h", "route")
	c.Inc(`/a"b\c` + "\n")
	out := render(r)
	if !strings.Contains(out, `c{route="/a\"b\\c\n"} 1`) {
		t.Fatalf("label escaping wrong:\n%s", out)
	}
}

func TestHistogramBucketsAreCumulative(t *testing.T) {
	r := NewRegistry()
	h := r.NewHistogram("req_seconds", "Latency.", []float64{0.1, 0.5, 1}, "route")
	for _, v := range []float64{0.05, 0.2, 0.7, 3} {
		h.Observe(v, "/x")
	}
	out := render(r)
	for _, want := range []string{
		`req_seconds_bucket{route="/x",le="0.1"} 1`,
		`req_seconds_bucket{route="/x",le="0.5"} 2`,
		`req_seconds_bucket{route="/x",le="1"} 3`,
		`req_seconds_bucket{route="/x",le="+Inf"} 4`,
		`req_seconds_sum{route="/x"} 3.95`,
		`req_seconds_count{route="/x"} 4`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("histogram output missing %q:\n%s", want, out)
		}
	}
}

func TestGaugeFuncAndRuntimeMetrics(t *testing.T) {
	r := NewRegistry()
	r.NewGaugeFunc("rooms", "Rooms.", func() float64 { return 7 })
	r.RegisterGoRuntime()
	out := render(r)
	if !strings.Contains(out, "rooms 7\n") || !strings.Contains(out, "go_goroutines ") || !strings.Contains(out, "process_start_time_seconds ") {
		t.Fatalf("missing gauge func / runtime metrics:\n%s", out)
	}
}

func TestHandlerContentType(t *testing.T) {
	r := NewRegistry()
	r.NewCounter("x_total", "x").Inc()
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain; version=0.0.4") {
		t.Fatalf("content type = %q", got)
	}
	if !strings.Contains(rec.Body.String(), "x_total 1\n") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestCounterFuncAndProtectedHandler(t *testing.T) {
	r := NewRegistry()
	r.NewCounterFunc("joins_total", "Joins.", func() float64 { return 12 })
	if out := render(r); !strings.Contains(out, "# TYPE joins_total counter\njoins_total 12\n") {
		t.Fatalf("counter func output wrong:\n%s", out)
	}

	h := r.ProtectedHandler("s3cret")
	serve := func(auth, query string) int {
		req := httptest.NewRequest(http.MethodGet, "/metrics"+query, nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := serve("", ""); code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", code)
	}
	if code := serve("Bearer wrong", ""); code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", code)
	}
	if code := serve("Bearer s3cret", ""); code != http.StatusOK {
		t.Fatalf("bearer token: %d", code)
	}
	if code := serve("", "?token=s3cret"); code != http.StatusOK {
		t.Fatalf("query token: %d", code)
	}
	if code := NewRegistry().ProtectedHandler("").(http.HandlerFunc); code == nil {
		t.Fatal("empty token must return the plain handler")
	}
}

func TestDuplicateNamePanics(t *testing.T) {
	r := NewRegistry()
	r.NewCounter("dup", "a")
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate metric name must panic")
		}
	}()
	r.NewGauge("dup", "b")
}

func TestLabelCountMismatchPanics(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounter("c", "h", "a", "b")
	defer func() {
		if recover() == nil {
			t.Fatal("wrong label count must panic")
		}
	}()
	c.Inc("only-one")
}
