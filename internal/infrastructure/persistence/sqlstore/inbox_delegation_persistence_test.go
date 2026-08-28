package sqlstore_test

import (
	"testing"

	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
)

func TestInboxDelegationLifecycleAndActiveWindow(t *testing.T) {
	_, store := migratedStore(t)
	delegation := inbox.Delegation{ID: "delegation-1", WorkspaceID: "workspace-1", OwnerUserID: "owner-1", DelegateUserID: "delegate-1",
		Surface: "business_workspace", StartsAt: "2026-08-24T01:00:00.000000000Z", EndsAt: "2026-08-24T02:00:00.000000000Z", Enabled: true,
		CreatedAt: "2026-08-24T00:59:00.000000000Z", UpdatedAt: "2026-08-24T00:59:00.000000000Z"}
	if _, err := store.SaveDelegation(t.Context(), delegation); err != nil {
		t.Fatal(err)
	}
	listed, err := store.ListDelegations(t.Context(), delegation.WorkspaceID, delegation.OwnerUserID, delegation.Surface)
	if err != nil || len(listed) != 1 || listed[0].DelegateUserID != delegation.DelegateUserID {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
	active, err := store.ListActiveDelegatedOwnerIDs(t.Context(), delegation.WorkspaceID, delegation.DelegateUserID, delegation.Surface, "2026-08-24T01:30:00.000000000Z")
	if err != nil || len(active) != 1 || active[0] != delegation.OwnerUserID {
		t.Fatalf("active=%+v err=%v", active, err)
	}
	expired, err := store.ListActiveDelegatedOwnerIDs(t.Context(), delegation.WorkspaceID, delegation.DelegateUserID, delegation.Surface, delegation.EndsAt)
	if err != nil || len(expired) != 0 {
		t.Fatalf("expired=%+v err=%v", expired, err)
	}
	deleted, err := store.DeleteDelegation(t.Context(), delegation.WorkspaceID, delegation.OwnerUserID, delegation.ID)
	if err != nil || !deleted {
		t.Fatalf("deleted=%v err=%v", deleted, err)
	}
}
