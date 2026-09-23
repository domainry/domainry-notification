package templatestore_test

import (
	"errors"
	"testing"

	metadatasdk "github.com/domainry/domainry-metadata-sdk"
	"github.com/domainry/domainry-notification/internal/domain/template/service"
)

func TestTemplateRecordDraftPublishVersionAndDisableLifecycle(t *testing.T) {
	database, store := migratedStore(t)
	draft := template.Template{Key: "workflow.failed", Name: "Workflow failed", Channel: "email", Status: "draft", Version: 1,
		DefaultLocale: "en", Locales: map[string]template.Content{"en": {Subject: "Failed", Text: "Run failed"}}}
	record, err := store.SaveDraft(t.Context(), draft, "", "admin-1")
	if err != nil || record.Draft == nil || record.Published != nil || record.UpdatedAt == "" {
		t.Fatalf("draft record=%+v err=%v", record, err)
	}
	if _, err := store.SaveDraft(t.Context(), draft, "stale-revision", "admin-2"); !errors.Is(err, template.ErrRecordConflict) {
		t.Fatalf("stale draft err=%v", err)
	}
	published, err := store.Publish(t.Context(), draft, record.UpdatedAt, "admin-1")
	if err != nil || published.Draft != nil || published.Published == nil || published.PublishedVersion != 1 {
		t.Fatalf("published record=%+v err=%v", published, err)
	}
	if published.Published.ContentHash == "" || published.Published.ContentHash != template.ContentHash(*published.Published) {
		t.Fatalf("published hash=%q", published.Published.ContentHash)
	}
	versions, err := store.ListVersions(t.Context(), draft.Key)
	if err != nil || len(versions) != 1 || versions[0].Version != 1 || versions[0].ContentHash == "" {
		t.Fatalf("versions=%+v err=%v", versions, err)
	}
	disabled, err := store.Disable(t.Context(), draft.Key, published.UpdatedAt, "admin-2")
	if err != nil || disabled.Status != "disabled" {
		t.Fatalf("disabled=%+v err=%v", disabled, err)
	}
	version, found, err := store.GetVersion(t.Context(), draft.Key, 1)
	if err != nil || !found || version.ContentHash != published.Published.ContentHash {
		t.Fatalf("version=%+v found=%v err=%v", version, found, err)
	}
	var privateTables int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('_notification_templates', '_notification_template_versions')`).Scan(&privateTables); err != nil || privateTables != 0 {
		t.Fatalf("private template tables=%d err=%v", privateTables, err)
	}
	var roots, publishedVersions, history int
	if err := database.QueryRow(`SELECT COUNT(*) FROM _definitions WHERE owner = ? AND kind = ?`, metadatasdk.DefinitionOwnerNotification, "notification_template").Scan(&roots); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM _definitions WHERE owner = ? AND kind = ?`, metadatasdk.DefinitionOwnerNotification, "notification_template_version").Scan(&publishedVersions); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM _definition_versions WHERE owner = ?`, metadatasdk.DefinitionOwnerNotification).Scan(&history); err != nil {
		t.Fatal(err)
	}
	if roots != 1 || publishedVersions != 1 || history != 4 {
		t.Fatalf("shared definitions roots=%d published_versions=%d history=%d", roots, publishedVersions, history)
	}
}

func TestSyncPublishedSeedsRecordAndVersionWithoutOverwritingManagementState(t *testing.T) {
	_, store := migratedStore(t)
	manifest := template.Template{Key: "record.created", Name: "Record created", Channel: "email", Status: "published", Version: 2,
		DefaultLocale: "en", Locales: map[string]template.Content{"en": {Subject: "Created", Text: "Record created"}}}
	if err := store.SyncPublished(t.Context(), []template.Template{manifest}); err != nil {
		t.Fatal(err)
	}
	seeded, found, err := store.Get(t.Context(), manifest.Key)
	if err != nil || !found || seeded.Published == nil || seeded.PublishedVersion != 2 || seeded.UpdatedBy != "manifest" {
		t.Fatalf("seeded=%+v found=%v err=%v", seeded, found, err)
	}
	draft := manifest
	draft.Status, draft.Name = "draft", "Managed name"
	managed, err := store.SaveDraft(t.Context(), draft, seeded.UpdatedAt, "admin-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SyncPublished(t.Context(), []template.Template{manifest}); err != nil {
		t.Fatal(err)
	}
	after, _, err := store.Get(t.Context(), manifest.Key)
	if err != nil || after.Draft == nil || after.Draft.Name != managed.Draft.Name || after.UpdatedBy != "admin-1" {
		t.Fatalf("after=%+v err=%v", after, err)
	}
	revision, err := store.PublishedRevision(t.Context())
	if err != nil || revision == "" {
		t.Fatalf("revision=%q err=%v", revision, err)
	}
}
