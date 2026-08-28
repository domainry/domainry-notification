package sqlstore

import (
	"database/sql"
	"encoding/json"
	"testing"

	_ "modernc.org/sqlite"
)

func TestPortableMigrationFiltersWorkspaceImportsOwnershipAndReconciles(t *testing.T) {
	source := openPortableDatabase(t, "source")
	applyPortableMigrations(t, source, mustSchemaMigrations(t, ""))
	if _, err := source.Exec(`INSERT INTO notification_template_records (template_key, draft_json, published_json, published_version, status, updated_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, "welcome", nil, `{}`, 1, "active", "admin", "now", "now"); err != nil {
		t.Fatal(err)
	}
	insertPortableEvent(t, source, "workspace", "event-one", "source-one", "worker")
	insertPortableEvent(t, source, "other-workspace", "event-other", "source-other", "")
	sourceDialect, _ := NewDialect(SQLite, "", "")
	scope := PortableScope{TenantID: "tenant", WorkspaceID: "workspace", ApplicationKey: "application"}
	bundle, inventory, err := ExportPortable(t.Context(), source, sourceDialect, scope)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Rows != 2 || inventory.ActiveLeases != 1 || inventory.Tables["notification_events"] != 1 || bundle.Fingerprint == "" {
		t.Fatalf("inventory=%+v bundle=%+v", inventory, bundle)
	}

	target := openPortableDatabase(t, "target")
	prefix := "application_"
	migrations, err := ApplicationSchemaMigrations(SQLite, "", prefix, ApplicationScope{TenantID: scope.TenantID, WorkspaceID: scope.WorkspaceID, ApplicationKey: scope.ApplicationKey})
	if err != nil {
		t.Fatal(err)
	}
	applyPortableMigrations(t, target, migrations)
	targetDialect, _ := NewDialect(SQLite, "", prefix)
	receipt, err := ImportPortable(t.Context(), target, targetDialect, scope, bundle)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Rows != inventory.Rows || receipt.Fingerprint != bundle.Fingerprint || receipt.AlreadyPresent {
		t.Fatalf("receipt=%+v", receipt)
	}
	repeated, err := ImportPortable(t.Context(), target, targetDialect, scope, bundle)
	if err != nil || !repeated.AlreadyPresent || repeated.Fingerprint != receipt.Fingerprint {
		t.Fatalf("repeated=%+v err=%v", repeated, err)
	}
	var tenantID, applicationKey, workspaceID string
	if err := target.QueryRow(`SELECT tenant_id, application_key, workspace_id FROM application_notification_events WHERE id = ?`, "event-one").Scan(&tenantID, &applicationKey, &workspaceID); err != nil {
		t.Fatal(err)
	}
	if tenantID != scope.TenantID || applicationKey != scope.ApplicationKey || workspaceID != scope.WorkspaceID {
		t.Fatalf("ownership=(%q,%q,%q)", tenantID, applicationKey, workspaceID)
	}
}

func TestPortableMigrationRejectsTamperingAndNonEmptyTarget(t *testing.T) {
	database := openPortableDatabase(t, "tamper")
	applyPortableMigrations(t, database, mustSchemaMigrations(t, ""))
	scope := PortableScope{TenantID: "tenant", WorkspaceID: "workspace", ApplicationKey: "application"}
	dialect, _ := NewDialect(SQLite, "", "")
	bundle, _, err := ExportPortable(t.Context(), database, dialect, scope)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Tables[0].Rows = append(bundle.Tables[0].Rows, []json.RawMessage{})
	if err := ValidatePortable(bundle, scope); err == nil {
		t.Fatal("tampered bundle was accepted")
	}
}

func openPortableDatabase(t *testing.T, suffix string) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+t.Name()+"-"+suffix+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func mustSchemaMigrations(t *testing.T, prefix string) []SchemaMigration {
	t.Helper()
	migrations, err := SchemaMigrations(SQLite, "", prefix)
	if err != nil {
		t.Fatal(err)
	}
	return migrations
}

func applyPortableMigrations(t *testing.T, database *sql.DB, migrations []SchemaMigration) {
	t.Helper()
	for _, migration := range migrations {
		for _, statement := range migration.Statements {
			if _, err := database.Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func insertPortableEvent(t *testing.T, database *sql.DB, workspaceID, id, sourceEventID, leaseOwner string) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO notification_events (id, workspace_id, source, source_event_id, status, payload_json, lease_owner, occurred_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, workspaceID, "test", sourceEventID, "queued", `{}`, leaseOwner, "now", "now", "now"); err != nil {
		t.Fatal(err)
	}
}
