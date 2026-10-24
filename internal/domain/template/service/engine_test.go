package template_test

import (
	"context"
	"strings"
	"testing"

	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-notification/internal/domain/template/service"
)

type recipientResolver map[notification.UserID]notification.Recipient

func (d recipientResolver) FindRecipient(_ context.Context, _ notification.WorkspaceID, id notification.UserID) (notification.Recipient, bool, error) {
	value, found := d[id]
	return value, found, nil
}

type nativePushResolver struct {
	recipientResolver
	tokens map[string]string
}

func (r nativePushResolver) FindNativePushToken(_ context.Context, _ notification.WorkspaceID, userID notification.UserID, provider string) (string, bool, error) {
	token, found := r.tokens[userID.String()+"/"+provider]
	return token, found, nil
}

func TestEngineRendersPortableSnapshotAndResolvesRecipients(t *testing.T) {
	value := validEmailTemplate()
	content := value.Locales["en-US"]
	content.Facts = []template.Fact{{Key: "Task", Value: "{{task}}"}}
	content.Actions = []template.Action{{Label: "Open", URL: "https://example.test/tasks/{{task}}", Style: "primary"}}
	value.Locales["en-US"] = content
	engine, err := template.NewEngine("en-US", []template.Template{value}, emailValidator(t), recipientResolver{
		"user-1": {ID: "user-1", Email: "USER@example.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := notification.NewWorkspaceID("workspace-1")
	rendered, err := engine.Render(t.Context(), template.RenderRequest{
		WorkspaceID: workspace, TemplateKey: value.Key, Recipients: []notification.UserID{"user-1"}, Variables: map[string]any{"task": "task-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rendered.Recipients) != 1 || rendered.Recipients[0] != "user@example.test" {
		t.Fatalf("recipients=%v", rendered.Recipients)
	}
	if !strings.Contains(rendered.HTML, `data-notification-facts="true"`) || !strings.Contains(rendered.HTML, `href="https://example.test/tasks/task-1"`) {
		t.Fatalf("html=%s", rendered.HTML)
	}
}

func TestEngineResolvesFeishuCollaborationRecipientByIdentityEmail(t *testing.T) {
	value := template.Template{
		Key: "task.reminder.feishu", Name: "Feishu task reminder", Channel: "collaboration", Provider: "feishu",
		Status: "published", Version: 1, DefaultLocale: "zh-CN",
		Variables: []template.Variable{{Key: "task", Type: "text", Required: true}},
		Locales:   map[string]template.Content{"zh-CN": {Title: "任务提醒", Markdown: "请处理 {{task}}"}},
	}
	engine, err := template.NewEngine("zh-CN", []template.Template{value}, collaborationValidator(t), recipientResolver{
		"user-1": {ID: "user-1", Email: "Sales.User@Example.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := notification.NewWorkspaceID("workspace-1")
	rendered, err := engine.Render(t.Context(), template.RenderRequest{
		WorkspaceID: workspace, TemplateKey: value.Key, Recipients: []notification.UserID{"user-1"}, Variables: map[string]any{"task": "发送报价"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rendered.Recipients) != 1 || rendered.Recipients[0] != "sales.user@example.test" || rendered.Provider != "feishu" {
		t.Fatalf("rendered=%+v", rendered)
	}
}

func TestEngineResolvesNativePushTokenAtRenderTime(t *testing.T) {
	value := template.Template{
		Key: "safety.alert.fcm", Name: "Safety alert FCM", Channel: "native_push", Provider: "fcm",
		Status: "published", Version: 1, DefaultLocale: "ja-JP",
		Variables: []template.Variable{{Key: "site", Type: "text", Required: true}},
		Locales:   map[string]template.Content{"ja-JP": {Title: "安否確認", Text: "{{site}}で応答してください"}},
	}
	capabilities, err := template.NewCapabilities([]template.Provider{{Capability: template.Capability{Channel: "native_push", Provider: "fcm"}}})
	if err != nil {
		t.Fatal(err)
	}
	validator, _ := template.NewValidator(capabilities)
	resolver := nativePushResolver{recipientResolver: recipientResolver{"user-1": {ID: "user-1"}}, tokens: map[string]string{"user-1/fcm": "current-token"}}
	engine, err := template.NewEngine("ja-JP", []template.Template{value}, validator, resolver)
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := notification.NewWorkspaceID("workspace-1")
	rendered, err := engine.Render(t.Context(), template.RenderRequest{WorkspaceID: workspace, TemplateKey: value.Key, Recipients: []notification.UserID{"user-1"}, Variables: map[string]any{"site": "地下機械室"}})
	if err != nil || len(rendered.Recipients) != 1 || rendered.Recipients[0] != "current-token" || rendered.Title != "安否確認" {
		t.Fatalf("rendered=%+v err=%v", rendered, err)
	}
	delete(resolver.tokens, "user-1/fcm")
	if _, err := engine.Render(t.Context(), template.RenderRequest{WorkspaceID: workspace, TemplateKey: value.Key, Recipients: []notification.UserID{"user-1"}, Variables: map[string]any{"site": "地下機械室"}}); notification.ErrorCode(err) != "backend.notification.recipient_push_target_missing" {
		t.Fatalf("missing target code=%q err=%v", notification.ErrorCode(err), err)
	}
}

func TestEngineCatalogDoesNotLeakMutableTemplateState(t *testing.T) {
	value := validEmailTemplate()
	engine, err := template.NewEngine("en-US", []template.Template{value}, emailValidator(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	value.Locales["en-US"] = template.Content{Subject: "mutated", Text: "mutated"}
	listed := engine.Templates()
	listed[0].Locales["en-US"] = template.Content{Subject: "also mutated", Text: "also mutated"}
	again := engine.Templates()
	if again[0].Locales["en-US"].Subject != "Task {{task}}" {
		t.Fatalf("catalog leaked mutable state: %+v", again[0].Locales["en-US"])
	}
}
