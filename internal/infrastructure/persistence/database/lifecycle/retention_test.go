package lifecyclestore_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/domainry/domainry-notification-sdk/contract"
)

func TestRetentionArchivesAndPurgesOnlyTerminalWorkspaceRows(t *testing.T) {
	db, store, content := migratedStoreWithArtifactContent(t)
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
	var events int
	if err := db.QueryRow(`SELECT COUNT(*) FROM _notification_events`).Scan(&events); err != nil || events != 2 {
		t.Fatalf("events=%d err=%v", events, err)
	}
	var artifactID, contentSHA256, storageReference, status, scanStatus, metadataJSON string
	var sizeBytes int64
	if err := db.QueryRow(`SELECT id, content_sha256, size_bytes, storage_reference, status, scan_status, metadata_json FROM _artifacts WHERE workspace_id = ? AND owner = 'lifecycle' AND kind = 'archive'`, "workspace-a").
		Scan(&artifactID, &contentSHA256, &sizeBytes, &storageReference, &status, &scanStatus, &metadataJSON); err != nil {
		t.Fatal(err)
	}
	if status != "available" || scanStatus != "not_required" || artifactID == "" || storageReference == "" {
		t.Fatalf("archive Artifact id=%q reference=%q status=%q scan=%q", artifactID, storageReference, status, scanStatus)
	}
	var bindings int
	if err := db.QueryRow(`SELECT COUNT(*) FROM _artifact_bindings WHERE workspace_id = ? AND artifact_id = ?`, "workspace-a", artifactID).Scan(&bindings); err != nil || bindings != 2 {
		t.Fatalf("archive bindings=%d err=%v", bindings, err)
	}
	var sourceBindings int
	if err := db.QueryRow(`SELECT COUNT(*) FROM _artifact_bindings WHERE workspace_id = ? AND artifact_id = ? AND owner = 'lifecycle' AND kind = 'object_field' AND resource_type = '_notification_events' AND resource_id = 'terminal' AND field_key = ?`, "workspace-a", artifactID, policy.Key).Scan(&sourceBindings); err != nil || sourceBindings != 1 {
		t.Fatalf("archive source bindings=%d err=%v", sourceBindings, err)
	}
	reader, err := content.Open(t.Context(), "workspace-a", storageReference)
	if err != nil {
		t.Fatal(err)
	}
	raw, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read archive blob error=%v close=%v", readErr, closeErr)
	}
	digest := sha256.Sum256(raw)
	if sizeBytes != int64(len(raw)) || contentSHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("archive blob size=%d/%d digest=%q/%q", sizeBytes, len(raw), contentSHA256, hex.EncodeToString(digest[:]))
	}
	var metadata map[string]string
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil || metadata["owner"] != "notification" || metadata["job_id"] != "job-archive" {
		t.Fatalf("archive metadata=%v err=%v", metadata, err)
	}
	var retired int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = '_lifecycle_archive_entries'`).Scan(&retired); err != nil || retired != 0 {
		t.Fatalf("retired archive table count=%d err=%v", retired, err)
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
