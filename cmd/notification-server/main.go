package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"github.com/domainry/domainry-foundation/telemetry"
	"github.com/domainry/domainry-notification-sdk/deliverygateway"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	server "github.com/domainry/domainry-notification/internal/assembly/saas"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/sqlstore"
	notificationhttp "github.com/domainry/domainry-notification/internal/transport/http"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	config, err := configurationFromEnvironment()
	if err != nil {
		return err
	}
	shutdownTelemetry, err := telemetry.Initialize(ctx, config.telemetry)
	if err != nil {
		return err
	}
	defer shutdownTelemetry(context.Background())
	database, err := sql.Open(config.sqlDriver, config.databaseDSN)
	if err != nil {
		return fmt.Errorf("open Notification SaaS database: %w", err)
	}
	database.SetMaxOpenConns(config.databaseMaxOpen)
	database.SetMaxIdleConns(config.databaseMaxIdle)
	database.SetConnMaxLifetime(config.databaseConnLifetime)
	persistence, err := server.NewSQLPersistence(server.SQLPersistenceOptions{Database: database, Driver: config.storeDriver, Schema: config.databaseSchema, OwnsDatabase: true})
	if err != nil {
		_ = database.Close()
		return err
	}
	remoteGateway, err := deliverygateway.NewRemote(deliverygateway.RemoteConfig{BaseURL: config.deliveryGatewayURL, ServiceCredential: config.deliveryGatewayCredential, RequestTimeout: config.deliveryGatewayTimeout, MaxAttempts: config.deliveryGatewayAttempts})
	if err != nil {
		_ = persistence.Close()
		return err
	}
	applications, err := server.NewSQLApplicationFactory(server.SQLApplicationFactoryOptions{Persistence: persistence, Catalog: config.catalog, WorkerID: config.workerID, RemoteDeliveryGateway: remoteGateway})
	if err != nil {
		_ = persistence.Close()
		return err
	}
	metrics := notificationhttp.NewOperationalMetrics()
	runtime, err := server.Open(ctx, server.Options{Identity: server.IdentityOptionsFromEnvironment(), Applications: applications, Workers: server.WorkerOptions{PollInterval: config.workerPollInterval, BatchSize: config.workerBatchSize}, Metrics: metrics})
	if err != nil {
		_ = applications.Close(ctx)
		return err
	}
	defer runtime.Close(context.Background())
	mux := http.NewServeMux()
	mux.HandleFunc("GET /live", func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /ready", func(response http.ResponseWriter, _ *http.Request) {
		if !runtime.Ready() {
			http.Error(response, "not ready", http.StatusServiceUnavailable)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("GET /metrics", metrics.Handler())
	mux.Handle("/", runtime.Handler())
	httpServer := &http.Server{Addr: config.httpAddress, Handler: notificationhttp.ObserveHTTP(mux, metrics), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- httpServer.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownContext)
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

type configuration struct {
	httpAddress, sqlDriver, databaseDSN, databaseSchema string
	storeDriver                                         sqlstore.Driver
	databaseMaxOpen, databaseMaxIdle                    int
	databaseConnLifetime                                time.Duration
	deliveryGatewayURL, deliveryGatewayCredential       string
	deliveryGatewayTimeout                              time.Duration
	deliveryGatewayAttempts                             int
	workerID                                            string
	workerPollInterval                                  time.Duration
	workerBatchSize                                     int
	catalog                                             modulehost.Catalog
	telemetry                                           telemetry.Config
}

func configurationFromEnvironment() (configuration, error) {
	value := configuration{
		httpAddress: env("NOTIFICATION_HTTP_ADDRESS", ":8080"), databaseDSN: strings.TrimSpace(os.Getenv("NOTIFICATION_DATABASE_DSN")), databaseSchema: strings.TrimSpace(os.Getenv("NOTIFICATION_DATABASE_SCHEMA")),
		databaseMaxOpen: 20, databaseMaxIdle: 10, databaseConnLifetime: 30 * time.Minute,
		deliveryGatewayURL: strings.TrimSpace(os.Getenv("NOTIFICATION_DELIVERY_GATEWAY_URL")), deliveryGatewayCredential: strings.TrimSpace(os.Getenv("NOTIFICATION_DELIVERY_GATEWAY_SERVICE_CREDENTIAL")), deliveryGatewayTimeout: 10 * time.Second, deliveryGatewayAttempts: 3,
		workerID: strings.TrimSpace(os.Getenv("NOTIFICATION_WORKER_ID")), workerPollInterval: time.Second, workerBatchSize: 100,
		telemetry: telemetry.Config{ServiceName: "domainry-notification", ServiceVersion: strings.TrimSpace(os.Getenv("NOTIFICATION_SERVICE_VERSION")), Exporter: env("NOTIFICATION_TELEMETRY_EXPORTER", "none"), Endpoint: strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")), Headers: telemetryHeaders(os.Getenv("OTEL_EXPORTER_OTLP_HEADERS")), Insecure: strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_INSECURE")), "true"), SampleRatio: 1, ExportTimeout: 10 * time.Second},
	}
	if raw := strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER_ARG")); raw != "" {
		ratio, parseErr := strconv.ParseFloat(raw, 64)
		if parseErr != nil || ratio <= 0 || ratio > 1 {
			return configuration{}, fmt.Errorf("OTEL_TRACES_SAMPLER_ARG must be greater than 0 and at most 1")
		}
		value.telemetry.SampleRatio = ratio
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("NOTIFICATION_DATABASE_DRIVER"))) {
	case "sqlite":
		value.storeDriver, value.sqlDriver = sqlstore.SQLite, "sqlite"
	case "postgres", "postgresql", "pgx":
		value.storeDriver, value.sqlDriver = sqlstore.Postgres, "pgx"
	case "mysql":
		value.storeDriver, value.sqlDriver = sqlstore.MySQL, "mysql"
	default:
		return configuration{}, fmt.Errorf("NOTIFICATION_DATABASE_DRIVER must be sqlite, postgres or mysql")
	}
	if value.databaseDSN == "" || value.deliveryGatewayURL == "" || value.deliveryGatewayCredential == "" || value.workerID == "" {
		return configuration{}, fmt.Errorf("Notification SaaS database, Delivery Gateway and worker configuration are required")
	}
	catalogFile := strings.TrimSpace(os.Getenv("NOTIFICATION_CATALOG_FILE"))
	if catalogFile == "" {
		return configuration{}, fmt.Errorf("NOTIFICATION_CATALOG_FILE is required")
	}
	raw, err := os.ReadFile(catalogFile)
	if err != nil {
		return configuration{}, fmt.Errorf("read Notification catalog: %w", err)
	}
	if err := json.Unmarshal(raw, &value.catalog); err != nil {
		return configuration{}, fmt.Errorf("decode Notification catalog: %w", err)
	}
	if strings.TrimSpace(value.catalog.DefaultLocale) == "" {
		return configuration{}, fmt.Errorf("Notification catalog default locale is required")
	}
	return value, nil
}

func telemetryHeaders(raw string) map[string]string {
	result := map[string]string{}
	for _, entry := range strings.Split(raw, ",") {
		key, value, found := strings.Cut(entry, "=")
		if key = strings.TrimSpace(key); found && key != "" {
			result[key] = strings.TrimSpace(value)
		}
	}
	return result
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
