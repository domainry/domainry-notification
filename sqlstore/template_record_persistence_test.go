package sqlstore_test

import (
	"errors"
	"testing"

	"github.com/domainry/domainry-notification/template"
)

func TestTemplateRecordDraftPublishVersionAndDisableLifecycle(t *testing.T) {
	_, store := migratedStore(t)
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
