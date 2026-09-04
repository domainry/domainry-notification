package inbox_test

import (
	"context"
	"testing"
	"time"

	appinbox "github.com/domainry/domainry-notification/internal/application/inbox"
	"github.com/domainry/domainry-notification/internal/domain/delivery/service"
	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

type eventStore struct {
	event        inbox.Event
	created      bool
	materialized inbox.Event
	items        []inbox.Item
	retried      bool
	failed       bool
}

func (s *eventStore) Enqueue(_ context.Context, event inbox.Event) (inbox.Event, bool, error) {
	s.event = event
	return event, s.created, nil
}
func (s *eventStore) ListDue(context.Context, string, int) ([]inbox.Event, error) {
	return []inbox.Event{s.event}, nil
}
func (s *eventStore) Claim(_ context.Context, _ notification.WorkspaceID, _, owner, _, expires string) (inbox.Event, bool, error) {
	value := s.event
	value.Status, value.LeaseOwner, value.LeaseExpiresAt = inbox.EventProcessing, owner, expires
	value.FencingToken++
	return value, true, nil
}
func (s *eventStore) Materialize(_ context.Context, event inbox.Event, items []inbox.Item) error {
	s.materialized, s.items = event, items
	return nil
}
func (s *eventStore) Retry(context.Context, inbox.Event, string, string, string, string) error {
	s.retried = true
	return nil
}
func (s *eventStore) Fail(context.Context, inbox.Event, string, string, string) error {
	s.failed = true
	return nil
}

type audienceResolver struct{}

func (audienceResolver) ResolveAudience(context.Context, string, inbox.Event) ([]notification.UserID, error) {
	return []notification.UserID{"resolved-user", "explicit-user"}, nil
}

type localeResolver struct{}

func (localeResolver) RecipientLocale(_ context.Context, _ notification.WorkspaceID, user notification.UserID) (string, error) {
	if user == "resolved-user" {
		return "zh-CN", nil
	}
	return "en-US", nil
}

func TestProcessorResolvesAudienceAndMaterializesLocalizedItems(t *testing.T) {
	wakeups := &notifier{}
	store := &eventStore{event: inbox.Event{
		ID: "event-1", WorkspaceID: "workspace-1", RecipientUserIDs: []notification.UserID{"explicit-user"}, AudienceResolverKeys: []string{"workflow_task_assignee"},
		Snapshot: inbox.Snapshot{Title: "English", Body: "Body"}, LocalizedSnapshots: map[string]inbox.Snapshot{"zh-CN": {Title: "中文", Body: "正文"}},
		ChannelPlans: []delivery.Plan{{ID: "plan-1", WorkspaceID: "workspace-1"}},
		OccurredAt:   "2026-08-24T00:00:00.000000000Z", CreatedAt: "2026-08-24T00:00:00.000000000Z", UpdatedAt: "2026-08-24T00:00:00.000000000Z",
	}}
	processor, err := appinbox.NewProcessor(appinbox.ProcessorDependencies{
		Events: store, Clock: fixedClock{value: time.Date(2026, 8, 24, 1, 0, 0, 0, time.UTC)}, WorkerID: "worker-1",
		Audiences: audienceResolver{}, RecipientLocale: localeResolver{}, WorkNotifier: wakeups,
	})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := processor.Process(t.Context(), "workspace-1", "event-1")
	if err != nil || !completed {
		t.Fatalf("completed=%v err=%v", completed, err)
	}
	if len(store.materialized.RecipientUserIDs) != 2 || len(store.items) != 2 {
		t.Fatalf("event=%+v items=%+v", store.materialized, store.items)
	}
	for _, item := range store.items {
		if item.RecipientUserID == "resolved-user" && item.Title != "中文" {
			t.Fatalf("localized item=%+v", item)
		}
	}
	if len(wakeups.work) != 1 || wakeups.work[0].Kind != notification.WorkChannelPlan || wakeups.work[0].TaskID != "plan-1" {
		t.Fatalf("wakeups=%+v", wakeups.work)
	}
}

type notifier struct{ work []notification.Work }

func (n *notifier) Notify(_ context.Context, work notification.Work) { n.work = append(n.work, work) }

func TestPublisherWakesOnlyAfterCreatedEvent(t *testing.T) {
	validator := inboxValidator(t)
	catalog, err := inbox.NewCatalog(validator, []inbox.EventType{validEventType()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	compiler, _ := inbox.NewCompiler(catalog, validator, fixedClock{value: time.Now().UTC()})
	store, wakeups := &eventStore{created: true}, &notifier{}
	publisher, err := inbox.NewPublisher(compiler, store, wakeups)
	if err != nil {
		t.Fatal(err)
	}
	_, created, err := publisher.PublishIntent(t.Context(), inbox.Intent{
		ID: "event-1", WorkspaceID: "workspace-1", SourceEventID: "source-1", EventType: "workflow.task.opened",
		RecipientUserIDs: []notification.UserID{"user-1"}, OccurredAt: time.Now().UTC().Format(time.RFC3339Nano), Variables: map[string]any{"task_title": "Task"},
	})
	if err != nil || !created || len(wakeups.work) != 1 || wakeups.work[0].TaskID != "event-1" {
		t.Fatalf("created=%v wakeups=%+v err=%v", created, wakeups.work, err)
	}
}
