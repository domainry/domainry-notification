package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/domainry/domainry-notification/sqlstore"
)

func TestConfigurationRequiresStandaloneSaaSDependencies(t *testing.T) {
	for _, key := range []string{"NOTIFICATION_DATABASE_DRIVER", "NOTIFICATION_DATABASE_DSN", "NOTIFICATION_DELIVERY_GATEWAY_URL", "NOTIFICATION_DELIVERY_GATEWAY_SERVICE_CREDENTIAL", "NOTIFICATION_WORKER_ID", "NOTIFICATION_CATALOG_FILE"} {
		t.Setenv(key, "")
	}
	if _, err := configurationFromEnvironment(); err == nil {
		t.Fatal("incomplete standalone Notification configuration was accepted")
	}
}

func TestConfigurationSelectsPostgresAndLoadsCatalog(t *testing.T) {
	catalogFile := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(catalogFile, []byte(`{"DefaultLocale":"en","Surfaces":["business_workspace"],"TemplateCapabilities":[{"channel":"in_app"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOTIFICATION_DATABASE_DRIVER", "postgres")
	t.Setenv("NOTIFICATION_DATABASE_DSN", "postgres://notification@example/notification")
	t.Setenv("NOTIFICATION_DELIVERY_GATEWAY_URL", "https://runtime.example")
	t.Setenv("NOTIFICATION_DELIVERY_GATEWAY_SERVICE_CREDENTIAL", "credential")
	t.Setenv("NOTIFICATION_WORKER_ID", "worker")
	t.Setenv("NOTIFICATION_CATALOG_FILE", catalogFile)
	config, err := configurationFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if config.storeDriver != sqlstore.Postgres || config.sqlDriver != "pgx" || config.catalog.DefaultLocale != "en" {
		t.Fatalf("config=%+v", config)
	}
}
