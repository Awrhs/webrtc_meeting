// Package metrics 是一个极简的 Prometheus 指标库：计数器、仪表、直方图与文本格式输出。
//
// 只依赖标准库（本仓库无法在开发机上更新 go.sum，因此不引入 prometheus/client_golang）。
// 输出遵循 Prometheus 文本格式 0.0.4，可被 Prometheus / Grafana Agent / VictoriaMetrics 直接抓取。
package metrics

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Registry 持有一组指标，按注册顺序输出。
type Registry struct {
	mu      sync.Mutex
	metrics []collector
	names   map[string]bool
}

type collector interface {
	write(w io.Writer)
}

func NewRegistry() *Registry {
	return &Registry{names: make(map[string]bool)}
}

func (r *Registry) register(name string, c collector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.names[name] {
		panic("metrics: duplicate metric name " + name)
	}
	r.names[name] = true
	r.metrics = append(r.metrics, c)
}

// RegisterGoRuntime 注册 goroutine 数、堆内存与进程启动时间等常用运行时指标。
func (r *Registry) RegisterGoRuntime() {
	start := float64(time.Now().Unix())
	r.NewGaugeFunc("go_goroutines", "Number of goroutines that currently exist.", func() float64 {
		return float64(runtime.NumGoroutine())
	})
	r.NewGaugeFunc("go_memstats_heap_alloc_bytes", "Number of heap bytes allocated and still in use.", func() float64 {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return float64(m.HeapAlloc)
	})
	r.NewGaugeFunc("process_start_time_seconds", "Start time of the process since unix epoch in seconds.", func() float64 {
		return start
	})
}

// Write 把全部指标以 Prometheus 文本格式写出。
func (r *Registry) Write(w io.Writer) {
	r.mu.Lock()
	metrics := append([]collector(nil), r.metrics...)
	r.mu.Unlock()
	for _, m := range metrics {
		m.write(w)
	}
}

// Handler 返回 /metrics 的 HTTP 处理器。
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		r.Write(w)
	})
}

// ===================== 带标签的样本表 =====================

type labeled struct {
	mu         sync.Mutex
	labelNames []string
	values     map[string]*sample // key: 标签值用 \x00 拼接
}

type sample struct {
	labelValues []string
	value       float64
	// histogram 专用
	buckets []uint64
	sum     float64
	count   uint64
}

func (l *labeled) get(labelValues []string) *sample {
	if len(labelValues) != len(l.labelNames) {
		panic(fmt.Sprintf("metrics: expected %d label values, got %d", len(l.labelNames), len(labelValues)))
	}
	key := strings.Join(labelValues, "\x00")
	l.mu.Lock()
	defer l.mu.Unlock()
	s, ok := l.values[key]
	if !ok {
		s = &sample{labelValues: append([]string(nil), labelValues...)}
		l.values[key] = s
	}
	return s
}

func (l *labeled) snapshot() []*sample {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]*sample, 0, len(l.values))
	for _, s := range l.values {
		copied := *s
		copied.buckets = append([]uint64(nil), s.buckets...)
		out = append(out, &copied)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.Join(out[i].labelValues, "\x00") < strings.Join(out[j].labelValues, "\x00")
	})
	return out
}

func formatLabels(names, values []string, extra ...string) string {
	if len(names) == 0 && len(extra) == 0 {
		return ""
	}
	parts := make([]string, 0, len(names)+1)
	for i, n := range names {
		parts = append(parts, n+`="`+escapeLabelValue(values[i])+`"`)
	}
	for i := 0; i+1 < len(extra); i += 2 {
		parts = append(parts, extra[i]+`="`+escapeLabelValue(extra[i+1])+`"`)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func escapeLabelValue(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return v
}

func formatFloat(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	case math.IsNaN(v):
		return "NaN"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

func writeHeader(w io.Writer, name, help, typ string) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", name, strings.ReplaceAll(help, "\n", " "), name, typ)
}

// ===================== Counter =====================

// Counter 单调递增计数器。
type Counter struct {
	name, help string
	labeled
}

func (r *Registry) NewCounter(name, help string, labelNames ...string) *Counter {
	c := &Counter{name: name, help: help, labeled: labeled{labelNames: labelNames, values: make(map[string]*sample)}}
	r.register(name, c)
	return c
}

func (c *Counter) Inc(labelValues ...string) { c.Add(1, labelValues...) }

func (c *Counter) Add(delta float64, labelValues ...string) {
	if delta < 0 {
		panic("metrics: counter cannot decrease")
	}
	s := c.get(labelValues)
	c.mu.Lock()
	s.value += delta
	c.mu.Unlock()
}

func (c *Counter) write(w io.Writer) {
	writeHeader(w, c.name, c.help, "counter")
	samples := c.snapshot()
	if len(samples) == 0 && len(c.labelNames) == 0 {
		fmt.Fprintf(w, "%s 0\n", c.name)
		return
	}
	for _, s := range samples {
		fmt.Fprintf(w, "%s%s %s\n", c.name, formatLabels(c.labelNames, s.labelValues), formatFloat(s.value))
	}
}

// ===================== Gauge =====================

// Gauge 可增可减的瞬时值。
type Gauge struct {
	name, help string
	labeled
}

func (r *Registry) NewGauge(name, help string, labelNames ...string) *Gauge {
	g := &Gauge{name: name, help: help, labeled: labeled{labelNames: labelNames, values: make(map[string]*sample)}}
	r.register(name, g)
	return g
}

func (g *Gauge) Set(v float64, labelValues ...string) {
	s := g.get(labelValues)
	g.mu.Lock()
	s.value = v
	g.mu.Unlock()
}

func (g *Gauge) Add(delta float64, labelValues ...string) {
	s := g.get(labelValues)
	g.mu.Lock()
	s.value += delta
	g.mu.Unlock()
}

func (g *Gauge) Inc(labelValues ...string) { g.Add(1, labelValues...) }
func (g *Gauge) Dec(labelValues ...string) { g.Add(-1, labelValues...) }

func (g *Gauge) write(w io.Writer) {
	writeHeader(w, g.name, g.help, "gauge")
	samples := g.snapshot()
	if len(samples) == 0 && len(g.labelNames) == 0 {
		fmt.Fprintf(w, "%s 0\n", g.name)
		return
	}
	for _, s := range samples {
		fmt.Fprintf(w, "%s%s %s\n", g.name, formatLabels(g.labelNames, s.labelValues), formatFloat(s.value))
	}
}

// ===================== GaugeFunc =====================

// gaugeFunc 抓取时调用回调取值的仪表（适合"当前房间数"这类随时能算出来的量）。
type gaugeFunc struct {
	name, help string
	fn         func() float64
}

func (r *Registry) NewGaugeFunc(name, help string, fn func() float64) {
	r.register(name, &gaugeFunc{name: name, help: help, fn: fn})
}

func (g *gaugeFunc) write(w io.Writer) {
	writeHeader(w, g.name, g.help, "gauge")
	fmt.Fprintf(w, "%s %s\n", g.name, formatFloat(g.fn()))
}

// ===================== Histogram =====================

// Histogram 累计直方图（_bucket/_sum/_count）。
type Histogram struct {
	name, help string
	buckets    []float64 // 升序，不含 +Inf
	labeled
}

// DefBuckets 适合 HTTP 请求耗时（秒）的默认桶。
var DefBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

func (r *Registry) NewHistogram(name, help string, buckets []float64, labelNames ...string) *Histogram {
	if len(buckets) == 0 {
		buckets = DefBuckets
	}
	sorted := append([]float64(nil), buckets...)
	sort.Float64s(sorted)
	h := &Histogram{name: name, help: help, buckets: sorted, labeled: labeled{labelNames: labelNames, values: make(map[string]*sample)}}
	r.register(name, h)
	return h
}

func (h *Histogram) Observe(v float64, labelValues ...string) {
	s := h.get(labelValues)
	h.mu.Lock()
	defer h.mu.Unlock()
	if s.buckets == nil {
		s.buckets = make([]uint64, len(h.buckets))
	}
	for i, upper := range h.buckets {
		if v <= upper {
			s.buckets[i]++
		}
	}
	s.sum += v
	s.count++
}

func (h *Histogram) write(w io.Writer) {
	writeHeader(w, h.name, h.help, "histogram")
	for _, s := range h.snapshot() {
		for i, upper := range h.buckets {
			var n uint64
			if i < len(s.buckets) {
				n = s.buckets[i]
			}
			fmt.Fprintf(w, "%s_bucket%s %d\n", h.name, formatLabels(h.labelNames, s.labelValues, "le", formatFloat(upper)), n)
		}
		fmt.Fprintf(w, "%s_bucket%s %d\n", h.name, formatLabels(h.labelNames, s.labelValues, "le", "+Inf"), s.count)
		fmt.Fprintf(w, "%s_sum%s %s\n", h.name, formatLabels(h.labelNames, s.labelValues), formatFloat(s.sum))
		fmt.Fprintf(w, "%s_count%s %d\n", h.name, formatLabels(h.labelNames, s.labelValues), s.count)
	}
}
