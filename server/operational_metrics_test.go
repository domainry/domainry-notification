package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/domainry/domainry-foundation/telemetry"
)

func TestObserveHTTPExportsMetricsAndContinuesW3CTrace(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previousProvider, previousPropagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
		_ = provider.Shutdown(t.Context())
	})
	metrics := NewOperationalMetrics()
	var observed trace.SpanContext
	handler := ObserveHTTP(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		observed = trace.SpanContextFromContext(request.Context())
		if _, ok := response.(http.Flusher); !ok {
			t.Error("observability wrapper removed streaming support")
		}
		response.WriteHeader(http.StatusServiceUnavailable)
	}), metrics)
	request := httptest.NewRequest(http.MethodPost, "/v1/events:publish", nil)
	request.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if observed.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("continued trace=%s", observed.TraceID())
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Parent.SpanID().String() != "00f067aa0ba902b7" {
		t.Fatalf("spans=%+v", spans)
	}
	prometheus := metrics.Prometheus()
	for _, expected := range []string{`notification_http_requests_total{method="POST",path="/v1/events:publish",status_class="5xx"} 1`, `notification_http_errors_total{method="POST",path="/v1/events:publish",status_class="5xx"} 1`} {
		if !strings.Contains(prometheus, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, prometheus)
		}
	}
}

func TestOperationalWorkerMetricsAreBoundedByOwnedKinds(t *testing.T) {
	metrics := NewOperationalMetrics()
	metrics.ObserveWorker("inbox", "success", 3, 250*time.Millisecond)
	prometheus := metrics.Prometheus()
	for _, expected := range []string{`notification_worker_runs_total{kind="inbox",result="success"} 1`, `notification_worker_items_total{kind="inbox"} 3`, `notification_worker_duration_seconds_sum{kind="inbox"} 0.25`} {
		if !strings.Contains(prometheus, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, prometheus)
		}
	}
}

func TestTelemetryRejectsUnknownExporter(t *testing.T) {
	if _, err := telemetry.Initialize(t.Context(), telemetry.Config{Exporter: "unknown"}); err == nil {
		t.Fatal("unknown telemetry exporter was accepted")
	}
}
