package inboxstore_test

import (
	"testing"

	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

func TestMailboxQueriesNeverEscapeExplicitRecipientBoundary(t *testing.T) {
	db, store := migratedStore(t)
	event := claimedEvent()
	insertClaimedEvent(t, db, event)
	items := []inbox.Item{mailboxItem(event, "item-user-1", "user-1"), mailboxItem(event, "item-user-2", "user-2")}
	if err := store.Materialize(t.Context(), event, items); err != nil {
		t.Fatal(err)
	}
	query := personalMailboxQuery("user-1")
	listed, more, err := store.ListItems(t.Context(), query)
	if err != nil || more || len(listed) != 1 || listed[0].RecipientUserID != "user-1" {
		t.Fatalf("listed=%+v more=%v err=%v", listed, more, err)
	}
	query.RecipientUserIDs = nil
	if _, _, err := store.ListItems(t.Context(), query); err == nil {
		t.Fatal("expected missing recipient-boundary rejection")
	}
}

func TestMailboxPersonalMutationsAndAlertAcknowledgement(t *testing.T) {
	db, store := migratedStore(t)
	event := claimedEvent()
	insertClaimedEvent(t, db, event)
	item := mailboxItem(event, "item-1", "user-1")
	second := mailboxItem(event, "item-2", "user-1")
	if err := store.Materialize(t.Context(), event, []inbox.Item{item, second}); err != nil {
		t.Fatal(err)
	}
	query := personalMailboxQuery("user-1")
	facets, err := store.CountFacets(t.Context(), query)
	if err != nil || facets.Unread != 2 || facets.ActionRequired != 2 || len(facets.Categories) != 1 {
		t.Fatalf("facets=%+v err=%v", facets, err)
	}
	acknowledged, found, err := store.AcknowledgeAlert(t.Context(), query, item.ID, "user-1", "2026-08-24T01:01:00.000000000Z")
	if err != nil || !found || acknowledged.AlertState != inbox.AlertAcknowledged {
		t.Fatalf("acknowledged=%+v found=%v err=%v", acknowledged, found, err)
	}
	marked, err := store.MarkAllRead(t.Context(), query, "2026-08-24T01:01:30.000000000Z")
	if err != nil || marked != 2 {
		t.Fatalf("marked=%d err=%v", marked, err)
	}
	read, found, err := store.SetRead(t.Context(), query, item.ID, "2026-08-24T01:02:00.000000000Z", "2026-08-24T01:02:00.000000000Z")
	if err != nil || !found || read.ReadAt == "" {
		t.Fatalf("read=%+v found=%v err=%v", read, found, err)
	}
	archived, found, err := store.SetArchived(t.Context(), query, item.ID, "2026-08-24T01:03:00.000000000Z", "2026-08-24T01:03:00.000000000Z")
	if err != nil || !found || archived.ArchivedAt == "" {
		t.Fatalf("archived=%+v found=%v err=%v", archived, found, err)
	}
	team := query
	team.Scope, team.ViewerUserID, team.RecipientUserIDs = inbox.ScopeTeam, "manager-1", []notification.UserID{"user-1"}
	if _, _, err := store.SetRead(t.Context(), team, item.ID, "now", "now"); err == nil {
		t.Fatal("expected team-scope mutation rejection")
	}
}

func TestMailboxWorkspaceIsolationAndAtomicMarkAllRead(t *testing.T) {
	db, store := migratedStore(t)
	first := claimedEvent()
	insertClaimedEvent(t, db, first)
	if err := store.Materialize(t.Context(), first, []inbox.Item{mailboxItem(first, "item-1", "user-1"), mailboxItem(first, "item-2", "user-1")}); err != nil {
		t.Fatal(err)
	}
	second := claimedEvent()
	second.ID, second.WorkspaceID, second.SourceEventID = "event-2", "workspace-2", "run-2"
	second.ChannelPlans = nil
	insertClaimedEvent(t, db, second)
	if err := store.Materialize(t.Context(), second, []inbox.Item{mailboxItem(second, "item-3", "user-1")}); err != nil {
		t.Fatal(err)
	}

	query := personalMailboxQuery("user-1")
	updated, err := store.MarkAllRead(t.Context(), query, "2026-08-24T01:05:00.000000000Z")
	if err != nil || updated != 2 {
		t.Fatalf("updated=%d err=%v", updated, err)
	}
	var workspaceOneUnread, workspaceTwoUnread int
	if err := db.QueryRow(`SELECT COUNT(*) FROM _notification_inbox_items WHERE workspace_id = ? AND read_at = 0`, "workspace-1").Scan(&workspaceOneUnread); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM _notification_inbox_items WHERE workspace_id = ? AND read_at = 0`, "workspace-2").Scan(&workspaceTwoUnread); err != nil {
		t.Fatal(err)
	}
	if workspaceOneUnread != 0 || workspaceTwoUnread != 1 {
		t.Fatalf("unread workspace-1=%d workspace-2=%d", workspaceOneUnread, workspaceTwoUnread)
	}

	foreign := personalMailboxQuery("user-1")
	foreign.WorkspaceID = "workspace-2"
	if _, _, err := store.ListItems(t.Context(), foreign); err == nil {
		t.Fatal("cross-workspace read was accepted")
	}
	if _, _, err := store.SetRead(t.Context(), foreign, "item-3", "now", "now"); err == nil {
		t.Fatal("cross-workspace mutation was accepted")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM _notification_inbox_items WHERE workspace_id = ? AND read_at = 0`, "workspace-2").Scan(&workspaceTwoUnread); err != nil || workspaceTwoUnread != 1 {
		t.Fatalf("unauthorized mutation changed foreign rows: unread=%d err=%v", workspaceTwoUnread, err)
	}
}

func mailboxItem(event inbox.Event, id string, recipient notification.UserID) inbox.Item {
	return inbox.Item{ID: id, WorkspaceID: event.WorkspaceID, RecipientUserID: recipient, EventID: event.ID,
		EventType: event.EventType, Source: event.Source, Category: event.Category, Severity: event.Severity, Title: "Build failed", Body: "Open run",
		ActionState: inbox.ActionOpen, AlertState: inbox.AlertFiring, GroupKey: event.GroupKey, OccurrenceCount: 1,
		FirstOccurredAt: event.OccurredAt, LastOccurredAt: event.OccurredAt, CreatedAt: event.CreatedAt, UpdatedAt: event.UpdatedAt}
}

func personalMailboxQuery(user notification.UserID) inbox.Query {
	return inbox.Query{WorkspaceID: "workspace-1", ViewerUserID: user, RecipientUserID: user, RecipientUserIDs: []notification.UserID{user}, Scope: inbox.ScopeMine, Mailbox: inbox.MailboxInbox, Limit: 20}
}
