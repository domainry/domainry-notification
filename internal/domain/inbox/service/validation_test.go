package inbox_test

import (
	"testing"
	"time"

	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

func inboxValidator(t *testing.T) *inbox.Validator {
	t.Helper()
	configuration, err := inbox.NewConfiguration([]notification.Surface{"business_workspace", "consumer_portal"}, []string{"email", "collaboration"})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := inbox.NewValidator(configuration)
	if err != nil {
		t.Fatal(err)
	}
	return validator
}

func validEvent() inbox.Event {
	return inbox.Event{
		ID: "event-1", WorkspaceID: "workspace-1", Source: "workflow", SourceEventID: "task-1:opened",
		EventType: "workflow.task.opened", Category: "approval", Severity: "info", Surface: "business_workspace",
		RecipientUserIDs: []notification.UserID{" user-1 ", "user-1"}, ActionState: inbox.ActionOpen,
		OccurredAt: time.Date(2026, 8, 24, 1, 2, 3, 0, time.FixedZone("offset", 8*60*60)).Format(time.RFC3339Nano),
		Snapshot:   inbox.Snapshot{Title: "Approval required", Body: "Review task", Actions: []inbox.ActionRef{{Key: "workflow.task.open", Kind: "route", Label: "Open", ResourceType: "workflow_task", ResourceID: "task-1"}}},
	}
}

func TestValidateEventNormalizesDurableState(t *testing.T) {
	value, err := inboxValidator(t).ValidateEvent(validEvent())
	if err != nil {
		t.Fatal(err)
	}
	if value.Status != inbox.EventQueued || value.ActionState != inbox.ActionOpen || len(value.RecipientUserIDs) != 1 || value.RecipientUserIDs[0] != "user-1" {
		t.Fatalf("event=%+v", value)
	}
	if value.OccurredAt != "2026-08-23T17:02:03.000000000Z" {
		t.Fatalf("occurred_at=%q", value.OccurredAt)
	}
}

func TestValidateEventUsesInjectedSurfaceCatalog(t *testing.T) {
	value := validEvent()
	value.Surface = "unknown_surface"
	if code := notification.ErrorCode(mustEventError(inboxValidator(t), value)); code != "backend.notification.inbox_event_surface_invalid" {
		t.Fatalf("code=%q", code)
	}
}

func TestValidateQueryPreservesAudienceBoundaries(t *testing.T) {
	validator := inboxValidator(t)
	query, err := validator.ValidateQuery(inbox.Query{WorkspaceID: "workspace-1", ViewerUserID: "manager", Surface: "business_workspace", Scope: inbox.ScopeTeam, ReportingUserIDs: []notification.UserID{"member"}})
	if err != nil || len(query.RecipientUserIDs) != 1 || query.RecipientUserIDs[0] != "member" || query.Limit != 50 {
		t.Fatalf("query=%+v err=%v", query, err)
	}
	_, err = validator.ValidateQuery(inbox.Query{WorkspaceID: "workspace-1", ViewerUserID: "manager", RecipientUserID: "outsider", Surface: "business_workspace", Scope: inbox.ScopeTeam, ReportingUserIDs: []notification.UserID{"member"}})
	if notification.ErrorCode(err) != "backend.notification.inbox_team_scope_denied" {
		t.Fatalf("error=%v", err)
	}
}

func mustEventError(validator *inbox.Validator, value inbox.Event) error {
	_, err := validator.ValidateEvent(value)
	return err
}
