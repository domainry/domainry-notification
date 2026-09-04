package inbox_test

import (
	"testing"

	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

func TestCatalogIsImmutableAndDetectsActionContractConflicts(t *testing.T) {
	first := validEventType()
	first.Actions = []inbox.ActionDescriptor{{Key: "workflow.task.open", Kind: "route", ResourceType: "workflow_task", RouteKey: "workflow.task.detail"}}
	content := first.Locales["en-US"]
	content.ActionLabels = map[string]string{"workflow.task.open": "Open"}
	first.Locales["en-US"] = content
	catalog, err := inbox.NewCatalog(inboxValidator(t), []inbox.EventType{first}, nil)
	if err != nil {
		t.Fatal(err)
	}
	first.Actions[0].RouteKey = "mutated"
	descriptor, found := catalog.Action("workflow.task.open")
	if !found || descriptor.RouteKey != "workflow.task.detail" {
		t.Fatalf("descriptor=%+v found=%v", descriptor, found)
	}
	descriptor.RouteKey = "also.mutated"
	descriptor, _ = catalog.Action("workflow.task.open")
	if descriptor.RouteKey != "workflow.task.detail" {
		t.Fatal("catalog leaked mutable action state")
	}

	second := first
	second.Key, second.TemplateKey = "automation.execution.failed", "automation.execution.failed.in_app"
	second.Source, second.Category = "automation", "automation"
	second.Actions = []inbox.ActionDescriptor{{Key: "workflow.task.open", Kind: "route", ResourceType: "automation_rule", RouteKey: "automation.rule.detail"}}
	if _, err := inbox.NewCatalog(inboxValidator(t), []inbox.EventType{first, second}, nil); notification.ErrorCode(err) != "backend.notification.inbox_action_contract_conflict" {
		t.Fatalf("error=%v", err)
	}
}
