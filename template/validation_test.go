package template_test

import (
	"strings"
	"testing"

	"github.com/domainry/domainry-notification"
	"github.com/domainry/domainry-notification/template"
)

func emailValidator(t *testing.T) *template.Validator {
	t.Helper()
	capabilities, err := template.NewCapabilities([]template.Provider{{Capability: template.Capability{
		Channel: "email", SupportsHTML: true, SupportsFacts: true, SupportsURLActions: true, MaxFacts: 10, MaxActions: 5,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := template.NewValidator(capabilities)
	if err != nil {
		t.Fatal(err)
	}
	return validator
}

func validEmailTemplate() template.Template {
	return template.Template{
		Key: "workflow.task.opened", Name: "Workflow task opened", Channel: "email", Status: "published", Version: 1,
		DefaultLocale: "en-US", Variables: []template.Variable{{Key: "task", Type: "text", Required: true}},
		Locales: map[string]template.Content{"en-US": {Subject: "Task {{task}}", HTML: "<p>{{task}}</p>"}},
	}
}

func TestValidatorUsesInjectedCapabilities(t *testing.T) {
	validator := emailValidator(t)
	value := validEmailTemplate()
	if err := validator.Validate(value); err != nil {
		t.Fatal(err)
	}
	value.Provider = "unknown"
	if code := notification.ErrorCode(validator.Validate(value)); code != "backend.notification.template_provider_unsupported" {
		t.Fatalf("code=%q", code)
	}
}

func TestValidatorRejectsUnknownTokensAndUnsupportedContent(t *testing.T) {
	validator := emailValidator(t)
	value := validEmailTemplate()
	content := value.Locales["en-US"]
	content.Subject = "Task {{unknown}}"
	value.Locales["en-US"] = content
	if code := notification.ErrorCode(validator.Validate(value)); code != "backend.notification.template_variable_unknown" {
		t.Fatalf("code=%q", code)
	}

	capabilities, err := template.NewCapabilities([]template.Provider{{Capability: template.Capability{Channel: "collaboration", Provider: "example", SupportsFacts: true, MaxFacts: 10, MaxActions: 5}}})
	if err != nil {
		t.Fatal(err)
	}
	validator, _ = template.NewValidator(capabilities)
	value = validEmailTemplate()
	value.Channel, value.Provider = "collaboration", "example"
	content = value.Locales["en-US"]
	content.Text = "Task {{task}}"
	value.Locales["en-US"] = content
	if code := notification.ErrorCode(validator.Validate(value)); code != "backend.notification.template_channel_mismatch" {
		t.Fatalf("code=%q", code)
	}
}

func TestRestrictedRenderingValidatesValuesAndEscapesHTML(t *testing.T) {
	value := validEmailTemplate()
	variables, err := template.ValidateRenderVariables(value, map[string]any{"task": `<Admin>`})
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := template.RenderRestricted(`<p>{{task}}</p>`, variables, true)
	if err != nil || rendered != `<p>&lt;Admin&gt;</p>` {
		t.Fatalf("rendered=%q err=%v", rendered, err)
	}
	if _, err := template.RenderRestricted("{{missing}}", variables, false); err == nil || !strings.Contains(err.Error(), "render failed") {
		t.Fatalf("missing variable error=%v", err)
	}
}

func TestProviderTemplateCapabilityRequiresProviderValidator(t *testing.T) {
	_, err := template.NewCapabilities([]template.Provider{{Capability: template.Capability{Channel: "whatsapp", Provider: "example", SupportsProviderTemplate: true}}})
	if err == nil {
		t.Fatal("provider-template capability without provider validation was accepted")
	}
}
