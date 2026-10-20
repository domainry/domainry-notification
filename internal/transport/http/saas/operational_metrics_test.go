package notificationhttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/domainry/domainry-foundation/telemetry"
)

func TestObserveHTTPExportsBoundedMetricsWithoutOwningTraceBoundary(t *testing.T) {
	metrics := NewOperationalMetrics()
	handler := ObserveHTTP(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		if _, ok := response.(http.Flusher); !ok {
			t.Error("observability wrapper removed streaming support")
		}
		response.WriteHeader(http.StatusServiceUnavailable)
	}), metrics)
	request := httptest.NewRequest(http.MethodPost, "/notification/v1/events:publish", nil)
	request.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	prometheus := metrics.Prometheus()
	for _, expected := range []string{`notification_http_requests_total{method="POST",path="other",status_class="5xx"} 1`, `notification_http_errors_total{method="POST",path="other",status_class="5xx"} 1`} {
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
