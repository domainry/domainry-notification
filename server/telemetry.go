package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/semconv/v1.37.0"
)

const (
	TelemetryExporterDisabled = "none"
	TelemetryExporterOTLPHTTP = "otlp-http"
)

type TelemetryConfig struct {
	ServiceName, ServiceVersion, Exporter, Endpoint string
	Headers                                         map[string]string
	Insecure                                        bool
	SampleRatio                                     float64
	ExportTimeout                                   time.Duration
}

type TelemetryShutdown func(context.Context) error

func InitializeTelemetry(ctx context.Context, config TelemetryConfig) (TelemetryShutdown, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	exporterName := strings.ToLower(strings.TrimSpace(config.Exporter))
	if exporterName == "" || exporterName == "off" || exporterName == TelemetryExporterDisabled {
		return func(context.Context) error { return nil }, nil
	}
	if exporterName == "otlp" {
		exporterName = TelemetryExporterOTLPHTTP
	}
	if exporterName != TelemetryExporterOTLPHTTP {
		return nil, fmt.Errorf("unsupported Notification telemetry exporter %q", config.Exporter)
	}
	options := []otlptracehttp.Option{}
	if endpoint := strings.TrimSpace(config.Endpoint); endpoint != "" {
		options = append(options, otlptracehttp.WithEndpointURL(endpoint))
	}
	if config.Insecure {
		options = append(options, otlptracehttp.WithInsecure())
	}
	if len(config.Headers) > 0 {
		options = append(options, otlptracehttp.WithHeaders(config.Headers))
	}
	if config.ExportTimeout > 0 {
		options = append(options, otlptracehttp.WithTimeout(config.ExportTimeout))
	}
	exporter, err := otlptracehttp.New(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("initialize Notification OTLP HTTP trace exporter: %w", err)
	}
	ratio := config.SampleRatio
	if ratio <= 0 {
		ratio = 1
	}
	if ratio > 1 {
		ratio = 1
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes("", semconv.ServiceName(strings.TrimSpace(config.ServiceName)), semconv.ServiceVersion(strings.TrimSpace(config.ServiceVersion)))),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))),
	)
	otel.SetTracerProvider(provider)
	return provider.Shutdown, nil
}
