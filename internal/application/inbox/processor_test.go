package inbox_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/domainry/domainry-notification/internal/application/inbox"
	"github.com/domainry/domainry-notification/internal/domain/delivery/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type eventStore struct {
	event        inbox.Event
	materialized inbox.Event
	retried      bool
	deadLetter   bool
}

func (store *eventStore) Enqueue(context.Context, inbox.Event) (inbox.Event, bool, error) {
	return inbox.Event{}, false, nil
}
func (store *eventStore) ListDue(context.Context, string, int) ([]inbox.Event, error) {
	return []inbox.Event{store.event}, nil
}
func (store *eventStore) Claim(context.Context, notification.WorkspaceID, string, string, string, string) (inbox.Event, bool, error) {
	return store.event, true, nil
}
func (store *eventStore) Materialize(_ context.Context, event inbox.Event, _ []inbox.Item) error {
	store.materialized = event
	return nil
}
func (store *eventStore) Retry(context.Context, inbox.Event, string, string, string, string) error {
	store.retried = true
	return nil
}

func TestProcessorSplitsNativePushPlansPerRecipient(t *testing.T) {
	store := &eventStore{event: inbox.Event{
		EventID: "event-1", WorkspaceID: "workspace-1", RecipientUserIDs: []notification.UserID{"user-1", "user-2"},
		ChannelPlans: []delivery.Plan{
			{DeliveryID: "native-1", WorkspaceID: "workspace-1", EventID: "event-1", Channel: "native_push", TemplateKey: "event.fcm", Status: "queued"},
			{DeliveryID: "email-1", WorkspaceID: "workspace-1", EventID: "event-1", Channel: "email", TemplateKey: "event.email", Status: "queued"},
		},
	}}
	processor, err := inbox.NewProcessor(inbox.ProcessorDependencies{Events: store, Clock: fixedClock{now: time.Now()}, WorkerID: "worker-1"})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := processor.Process(t.Context(), store.event.WorkspaceID, store.event.EventID)
	if err != nil || !completed || len(store.materialized.ChannelPlans) != 3 {
		t.Fatalf("completed=%v plans=%+v err=%v", completed, store.materialized.ChannelPlans, err)
	}
	first, second, email := store.materialized.ChannelPlans[0], store.materialized.ChannelPlans[1], store.materialized.ChannelPlans[2]
	if len(first.RecipientUserIDs) != 1 || first.RecipientUserIDs[0] != "user-1" || len(second.RecipientUserIDs) != 1 || second.RecipientUserIDs[0] != "user-2" || first.DeliveryID == second.DeliveryID || len(email.RecipientUserIDs) != 2 {
		t.Fatalf("split plans=%+v", store.materialized.ChannelPlans)
	}
}
func (store *eventStore) Fail(context.Context, inbox.Event, string, string, string) error {
	store.deadLetter = true
	return nil
}

type audienceFailure struct{}

func (audienceFailure) ResolveAudience(context.Context, string, inbox.Event) ([]notification.UserID, error) {
	return nil, errors.New("resolver unavailable")
}

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

func TestProcessorLogsRetryAndDeadLetterAfterPersistence(t *testing.T) {
	for _, test := range []struct {
		name       string
		attempts   int
		wantEvent  string
		wantRetry  bool
		wantFailed bool
	}{
		{name: "retry", attempts: 0, wantEvent: "notification_inbox_retry_scheduled", wantRetry: true},
		{name: "dead_letter", attempts: 8, wantEvent: "notification_inbox_dead_lettered", wantFailed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			core, observed := observer.New(zap.WarnLevel)
			previous := zap.L()
			zap.ReplaceGlobals(zap.New(core))
			t.Cleanup(func() { zap.ReplaceGlobals(previous) })

			store := &eventStore{event: inbox.Event{
				EventID: "event-1", WorkspaceID: "workspace-1", AudienceResolverKeys: []string{"owners"}, AttemptCount: test.attempts,
			}}
			processor, err := inbox.NewProcessor(inbox.ProcessorDependencies{Events: store, Clock: fixedClock{now: time.Now()}, WorkerID: "worker-1", Audiences: audienceFailure{}})
			if err != nil {
				t.Fatal(err)
			}
			completed, err := processor.Process(t.Context(), store.event.WorkspaceID, store.event.EventID)
			if err != nil || completed || store.retried != test.wantRetry || store.deadLetter != test.wantFailed {
				t.Fatalf("completed=%v retried=%v dead=%v err=%v", completed, store.retried, store.deadLetter, err)
			}
			entries := observed.FilterMessage(test.wantEvent).All()
			if len(entries) != 1 || entries[0].ContextMap()["task_id"] != "event-1" {
				t.Fatalf("transition log=%v", entries)
			}
		})
	}
}
