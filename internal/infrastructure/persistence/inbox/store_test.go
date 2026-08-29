package inboxstore_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/domainry/domainry-notification/internal/domain/delivery/service"
	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	sqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence"
	ormdialect "github.com/domainry/domainry-orm/dialect"
	"github.com/domainry/domainry-orm/sqlhost"
	_ "modernc.org/sqlite"
)

type passthroughScope struct{}

func (passthroughScope) Context(ctx context.Context, _ notification.WorkspaceID) context.Context {
	return ctx
}

type storeClock struct{ value time.Time }

func (c storeClock) Now() time.Time { return c.value }

type queueScopes struct{}

func (*queueScopes) Register(context.Context, sqlhost.Executor, notification.WorkKind, notification.WorkspaceID, string) error {
	return nil
}

func (*queueScopes) Workspaces(context.Context, sqlhost.Queryer, notification.WorkKind, int) ([]notification.WorkspaceID, error) {
	return nil, nil
}

func migratedStore(t *testing.T) (*sql.DB, *sqlstore.Store) {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	migrations, err := sqlstore.SchemaMigrations(sqlstore.SQLite, "", "")
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
	dialect, _ := ormdialect.ParseRenderer("sqlite", "", "")
	store, err := sqlstore.New(sqlstore.Config{
		Database: database, Dialect: dialect, WorkspaceScope: passthroughScope{}, QueueScopes: &queueScopes{},
		Clock: storeClock{value: time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return database, store
}

func claimedEvent() inbox.Event {
	return inbox.Event{
		ID: "event-1", WorkspaceID: "workspace-1", Source: "workflow", SourceEventID: "run-1", EventType: "workflow.run.failed",
		Category: "workflow", Severity: "error", Surface: "business_workspace", GroupKey: "run-1", AlertState: inbox.AlertFiring,
		Status: inbox.EventProcessing, LeaseOwner: "worker-1", FencingToken: 3, OccurredAt: "2026-08-24T01:00:00.000000000Z",
		CreatedAt: "2026-08-24T01:00:00.000000000Z", UpdatedAt: "2026-08-24T01:00:00.000000000Z",
		ChannelPlans: []delivery.Plan{{ID: "plan-1", WorkspaceID: "workspace-1", EventID: "event-1", Channel: "slack", Status: "queued", CreatedAt: "2026-08-24T01:00:00.000000000Z", UpdatedAt: "2026-08-24T01:00:00.000000000Z"}},
	}
}

func insertClaimedEvent(t *testing.T, database *sql.DB, event inbox.Event) {
	t.Helper()
	raw := `{"id":"event-1"}`
	_, err := database.Exec(`INSERT INTO notification_events VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, event.ID, event.WorkspaceID, event.Source, event.SourceEventID, event.Status, raw, 0, "", "", event.LeaseOwner, "", event.FencingToken, event.OccurredAt, event.CreatedAt, event.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
}
