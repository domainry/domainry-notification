package operationstore_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/domainry/domainry-notification-sdk/modulehost"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"
	operationstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/operation"
	storeschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/schema"
	sqliteschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/sqlite/schema"
	ormdialect "github.com/domainry/domainry-orm/dialect"
	_ "modernc.org/sqlite"
)

func TestOperationControlStorePersistsAndFencesRevision(t *testing.T) {
	database, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	defer database.Close()
	dialect, err := ormdialect.ParseRenderer("sqlite", "", "")
	if err != nil {
		t.Fatal(err)
	}
	migrations, err := storeschema.SharedOperationSchemaMigrations(sqliteschema.Profile{}, dialect)
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		for _, statement := range migration.Statements {
			if _, err := database.Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	store, err := operationstore.New(base.NewSQLStore(database, dialect))
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.GetOperationControl(t.Context(), "notification_migration", "workspace_cutover", "workspace-1"); err != nil || found {
		t.Fatalf("unexpected initial control: found=%v err=%v", found, err)
	}
	now := time.Date(2026, 9, 23, 1, 0, 0, 0, time.UTC)
	control := modulehost.OperationControl{
		SystemPurpose: "notification_migration", Kind: "workspace_cutover", Owner: "workspace-1", State: "frozen",
		Reason: "notification workspace migration", Reference: `{"migration_id":"migration-1","role":"source"}`,
		UpdatedBy: "notification", Revision: 1, UpdatedAt: now,
	}
	if changed, err := store.PutOperationControl(t.Context(), control, 0); err != nil || !changed {
		t.Fatalf("create control: changed=%v err=%v", changed, err)
	}
	if changed, err := store.PutOperationControl(t.Context(), control, 0); err != nil || changed {
		t.Fatalf("duplicate create control: changed=%v err=%v", changed, err)
	}
	control.State, control.Revision, control.UpdatedAt = "active", 2, now.Add(time.Minute)
	if changed, err := store.PutOperationControl(t.Context(), control, 1); err != nil || !changed {
		t.Fatalf("transition control: changed=%v err=%v", changed, err)
	}
	control.State, control.Revision = "cutover", 2
	if changed, err := store.PutOperationControl(t.Context(), control, 1); err != nil || changed {
		t.Fatalf("stale control transition: changed=%v err=%v", changed, err)
	}
	persisted, found, err := store.GetOperationControl(t.Context(), control.SystemPurpose, control.Kind, control.Owner)
	if err != nil || !found || persisted.State != "active" || persisted.Revision != 2 {
		t.Fatalf("persisted control=%+v found=%v err=%v", persisted, found, err)
	}
}
