package inbox_test

import (
	"testing"
	"time"

	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

type fixedClock struct{ value time.Time }

func (c fixedClock) Now() time.Time { return c.value }

func TestCompilerProducesRetryStableDurableEvent(t *testing.T) {
	eventType := validEventType()
	eventType.Actions = []inbox.ActionDescriptor{{Key: "workflow.task.open", Kind: "route", ResourceType: "workflow_task", RouteKey: "workflow.task.detail"}}
	content := eventType.Locales["en-US"]
	content.ActionLabels = map[string]string{"workflow.task.open": "Open {{task_title}}"}
	eventType.Locales["en-US"] = content
	rules := []inbox.Rule{{EventTypeKey: eventType.Key, Enabled: true, AudienceResolvers: []string{"workflow_task_assignee"}, Channels: []inbox.RuleChannel{{
		Channel: "collaboration", TemplateKey: "workflow.task.opened.chat", ConnectorKey: "example", Operation: "send", DelaySeconds: 60,
	}}}}
	validator := inboxValidator(t)
	catalog, err := inbox.NewCatalog(validator, []inbox.EventType{eventType}, rules)
	if err != nil {
		t.Fatal(err)
	}
	compiler, err := inbox.NewCompiler(catalog, validator, fixedClock{value: time.Date(2026, 8, 24, 1, 2, 3, 4, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	intent := inbox.Intent{
		ID: "event-1", WorkspaceID: "workspace-1", SourceEventID: "task-1:opened", EventType: eventType.Key, RecipientUserIDs: []notification.UserID{"user-1"}, AudienceResolverKeys: []string{"workflow_task_assignee"},
		SubjectType: "workflow_task", SubjectID: "task-1", OccurredAt: "2026-08-24T00:00:00Z", Locale: "en_US",
		Variables: map[string]any{"task_title": "Approve order"},
	}
	first, err := compiler.Compile(intent)
	if err != nil {
		t.Fatal(err)
	}
	second, err := compiler.Compile(intent)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != inbox.EventQueued || first.Snapshot.Title != "Approval" || first.Snapshot.Actions[0].ResourceID != "task-1" {
		t.Fatalf("event=%+v", first)
	}
	if len(first.ChannelPlans) != 1 || first.ChannelPlans[0].ID == "" || first.ChannelPlans[0].ID != second.ChannelPlans[0].ID {
		t.Fatalf("first plans=%+v second plans=%+v", first.ChannelPlans, second.ChannelPlans)
	}
	if first.ChannelPlans[0].NextAttemptAt != "2026-08-24T00:01:00.000000000Z" {
		t.Fatalf("next attempt=%q", first.ChannelPlans[0].NextAttemptAt)
	}
	intent.Variables["task_title"] = "mutated"
	if first.ChannelPlans[0].Variables["task_title"] != "Approve order" {
		t.Fatal("compiled plan leaked producer variable state")
	}
}

func TestCompilerRejectsUnregisteredAudienceResolver(t *testing.T) {
	validator := inboxValidator(t)
	eventType := validEventType()
	catalog, err := inbox.NewCatalog(validator, []inbox.EventType{eventType}, nil)
	if err != nil {
		t.Fatal(err)
	}
	compiler, _ := inbox.NewCompiler(catalog, validator, fixedClock{value: time.Now()})
	_, err = compiler.Compile(inbox.Intent{
		ID: "event-1", WorkspaceID: "workspace-1", SourceEventID: "source-1", EventType: eventType.Key, AudienceResolverKeys: []string{"unknown_resolver"}, OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
		Variables: map[string]any{"task_title": "Task"},
	})
	if notification.ErrorCode(err) != "backend.notification.inbox_audience_resolver_not_allowed" {
		t.Fatalf("error=%v", err)
	}
}
