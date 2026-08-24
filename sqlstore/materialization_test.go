package sqlstore_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/domainry/domainry-notification"
	"github.com/domainry/domainry-notification/delivery"
	"github.com/domainry/domainry-notification/inbox"
	"github.com/domainry/domainry-notification/sqlstore"
	_ "modernc.org/sqlite"
)

type passthroughScope struct{}

func (passthroughScope) Context(ctx context.Context, _ notification.WorkspaceID) context.Context {
	return ctx
}

type queueScopes struct{ registrations []notification.Work }

func (q *queueScopes) Register(_ context.Context, _ sqlstore.Executor, kind notification.WorkKind, workspace notification.WorkspaceID, _ string) error {
	q.registrations = append(q.registrations, notification.Work{Kind: kind, WorkspaceID: workspace})
	return nil
}
func (*queueScopes) Workspaces(context.Context, sqlstore.Queryer, notification.WorkKind, int) ([]notification.WorkspaceID, error) {
	return nil, nil
}

func TestMaterializeCommitsAllProjectionsAndFencedEvent(t *testing.T) {
	db, store, scopes := materializationStore(t)
	event := claimedEvent()
	insertClaimedEvent(t, db, event)
	item := inbox.Item{
		ID: "item-1", WorkspaceID: event.WorkspaceID, RecipientUserID: "user-1", Surface: event.Surface,
		EventID: event.ID, EventType: event.EventType, Source: event.Source, Category: event.Category, Severity: event.Severity,
		Title: "Build failed", Body: "Open the run", ActionState: inbox.ActionOpen, AlertState: inbox.AlertFiring, GroupKey: event.GroupKey,
		OccurrenceCount: 1, FirstOccurredAt: event.OccurredAt, LastOccurredAt: event.OccurredAt, CreatedAt: event.CreatedAt, UpdatedAt: event.UpdatedAt,
	}
	if err := store.Materialize(t.Context(), event, []inbox.Item{item}); err != nil {
		t.Fatal(err)
	}
	for table, want := range map[string]int{"notification_inbox_items": 1, "notification_alert_groups": 1, "notification_channel_plans": 1} {
		var got int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil || got != want {
			t.Fatalf("%s count=%d err=%v", table, got, err)
		}
	}
	var status, owner string
	if err := db.QueryRow("SELECT status, lease_owner FROM notification_events WHERE id = ?", event.ID).Scan(&status, &owner); err != nil || status != "materialized" || owner != "" {
		t.Fatalf("status=%q owner=%q err=%v", status, owner, err)
	}
	if len(scopes.registrations) != 1 || scopes.registrations[0].Kind != notification.WorkChannelPlan {
		t.Fatalf("registrations=%+v", scopes.registrations)
	}
}

func TestMaterializeRollsBackWhenLeaseWasLost(t *testing.T) {
	db, store, _ := materializationStore(t)
	event := claimedEvent()
	insertClaimedEvent(t, db, event)
	event.FencingToken++
	item := inbox.Item{ID: "item-1", WorkspaceID: event.WorkspaceID, RecipientUserID: "user-1", Surface: event.Surface, EventID: event.ID,
		LastOccurredAt: event.OccurredAt, FirstOccurredAt: event.OccurredAt, CreatedAt: event.CreatedAt, UpdatedAt: event.UpdatedAt}
	if err := store.Materialize(t.Context(), event, []inbox.Item{item}); !errors.Is(err, sqlstore.ErrLeaseLost) {
		t.Fatalf("err=%v", err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM notification_inbox_items").Scan(&count); err != nil || count != 0 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}

func materializationStore(t *testing.T) (*sql.DB, *sqlstore.Store, *queueScopes) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	statements := []string{
		`CREATE TABLE notification_events (id TEXT, workspace_id TEXT, source TEXT, source_event_id TEXT, status TEXT, payload_json TEXT, attempt_count INTEGER, next_attempt_at TEXT, last_error_code TEXT, lease_owner TEXT, lease_expires_at TEXT, fencing_token INTEGER, occurred_at TEXT, created_at TEXT, updated_at TEXT, UNIQUE(workspace_id,id), UNIQUE(workspace_id,source,source_event_id))`,
		`CREATE TABLE notification_inbox_items (id TEXT, workspace_id TEXT, recipient_user_id TEXT, surface TEXT, event_id TEXT, event_type TEXT, source TEXT, category TEXT, severity TEXT, title TEXT, body TEXT, search_text TEXT, payload_json TEXT, subject_type TEXT, subject_id TEXT, action_state TEXT, alert_state TEXT, group_key TEXT, occurrence_count INTEGER, first_occurred_at TEXT, last_occurred_at TEXT, read_at TEXT, archived_at TEXT, expires_at TEXT, created_at TEXT, updated_at TEXT, UNIQUE(workspace_id,id))`,
		`CREATE TABLE notification_alert_groups (workspace_id TEXT, recipient_user_id TEXT, surface TEXT, group_key TEXT, state TEXT, occurrence_count INTEGER, first_occurred_at TEXT, last_occurred_at TEXT, acknowledged_at TEXT, acknowledged_by TEXT, resolved_at TEXT, last_event_id TEXT, updated_at TEXT, UNIQUE(workspace_id,recipient_user_id,surface,group_key))`,
		`CREATE TABLE notification_channel_plans (id TEXT, workspace_id TEXT, event_id TEXT, channel TEXT, status TEXT, payload_json TEXT, attempt_count INTEGER, next_attempt_at TEXT, last_error_code TEXT, outbox_message_id TEXT, lease_owner TEXT, lease_expires_at TEXT, fencing_token INTEGER, created_at TEXT, updated_at TEXT, UNIQUE(workspace_id,id))`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	dialect, _ := sqlstore.NewDialect(sqlstore.SQLite, "", "")
	scopes := &queueScopes{}
	store, err := sqlstore.New(sqlstore.Config{Database: db, Dialect: dialect, WorkspaceScope: passthroughScope{}, QueueScopes: scopes})
	if err != nil {
		t.Fatal(err)
	}
	return db, store, scopes
}

func claimedEvent() inbox.Event {
	return inbox.Event{ID: "event-1", WorkspaceID: "workspace-1", Source: "workflow", SourceEventID: "run-1", EventType: "workflow.run.failed",
		Category: "workflow", Severity: "error", Surface: "business_workspace", GroupKey: "run-1", AlertState: inbox.AlertFiring,
		Status: inbox.EventProcessing, LeaseOwner: "worker-1", FencingToken: 3, OccurredAt: "2026-08-24T01:00:00.000000000Z",
		CreatedAt: "2026-08-24T01:00:00.000000000Z", UpdatedAt: "2026-08-24T01:00:00.000000000Z",
		ChannelPlans: []delivery.Plan{{ID: "plan-1", WorkspaceID: "workspace-1", EventID: "event-1", Channel: "slack", Status: "queued", CreatedAt: "2026-08-24T01:00:00.000000000Z", UpdatedAt: "2026-08-24T01:00:00.000000000Z"}}}
}

func insertClaimedEvent(t *testing.T, db *sql.DB, event inbox.Event) {
	t.Helper()
	raw := `{"id":"event-1"}`
	_, err := db.Exec(`INSERT INTO notification_events VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, event.ID, event.WorkspaceID, event.Source, event.SourceEventID, event.Status, raw, 0, "", "", event.LeaseOwner, "", event.FencingToken, event.OccurredAt, event.CreatedAt, event.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
}
