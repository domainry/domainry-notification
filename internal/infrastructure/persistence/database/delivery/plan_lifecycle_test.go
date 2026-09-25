package deliverystore_test

import (
	"database/sql"
	"testing"

	"github.com/domainry/domainry-foundation/mutation"
	"github.com/domainry/domainry-notification/internal/domain/delivery/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/timejson"
)

func TestChannelPlanClaimAndRetryAreFenced(t *testing.T) {
	db, store, _ := materializationStore(t)
	plan := delivery.Plan{ID: "plan-1", WorkspaceID: "workspace-1", EventID: "event-1", Channel: "slack", Status: "queued",
		CreatedAt: "2026-08-24T01:00:00.000000000Z", UpdatedAt: "2026-08-24T01:00:00.000000000Z"}
	insertPlan(t, db, plan)
	claimed, found, err := store.ClaimPlan(t.Context(), plan.WorkspaceID, plan.ID, "worker-1", "2026-08-24T01:01:00.000000000Z", "2026-08-24T01:01:30.000000000Z")
	if err != nil || !found || claimed.FencingToken != 1 || claimed.LeaseOwner != "worker-1" {
		t.Fatalf("claimed=%+v found=%v err=%v", claimed, found, err)
	}
	if err := store.RetryPlan(t.Context(), claimed, "connector.unavailable", "2026-08-24T01:02:00.000000000Z", "2026-08-24T01:01:01.000000000Z"); err != nil {
		t.Fatal(err)
	}
	var attemptKind, deliveryID, stage, disposition string
	if err := db.QueryRow(`SELECT attempt_kind, delivery_id, stage, disposition FROM _notification_delivery_attempts WHERE workspace_id = ? AND event_id = ?`, plan.WorkspaceID.String(), plan.EventID).Scan(&attemptKind, &deliveryID, &stage, &disposition); err != nil || attemptKind != "channel" || deliveryID != plan.ID || stage != "channel_dispatch" || disposition != "retry_scheduled" {
		t.Fatalf("attempt kind=%q delivery=%q stage=%q disposition=%q err=%v", attemptKind, deliveryID, stage, disposition, err)
	}
	stored, found, err := store.GetPlan(t.Context(), plan.WorkspaceID, plan.ID)
	if err != nil || !found || stored.Status != "queued" || stored.AttemptCount != 1 || stored.LeaseOwner != "" {
		t.Fatalf("stored=%+v found=%v err=%v", stored, found, err)
	}
	if err := store.FailPlan(t.Context(), claimed, "connector.failed", "2026-08-24T01:01:02.000000000Z"); !mutation.IsMutationConflict(err, mutation.MutationConflictLeaseLost) {
		t.Fatalf("stale transition err=%v", err)
	}
}

func TestChannelPlanBatchRejectsCrossWorkspaceMutation(t *testing.T) {
	_, store, _ := materializationStore(t)
	err := store.CompletePlanBatch(t.Context(), []delivery.Plan{
		{ID: "plan-1", WorkspaceID: "workspace-1"},
		{ID: "plan-2", WorkspaceID: "workspace-2"},
	}, "outbox-1", "2026-08-24T01:01:00.000000000Z")
	if err == nil {
		t.Fatal("expected cross-workspace batch rejection")
	}
}

func insertPlan(t *testing.T, executor *sql.DB, plan delivery.Plan) {
	t.Helper()
	raw, err := timejson.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	_, err = executor.Exec(`INSERT INTO _notification_deliveries VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, plan.ID, plan.WorkspaceID.String(), "delivery", plan.EventID, "", plan.TemplateKey, plan.Channel, plan.DedupeKey,
		plan.Status, string(raw), plan.AttemptCount, notification.TimestampMillis(plan.NextAttemptAt), plan.LastErrorCode, plan.OutboxMessageID, plan.LeaseOwner, notification.TimestampMillis(plan.LeaseExpiresAt), plan.FencingToken, notification.TimestampMillis(plan.CreatedAt), notification.TimestampMillis(plan.UpdatedAt))
	if err != nil {
		t.Fatal(err)
	}
}
