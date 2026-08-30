package inboxstore_test

import (
	"testing"

	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
)

func TestGovernanceMetricsAggregateInboxAndFailureEvidence(t *testing.T) {
	db, store := migratedStore(t)
	event := claimedEvent()
	insertClaimedEvent(t, db, event)
	items := []inbox.Item{mailboxItem(event, "item-1", "user-1"), mailboxItem(event, "item-2", "user-2")}
	items[1].ActionState, items[1].AlertState, items[1].OccurrenceCount = inbox.ActionNone, inbox.AlertResolved, 3
	if err := store.Materialize(t.Context(), event, items); err != nil {
		t.Fatal(err)
	}
	_, err := db.Exec(`INSERT INTO _notification_event_failures (id, workspace_id, event_id, event_type, source, source_event_id, stage, error_code, attempt, disposition, retryable, next_attempt_at, fencing_token, occurred_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"failure-1", "workspace-1", event.ID, event.EventType, event.Source, event.SourceEventID, "materialization", "backend.notification.failed", 1, "dead_letter", 0, "", 1, event.OccurredAt)
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := store.GovernanceMetrics(t.Context(), "workspace-1", "2026-08-24T00:00:00.000000000Z")
	if err != nil {
		t.Fatal(err)
	}
	if metrics.GeneratedAt != "2026-08-24T02:00:00.000000000Z" || metrics.Summary.Items != 2 || metrics.Summary.Occurrences != 4 || metrics.Summary.ActionRequired != 1 || metrics.Summary.ActiveAlerts != 1 {
		t.Fatalf("metrics=%+v", metrics)
	}
	if metrics.Failures.Total != 1 || metrics.Failures.DeadLetter != 1 || len(metrics.Failures.ByStage) != 1 || len(metrics.ByEventType) != 1 {
		t.Fatalf("metrics=%+v", metrics)
	}
}
