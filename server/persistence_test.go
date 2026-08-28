package server

import (
	"database/sql"
	"strings"
	"testing"

	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification/sqlstore"

	_ "modernc.org/sqlite"
)

func TestSQLPersistencePreparesAndReopensExactApplication(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	persistence, err := NewSQLPersistence(SQLPersistenceOptions{Database: db, Driver: sqlstore.SQLite})
	if err != nil {
		t.Fatal(err)
	}
	application := notificationsdk.ApplicationRef{TenantID: "tenant-one", WorkspaceID: "workspace-one", ApplicationKey: "app-one"}
	first, err := persistence.PrepareApplication(t.Context(), application)
	if err != nil {
		t.Fatal(err)
	}
	second, err := persistence.PrepareApplication(t.Context(), application)
	if err != nil {
		t.Fatal(err)
	}
	if first.Table("notification_events") != second.Table("notification_events") {
		t.Fatalf("application namespace changed: %s != %s", first.Table("notification_events"), second.Table("notification_events"))
	}
	var migrations int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notification_saas_schema_migrations WHERE namespace = ?`, applicationKey(application)).Scan(&migrations); err != nil || migrations != 1 {
		t.Fatalf("migration count=%d err=%v", migrations, err)
	}
	var columns int
	table := strings.Trim(first.Table("notification_events"), `"`)
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name IN ('tenant_id', 'application_key', 'workspace_id')`, table).Scan(&columns); err != nil || columns != 3 {
		t.Fatalf("ownership columns=%d err=%v", columns, err)
	}
}

func TestSQLPersistenceSeparatesApplicationsWithSameWorkspace(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	persistence, err := NewSQLPersistence(SQLPersistenceOptions{Database: db, Driver: sqlstore.SQLite})
	if err != nil {
		t.Fatal(err)
	}
	left := notificationsdk.ApplicationRef{TenantID: "tenant", WorkspaceID: "workspace", ApplicationKey: "left"}
	right := notificationsdk.ApplicationRef{TenantID: "tenant", WorkspaceID: "workspace", ApplicationKey: "right"}
	leftDialect, err := persistence.PrepareApplication(t.Context(), left)
	if err != nil {
		t.Fatal(err)
	}
	rightDialect, err := persistence.PrepareApplication(t.Context(), right)
	if err != nil {
		t.Fatal(err)
	}
	if leftDialect.Table("notification_events") == rightDialect.Table("notification_events") {
		t.Fatal("two applications share one physical Notification table")
	}
	var migrations int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notification_saas_schema_migrations`).Scan(&migrations); err != nil || migrations != 2 {
		t.Fatalf("migration count=%d err=%v", migrations, err)
	}
}

func TestSQLPersistenceRejectsMigrationChecksumDrift(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	persistence, err := NewSQLPersistence(SQLPersistenceOptions{Database: db, Driver: sqlstore.SQLite})
	if err != nil {
		t.Fatal(err)
	}
	application := notificationsdk.ApplicationRef{TenantID: "tenant", WorkspaceID: "workspace", ApplicationKey: "application"}
	if _, err := persistence.PrepareApplication(t.Context(), application); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE notification_saas_schema_migrations SET checksum = 'different' WHERE namespace = ?`, applicationKey(application)); err != nil {
		t.Fatal(err)
	}
	if _, err := persistence.PrepareApplication(t.Context(), application); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("error=%v", err)
	}
}

func TestApplicationTablePrefixDoesNotExposeScopeValues(t *testing.T) {
	application := notificationsdk.ApplicationRef{TenantID: "tenant-secret", WorkspaceID: "workspace-secret", ApplicationKey: "application-secret"}
	prefix := applicationTablePrefix(application)
	if !strings.HasPrefix(prefix, "notification_") || !strings.HasSuffix(prefix, "_") || strings.Contains(prefix, "secret") {
		t.Fatalf("unsafe prefix %q", prefix)
	}
}
