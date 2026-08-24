package sqlstore_test

import (
	"errors"
	"testing"

	"github.com/domainry/domainry-notification/template"
)

func TestTemplatePublicationRequestLockLeaseAndFencingLifecycle(t *testing.T) {
	_, store := migratedStore(t)
	request := template.PublicationRequest{
		ID: "request-1", TemplateKey: "workflow.failed", Snapshot: template.Template{Key: "workflow.failed", Version: 2},
		CandidateHash: "hash-1", DraftUpdatedAt: "2026-08-24T00:59:00.000000000Z", Status: template.PublicationScheduled,
		ScheduledFor: "2026-08-24T01:00:00.000000000Z", RequestedBy: "admin-1", RequestedAt: "2026-08-24T00:58:00.000000000Z",
		UpdatedAt: "2026-08-24T00:58:00.000000000Z",
	}
	if err := store.CreatePublicationRequest(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	conflict := request
	conflict.ID = "request-2"
	if err := store.CreatePublicationRequest(t.Context(), conflict); !errors.Is(err, template.ErrPublicationConflict) {
		t.Fatalf("second open request err=%v", err)
	}
	due, err := store.ListDuePublicationRequests(t.Context(), "2026-08-24T01:00:01.000000000Z", "2026-08-24T01:00:01.000000000Z", 10)
	if err != nil || len(due) != 1 || due[0].ID != request.ID {
		t.Fatalf("due=%+v err=%v", due, err)
	}
	claimed, found, err := store.ClaimPublicationRequest(t.Context(), request.ID, "worker-1", "2026-08-24T01:00:01.000000000Z", "2026-08-24T01:00:31.000000000Z")
	if err != nil || !found || claimed.Status != template.PublicationPublishing || claimed.FencingToken != 1 {
		t.Fatalf("claimed=%+v found=%v err=%v", claimed, found, err)
	}
	stale := template.PublicationTransition{Status: template.PublicationPublished, PublishedVersion: 2, ExpectedLeaseOwner: "worker-1", ExpectedFencingToken: 2, UpdatedAt: "2026-08-24T01:00:02.000000000Z"}
	if _, err := store.TransitionPublicationRequest(t.Context(), request.ID, template.PublicationPublishing, stale); !errors.Is(err, template.ErrPublicationConflict) {
		t.Fatalf("stale transition err=%v", err)
	}
	transition := stale
	transition.ExpectedFencingToken = claimed.FencingToken
	published, err := store.TransitionPublicationRequest(t.Context(), request.ID, template.PublicationPublishing, transition)
	if err != nil || published.Status != template.PublicationPublished || published.LeaseOwner != "" {
		t.Fatalf("published=%+v err=%v", published, err)
	}
	open, err := store.HasOpenPublicationRequest(t.Context(), request.TemplateKey)
	if err != nil || open {
		t.Fatalf("open=%v err=%v", open, err)
	}
	if err := store.CreatePublicationRequest(t.Context(), conflict); err != nil {
		t.Fatalf("new request after terminal transition: %v", err)
	}
}

func TestPublishingTransitionRequiresLeaseEvidence(t *testing.T) {
	_, store := migratedStore(t)
	_, err := store.TransitionPublicationRequest(t.Context(), "request-1", template.PublicationPublishing, template.PublicationTransition{Status: template.PublicationFailed})
	if err == nil {
		t.Fatal("expected missing lease evidence error")
	}
}
