package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence"
)

func TestConfigurationRequiresStandaloneSaaSDependencies(t *testing.T) {
	for _, key := range []string{"NOTIFICATION_DATABASE_DRIVER", "NOTIFICATION_DATABASE_DSN", "NOTIFICATION_DELIVERY_GATEWAY_URL", "NOTIFICATION_DELIVERY_GATEWAY_SERVICE_CREDENTIAL", "NOTIFICATION_WORKER_ID", "NOTIFICATION_CATALOG_FILE"} {
		t.Setenv(key, "")
	}
	if _, err := configurationFromEnvironment(); err == nil {
		t.Fatal("incomplete standalone Notification configuration was accepted")
	}
}

func TestOwnedSQLiteDatabaseUsesORMConnectionPolicy(t *testing.T) {
	config := configuration{
		storeDriver: sqlstore.SQLite, sqlDriver: "sqlite", databaseDSN: filepath.Join(t.TempDir(), "notification.db"),
		databaseMaxOpen: 6, databaseMaxIdle: 2, databaseConnLifetime: time.Minute, databaseLockTimeout: 100 * time.Millisecond,
	}
	database, err := openOwnedDatabase(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if stats := database.Stats(); stats.MaxOpenConnections != 6 {
		t.Fatalf("max open connections=%d", stats.MaxOpenConnections)
	}
	var journalMode string
	if err := database.QueryRowContext(t.Context(), "PRAGMA journal_mode").Scan(&journalMode); err != nil || !strings.EqualFold(journalMode, "wal") {
		t.Fatalf("journal mode=%q err=%v", journalMode, err)
	}
	connections := make([]interface{ Close() error }, 0, 2)
	for index := 0; index < 2; index++ {
		connection, err := database.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, connection)
		var busyTimeout, foreignKeys int
		if err := connection.QueryRowContext(t.Context(), "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil || busyTimeout != 100 {
			t.Fatalf("connection %d busy timeout=%d err=%v", index, busyTimeout, err)
		}
		if err := connection.QueryRowContext(t.Context(), "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil || foreignKeys != 1 {
			t.Fatalf("connection %d foreign keys=%d err=%v", index, foreignKeys, err)
		}
	}
	for _, connection := range connections {
		_ = connection.Close()
	}
}

func TestOwnedSQLiteDatabaseRejectsMemoryMode(t *testing.T) {
	_, err := openOwnedDatabase(t.Context(), configuration{storeDriver: sqlstore.SQLite, sqlDriver: "sqlite", databaseDSN: ":memory:", databaseMaxOpen: 1, databaseLockTimeout: time.Second})
	if err == nil || !strings.Contains(err.Error(), "requires a file database") {
		t.Fatalf("memory SQLite error=%v", err)
	}
}

func TestConfigurationSelectsPostgresAndLoadsCatalog(t *testing.T) {
	catalogFile := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(catalogFile, []byte(`{"DefaultLocale":"en","TemplateCapabilities":[{"channel":"in_app"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOTIFICATION_DATABASE_DRIVER", "postgres")
	t.Setenv("NOTIFICATION_DATABASE_DSN", "postgres://notification@example/notification")
	t.Setenv("NOTIFICATION_DELIVERY_GATEWAY_URL", "https://runtime.example")
	t.Setenv("NOTIFICATION_DELIVERY_GATEWAY_SERVICE_CREDENTIAL", "credential")
	t.Setenv("NOTIFICATION_WORKER_ID", "worker")
	t.Setenv("NOTIFICATION_CATALOG_FILE", catalogFile)
	t.Setenv("NOTIFICATION_TELEMETRY_EXPORTER", "otlp-http")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://telemetry.example/v1/traces")
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "authorization=secret,x-tenant=tenant")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.25")
	config, err := configurationFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if config.storeDriver != sqlstore.Postgres || config.sqlDriver != "pgx" || config.catalog.DefaultLocale != "en" || config.telemetry.Exporter != "otlp-http" || config.telemetry.Endpoint != "https://telemetry.example/v1/traces" || config.telemetry.Headers["x-tenant"] != "tenant" || config.telemetry.SampleRatio != 0.25 {
		t.Fatalf("config=%+v", config)
	}
}

func TestConfigurationDefaultsStandaloneSQLiteToRuntimeDatabase(t *testing.T) {
	catalogFile := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(catalogFile, []byte(`{"DefaultLocale":"en","TemplateCapabilities":[{"channel":"in_app"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"NOTIFICATION_DATABASE_DRIVER", "NOTIFICATION_DATABASE_DSN"} {
		t.Setenv(key, "")
	}
	t.Setenv("NOTIFICATION_DELIVERY_GATEWAY_URL", "https://runtime.example")
	t.Setenv("NOTIFICATION_DELIVERY_GATEWAY_SERVICE_CREDENTIAL", "credential")
	t.Setenv("NOTIFICATION_WORKER_ID", "worker")
	t.Setenv("NOTIFICATION_CATALOG_FILE", catalogFile)

	config, err := configurationFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if config.storeDriver != sqlstore.SQLite || config.sqlDriver != "sqlite" || config.databaseDSN != "runtime.db" {
		t.Fatalf("config=%+v", config)
	}
}
