package operationstore_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	sharedoperation "github.com/domainry/domainry-foundation/operation"
	ormdialect "github.com/domainry/domainry-orm/dialect"
	ormmigration "github.com/domainry/domainry-orm/migration"
	_ "modernc.org/sqlite"
)

type topologyMigrationRegistrar struct {
	database *sql.DB
	mu       sync.Mutex
	applied  map[string]bool
}

func (*topologyMigrationRegistrar) Driver() string { return "sqlite" }
func (*topologyMigrationRegistrar) Schema() string { return "" }
func (r *topologyMigrationRegistrar) ApplyOwnedMigrations(ctx context.Context, owner string, migrations []ormmigration.Migration) error {
	if owner != sharedoperation.MigrationOwner {
		return fmt.Errorf("unexpected migration owner %q", owner)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.applied == nil {
		r.applied = map[string]bool{}
	}
	if r.applied[owner] {
		return nil
	}
	for _, migration := range migrations {
		for _, statement := range migration.Statements {
			if _, err := r.database.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
	}
	r.applied[owner] = true
	return nil
}

func TestFoundationOperationsCompositionSharesModuleDatabaseAndIsolatesSaaSDatabases(t *testing.T) {
	dialect, err := ormdialect.ParseRenderer("sqlite", "", "")
	if err != nil {
		t.Fatal(err)
	}
	command := sharedoperation.Command{
		ID: "operation-1", Scope: sharedoperation.Scope{WorkspaceID: "workspace-1", ResourceType: "notification_template", ResourceID: "welcome"},
		Owner: "notification", Kind: "template_publication", ActionKey: "notification.template.publish",
		IdempotencyKey: "publish-welcome", RequestFingerprint: "sha256:one", RequestedBy: "user-1",
		Reason: "publish template", StatusURL: "/operations/operation-1", CreatedAt: time.Date(2026, 9, 23, 8, 0, 0, 0, time.UTC),
	}

	moduleDB := openTopologyDatabase(t, "module")
	moduleMigrations := &topologyMigrationRegistrar{database: moduleDB}
	moduleA, err := sharedoperation.Open(t.Context(), moduleDB, dialect, moduleMigrations)
	if err != nil {
		t.Fatal(err)
	}
	moduleB, err := sharedoperation.Open(t.Context(), moduleDB, dialect, moduleMigrations)
	if err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := moduleA.Claim(t.Context(), command); err != nil || !claimed {
		t.Fatalf("first Module claim claimed=%v err=%v", claimed, err)
	}
	if err := moduleA.Complete(t.Context(), sharedoperation.Completion{
		ID: command.ID, Scope: command.Scope, Owner: command.Owner, Kind: command.Kind,
		IdempotencyKey: command.IdempotencyKey, RequestFingerprint: command.RequestFingerprint,
		Result: json.RawMessage(`{"published":true}`), CompletedAt: command.CreatedAt.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	replayed, claimed, err := moduleB.Claim(t.Context(), command)
	if err != nil || claimed || replayed.Status != sharedoperation.StatusSucceeded {
		t.Fatalf("second Module Store replay=%+v claimed=%v err=%v", replayed, claimed, err)
	}
	assertTopologyTableCount(t, moduleDB, sharedoperation.TableName, 1)
	assertTopologyRowCount(t, moduleDB, 1)

	for _, name := range []string{"saas-a", "saas-b"} {
		database := openTopologyDatabase(t, name)
		store, err := sharedoperation.Open(t.Context(), database, dialect, &topologyMigrationRegistrar{database: database})
		if err != nil {
			t.Fatal(err)
		}
		if _, claimed, err := store.Claim(t.Context(), command); err != nil || !claimed {
			t.Fatalf("%s independent claim claimed=%v err=%v", name, claimed, err)
		}
		assertTopologyTableCount(t, database, sharedoperation.TableName, 1)
		assertTopologyRowCount(t, database, 1)
	}
}

func openTopologyDatabase(t *testing.T, name string) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+t.Name()+"-"+name+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func assertTopologyTableCount(t *testing.T, database *sql.DB, table string, want int) {
	t.Helper()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil || count != want {
		t.Fatalf("table %s count=%d want=%d err=%v", table, count, want, err)
	}
}

func assertTopologyRowCount(t *testing.T, database *sql.DB, want int) {
	t.Helper()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM _operations`).Scan(&count); err != nil || count != want {
		t.Fatalf("operation row count=%d want=%d err=%v", count, want, err)
	}
}
