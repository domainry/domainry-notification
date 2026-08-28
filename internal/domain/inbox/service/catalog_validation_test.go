package inbox_test

import (
	"testing"

	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-notification/internal/domain/template/service"
)

func validEventType() inbox.EventType {
	return inbox.EventType{
		Key: "workflow.task.opened", Source: "workflow", Category: "approval", DefaultSeverity: "info",
		Surfaces: []notification.Surface{"business_workspace"}, MandatoryInApp: true,
		TemplateKey: "workflow.task.opened.in_app", DefaultLocale: "en-US", Version: 1, Status: "published",
		Variables: []template.Variable{{Key: "task_title", Type: "text", Required: true}},
		Locales:   map[string]inbox.Content{"en-US": {Title: "Approval", Body: "Review {{task_title}}"}},
	}
}

func TestCatalogValidationUsesComposedExternalChannels(t *testing.T) {
	validator := inboxValidator(t)
	eventType := validEventType()
	rule := inbox.Rule{EventTypeKey: eventType.Key, Enabled: true, MandatoryInApp: true, Channels: []inbox.RuleChannel{{
		Channel: "collaboration", TemplateKey: "workflow.task.opened.chat", ConnectorKey: "example", Operation: "send",
	}}}
	if err := validator.ValidateCatalog([]inbox.EventType{eventType}, []inbox.Rule{rule}); err != nil {
		t.Fatal(err)
	}
	rule.Channels[0].Channel = "future_channel"
	if code := notification.ErrorCode(validator.ValidateCatalog([]inbox.EventType{eventType}, []inbox.Rule{rule})); code != "backend.notification.rule_channel_unsupported" {
		t.Fatalf("code=%q", code)
	}
}

func TestEventTypeRejectsSensitiveVariables(t *testing.T) {
	value := validEventType()
	value.Variables = append(value.Variables, template.Variable{Key: "access_token", Type: "text"})
	if _, err := inboxValidator(t).ValidateEventType(value); notification.ErrorCode(err) != "backend.notification.event_type_variable_unsafe" {
		t.Fatalf("error=%v", err)
	}
}
