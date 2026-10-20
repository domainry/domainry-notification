package inbox_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/domainry/domainry-notification/internal/application/inbox"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type eventStore struct {
	event      inbox.Event
	retried    bool
	deadLetter bool
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
func (*eventStore) Materialize(context.Context, inbox.Event, []inbox.Item) error { return nil }
func (store *eventStore) Retry(context.Context, inbox.Event, string, string, string, string) error {
	store.retried = true
	return nil
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
				ID: "event-1", WorkspaceID: "workspace-1", AudienceResolverKeys: []string{"owners"}, AttemptCount: test.attempts,
			}}
			processor, err := inbox.NewProcessor(inbox.ProcessorDependencies{Events: store, Clock: fixedClock{now: time.Now()}, WorkerID: "worker-1", Audiences: audienceFailure{}})
			if err != nil {
				t.Fatal(err)
			}
			completed, err := processor.Process(t.Context(), store.event.WorkspaceID, store.event.ID)
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
