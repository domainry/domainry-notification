package deliverystore_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/domainry/domainry-notification/internal/domain/delivery/service"
	sqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence"
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
	stored, found, err := store.GetPlan(t.Context(), plan.WorkspaceID, plan.ID)
	if err != nil || !found || stored.Status != "queued" || stored.AttemptCount != 1 || stored.LeaseOwner != "" {
		t.Fatalf("stored=%+v found=%v err=%v", stored, found, err)
	}
	if err := store.FailPlan(t.Context(), claimed, "connector.failed", "2026-08-24T01:01:02.000000000Z"); !errors.Is(err, sqlstore.ErrLeaseLost) {
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
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	_, err = executor.Exec(`INSERT INTO notification_channel_plans VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, plan.ID, plan.WorkspaceID.String(), plan.EventID, plan.Channel,
		plan.Status, string(raw), plan.AttemptCount, plan.NextAttemptAt, plan.LastErrorCode, plan.OutboxMessageID, plan.LeaseOwner, plan.LeaseExpiresAt, plan.FencingToken, plan.CreatedAt, plan.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
}
