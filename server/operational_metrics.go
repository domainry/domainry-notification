package server

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type operationalHTTPMetric struct {
	Count, Errors uint64
	Duration      float64
}

type OperationalMetrics struct {
	mu          sync.RWMutex
	http        map[string]operationalHTTPMetric
	workerRuns  map[string]uint64
	workerItems map[string]uint64
	workerTime  map[string]float64
	inFlight    atomic.Int64
}

func NewOperationalMetrics() *OperationalMetrics {
	return &OperationalMetrics{http: map[string]operationalHTTPMetric{}, workerRuns: map[string]uint64{}, workerItems: map[string]uint64{}, workerTime: map[string]float64{}}
}

func (m *OperationalMetrics) ObserveWorker(kind, result string, items int, duration time.Duration) {
	if m == nil {
		return
	}
	kind, result = metricLabel(kind, "unknown"), metricLabel(result, "unknown")
	m.mu.Lock()
	m.workerRuns[kind+"\x00"+result]++
	m.workerItems[kind] += uint64(max(items, 0))
	m.workerTime[kind] += duration.Seconds()
	m.mu.Unlock()
}

func (m *OperationalMetrics) Handler() http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = response.Write([]byte(m.Prometheus()))
	})
}

func (m *OperationalMetrics) Prometheus() string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var builder strings.Builder
	fmt.Fprintf(&builder, "# TYPE notification_http_in_flight gauge\nnotification_http_in_flight %d\n", m.inFlight.Load())
	keys := sortedMetricKeys(m.http)
	for _, key := range keys {
		parts := strings.Split(key, "\x00")
		value := m.http[key]
		labels := fmt.Sprintf("method=%q,path=%q,status_class=%q", parts[0], parts[1], parts[2])
		fmt.Fprintf(&builder, "notification_http_requests_total{%s} %d\nnotification_http_errors_total{%s} %d\nnotification_http_request_duration_seconds_sum{%s} %g\n", labels, value.Count, labels, value.Errors, labels, value.Duration)
	}
	workerKeys := sortedMetricKeys(m.workerRuns)
	for _, key := range workerKeys {
		parts := strings.Split(key, "\x00")
		fmt.Fprintf(&builder, "notification_worker_runs_total{kind=%q,result=%q} %d\n", parts[0], parts[1], m.workerRuns[key])
	}
	for _, kind := range sortedMetricKeys(m.workerItems) {
		fmt.Fprintf(&builder, "notification_worker_items_total{kind=%q} %d\nnotification_worker_duration_seconds_sum{kind=%q} %g\n", kind, m.workerItems[kind], kind, m.workerTime[kind])
	}
	return builder.String()
}

func ObserveHTTP(handler http.Handler, metrics *OperationalMetrics) http.Handler {
	if handler == nil {
		return nil
	}
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		ctx := otel.GetTextMapPropagator().Extract(request.Context(), propagation.HeaderCarrier(request.Header))
		ctx, span := otel.Tracer("domainry.notification.http").Start(ctx, request.Method+" "+request.URL.Path, trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(attribute.String("http.request.method", request.Method), attribute.String("url.path", request.URL.Path)))
		defer span.End()
		if metrics != nil {
			metrics.inFlight.Add(1)
			defer metrics.inFlight.Add(-1)
		}
		started := time.Now()
		capture := &statusCapture{ResponseWriter: response, status: http.StatusOK}
		handler.ServeHTTP(capture, request.WithContext(ctx))
		if capture.status >= 500 {
			span.SetStatus(codes.Error, http.StatusText(capture.status))
		}
		span.SetAttributes(attribute.Int("http.response.status_code", capture.status))
		if metrics != nil {
			metrics.observeHTTP(request.Method, request.URL.Path, capture.status, time.Since(started))
		}
	})
}

type statusCapture struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusCapture) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.wrote = true
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *statusCapture) Write(payload []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(payload)
}
func (w *statusCapture) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *statusCapture) Flush()                      { _ = http.NewResponseController(w.ResponseWriter).Flush() }
func (w *statusCapture) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}
func (w *statusCapture) Push(target string, options *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, options)
	}
	return http.ErrNotSupported
}

func (m *OperationalMetrics) observeHTTP(method, path string, status int, duration time.Duration) {
	method, path = metricLabel(strings.ToUpper(method), "UNKNOWN"), normalizedMetricPath(path)
	class := fmt.Sprintf("%dxx", status/100)
	key := method + "\x00" + path + "\x00" + class
	m.mu.Lock()
	value := m.http[key]
	value.Count++
	value.Duration += duration.Seconds()
	if status >= 500 {
		value.Errors++
	}
	m.http[key] = value
	m.mu.Unlock()
}

func normalizedMetricPath(path string) string {
	path = strings.TrimSpace(path)
	if strings.HasPrefix(path, "/v1/") || path == "/live" || path == "/ready" || path == "/metrics" {
		return path
	}
	return "other"
}
func metricLabel(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
func sortedMetricKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
