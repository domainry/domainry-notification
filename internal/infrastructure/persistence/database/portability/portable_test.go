package portabilitystore

import (
	"database/sql"
	"encoding/json"
	"testing"

	ormdialect "github.com/domainry/domainry-orm/dialect"
	_ "modernc.org/sqlite"
)

func TestPortableMigrationFiltersWorkspaceImportsOwnershipAndReconciles(t *testing.T) {
	source := openPortableDatabase(t, "source")
	applyPortableMigrations(t, source, mustSchemaMigrations(t, ""))
	if _, err := source.Exec(`INSERT INTO _notification_templates (template_key, draft_json, published_json, published_version, status, updated_by, created_at, updated_at, workspace_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, "welcome", nil, `{}`, 1, "active", "admin", "now", "now", "workspace"); err != nil {
		t.Fatal(err)
	}
	insertPortableEvent(t, source, "workspace", "event-one", "source-one", "worker")
	insertPortableEvent(t, source, "other-workspace", "event-other", "source-other", "")
	insertPortableArchive(t, source, "workspace", "archive-one")
	insertPortableArchive(t, source, "other-workspace", "archive-other")
	sourceDialect, _ := ormdialect.ParseRenderer("sqlite", "", "")
	scope := PortableScope{WorkspaceID: "workspace", ApplicationKey: "application"}
	bundle, inventory, err := ExportPortable(t.Context(), source, sourceDialect, scope)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Rows != 3 || inventory.ActiveLeases != 1 || inventory.Tables["_notification_events"] != 1 || inventory.Tables["_notification_retention_archive_entries"] != 1 || bundle.Fingerprint == "" {
		t.Fatalf("inventory=%+v bundle=%+v", inventory, bundle)
	}

	target := openPortableDatabase(t, "target")
	prefix := "application_"
	migrations, err := ApplicationSchemaMigrations(SQLite, "", prefix, ApplicationScope{WorkspaceID: scope.WorkspaceID, ApplicationKey: scope.ApplicationKey})
	if err != nil {
		t.Fatal(err)
	}
	applyPortableMigrations(t, target, migrations)
	targetDialect, _ := ormdialect.ParseRenderer("sqlite", "", prefix)
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
	var applicationKey, workspaceID string
	if err := target.QueryRow(`SELECT application_key, workspace_id FROM application__notification_events WHERE id = ?`, "event-one").Scan(&applicationKey, &workspaceID); err != nil {
		t.Fatal(err)
	}
	if applicationKey != scope.ApplicationKey || workspaceID != scope.WorkspaceID {
		t.Fatalf("ownership=(%q,%q)", applicationKey, workspaceID)
	}
	var archivedWorkspaceID string
	if err := target.QueryRow(`SELECT workspace_id FROM application__notification_retention_archive_entries WHERE id = ?`, "archive-one").Scan(&archivedWorkspaceID); err != nil {
		t.Fatal(err)
	}
	if archivedWorkspaceID != scope.WorkspaceID {
		t.Fatalf("archive workspace=%q", archivedWorkspaceID)
	}
}

func TestPortableMigrationRejectsTamperingAndNonEmptyTarget(t *testing.T) {
	database := openPortableDatabase(t, "tamper")
	applyPortableMigrations(t, database, mustSchemaMigrations(t, ""))
	scope := PortableScope{WorkspaceID: "workspace", ApplicationKey: "application"}
	dialect, _ := ormdialect.ParseRenderer("sqlite", "", "")
	bundle, _, err := ExportPortable(t.Context(), database, dialect, scope)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Tables[0].Rows = append(bundle.Tables[0].Rows, []json.RawMessage{})
	if err := ValidatePortable(bundle, scope); err == nil {
		t.Fatal("tampered bundle was accepted")
	}
}

func TestPortableMigrationReconcilesEachWorkspaceWithoutCrossContamination(t *testing.T) {
	source := openPortableDatabase(t, "multi-workspace-source")
	applyPortableMigrations(t, source, mustSchemaMigrations(t, ""))
	insertPortableEvent(t, source, "workspace-a", "event-a", "source-a", "")
	insertPortableEvent(t, source, "workspace-b", "event-b", "source-b", "")
	insertPortableArchive(t, source, "workspace-a", "archive-a")
	insertPortableArchive(t, source, "workspace-b", "archive-b")
	sourceDialect, _ := ormdialect.ParseRenderer("sqlite", "", "")

	for _, workspaceID := range []string{"workspace-a", "workspace-b"} {
		t.Run(workspaceID, func(t *testing.T) {
			scope := PortableScope{WorkspaceID: workspaceID, ApplicationKey: "application"}
			bundle, inventory, err := ExportPortable(t.Context(), source, sourceDialect, scope)
			if err != nil {
				t.Fatal(err)
			}
			if inventory.Rows != 2 || inventory.Tables["_notification_events"] != 1 || inventory.Tables["_notification_retention_archive_entries"] != 1 {
				t.Fatalf("source inventory=%+v", inventory)
			}
			target := openPortableDatabase(t, "multi-workspace-target-"+workspaceID)
			prefix := "application_"
			migrations, err := ApplicationSchemaMigrations(SQLite, "", prefix, ApplicationScope(scope))
			if err != nil {
				t.Fatal(err)
			}
			applyPortableMigrations(t, target, migrations)
			targetDialect, _ := ormdialect.ParseRenderer("sqlite", "", prefix)
			receipt, err := ImportPortable(t.Context(), target, targetDialect, scope, bundle)
			if err != nil {
				t.Fatal(err)
			}
			if receipt.Rows != inventory.Rows || receipt.Fingerprint != inventory.Fingerprint {
				t.Fatalf("receipt=%+v inventory=%+v", receipt, inventory)
			}
			var importedWorkspace, importedEvent string
			if err := target.QueryRow(`SELECT workspace_id,id FROM application__notification_events`).Scan(&importedWorkspace, &importedEvent); err != nil {
				t.Fatal(err)
			}
			if importedWorkspace != workspaceID || importedEvent != "event-"+workspaceID[len("workspace-"):] {
				t.Fatalf("imported workspace=%q event=%q", importedWorkspace, importedEvent)
			}
			var events int
			if err := target.QueryRow(`SELECT COUNT(*) FROM application__notification_events`).Scan(&events); err != nil {
				t.Fatal(err)
			}
			if events != 1 {
				t.Fatalf("cross-workspace event count=%d", events)
			}
		})
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
	if _, err := database.Exec(`INSERT INTO _notification_events (id, workspace_id, source, source_event_id, status, payload_json, lease_owner, occurred_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, workspaceID, "test", sourceEventID, "queued", `{}`, leaseOwner, "now", "now", "now"); err != nil {
		t.Fatal(err)
	}
}

func insertPortableArchive(t *testing.T, database *sql.DB, workspaceID, id string) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO _notification_retention_archive_entries (id, workspace_id, policy_key, policy_version, job_id, source_table, resource_id, payload_hash, payload_json, archived_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, workspaceID, "notification.history.v1", "1", "job", "_notification_events", "event", "hash", `{}`, "now"); err != nil {
		t.Fatal(err)
	}
}
