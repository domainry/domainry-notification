package inboxstore_test

import (
	"testing"

	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
)

func TestInboxSavedViewLifecycleIsScopedToOwnerAndSurface(t *testing.T) {
	_, store := migratedStore(t)
	view := inbox.SavedView{Key: "urgent", Name: "Urgent", Mailbox: inbox.MailboxUnread, Severities: []string{"error"},
		CreatedAt: "2026-08-24T01:00:00.000000000Z", UpdatedAt: "2026-08-24T01:00:00.000000000Z"}
	if _, err := store.SaveSavedView(t.Context(), "workspace-1", "user-1", "business_workspace", view); err != nil {
		t.Fatal(err)
	}
	listed, err := store.ListSavedViews(t.Context(), "workspace-1", "user-1", "business_workspace")
	if err != nil || len(listed) != 1 || listed[0].Key != view.Key {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
	otherSurface, err := store.ListSavedViews(t.Context(), "workspace-1", "user-1", "control_plane")
	if err != nil || len(otherSurface) != 0 {
		t.Fatalf("other surface=%+v err=%v", otherSurface, err)
	}
	deleted, err := store.DeleteSavedView(t.Context(), "workspace-1", "user-1", "business_workspace", view.Key)
	if err != nil || !deleted {
		t.Fatalf("deleted=%v err=%v", deleted, err)
	}
}
