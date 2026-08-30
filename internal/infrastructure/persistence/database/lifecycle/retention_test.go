package lifecyclestore_test

import (
	"testing"
	"time"

	"github.com/domainry/domainry-notification-sdk/contract"
)

func TestRetentionArchivesAndPurgesOnlyTerminalWorkspaceRows(t *testing.T) {
	db, store := migratedStore(t)
	old := "2025-01-01T00:00:00Z"
	for _, event := range []struct{ id, status string }{{"terminal", "materialized"}, {"active", "queued"}} {
		if _, err := db.Exec(`INSERT INTO _notification_events (id, workspace_id, source, source_event_id, status, payload_json, occurred_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, event.id, "workspace-a", "test", event.id, event.status, `{}`, old, old, old); err != nil {
			t.Fatal(err)
		}
	}
	policy := contract.NotificationRetentionPolicy{Key: contract.NotificationRetentionHistoryPolicy, Version: "1", DefaultRetentionSeconds: 24 * 60 * 60}
	now := time.Date(2026, 8, 29, 1, 0, 0, 0, time.UTC)
	preview, err := store.PreviewRetention(t.Context(), contract.NotificationRetentionPreviewRequest{WorkspaceID: "workspace-a", Policy: policy, Now: now})
	if err != nil || preview.Rows != 1 {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	archive, err := store.ProcessRetentionBatch(t.Context(), contract.NotificationRetentionBatchRequest{JobID: "job-archive", WorkspaceID: "workspace-a", Operation: "archive", Now: now, Policy: policy, Limit: 100})
	if err != nil || archive.Archived != 1 || archive.Purged != 0 {
		t.Fatalf("archive=%+v err=%v", archive, err)
	}
	var events, archives int
	if err := db.QueryRow(`SELECT COUNT(*) FROM _notification_events`).Scan(&events); err != nil || events != 2 {
		t.Fatalf("events=%d err=%v", events, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM _notification_retention_archive_entries`).Scan(&archives); err != nil || archives != 1 {
		t.Fatalf("archives=%d err=%v", archives, err)
	}
	purge, err := store.ProcessRetentionBatch(t.Context(), contract.NotificationRetentionBatchRequest{JobID: "job-purge", WorkspaceID: "workspace-a", Operation: "purge", Now: now, Policy: policy, Limit: 100})
	if err != nil || purge.Purged != 1 {
		t.Fatalf("purge=%+v err=%v", purge, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM _notification_events`).Scan(&events); err != nil || events != 1 {
		t.Fatalf("events after purge=%d err=%v", events, err)
	}
}

func TestRetentionLegalHoldFailsClosedForMatchingResource(t *testing.T) {
	db, store := migratedStore(t)
	old := "2025-01-01T00:00:00Z"
	if _, err := db.Exec(`INSERT INTO _notification_events (id, workspace_id, source, source_event_id, status, payload_json, occurred_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, "held", "workspace-a", "test", "held", "failed", `{}`, old, old, old); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 29, 1, 0, 0, 0, time.UTC)
	result, err := store.ProcessRetentionBatch(t.Context(), contract.NotificationRetentionBatchRequest{
		JobID: "job-held", WorkspaceID: "workspace-a", Operation: "purge", Now: now, Limit: 100,
		Policy: contract.NotificationRetentionPolicy{Key: contract.NotificationRetentionHistoryPolicy, Version: "1", DefaultRetentionSeconds: 1},
		Holds:  []contract.NotificationRetentionHold{{Owner: "notification", ResourceType: "_notification_events", ResourceID: "held", StartsAt: now.Add(-time.Hour)}},
	})
	if err != nil || result.Skipped != 1 || result.Purged != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
