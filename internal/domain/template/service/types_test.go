package template_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/domainry/domainry-notification/internal/domain/template/service"
)

func TestTemplateWireNamesRemainCompatibleWithPlane(t *testing.T) {
	value := template.Template{
		Key: "welcome", Name: "Welcome", Channel: "email", Status: "active",
		Version: 2, DefaultLocale: "en-US",
		Locales: map[string]template.Content{"en-US": {Subject: "Hello", HTML: "<p>Hello</p>"}},
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"default_locale"`, `"content_hash"`, `"provider_template"`} {
		if field == `"content_hash"` || field == `"provider_template"` {
			if strings.Contains(string(raw), field) {
				t.Fatalf("omitempty field %s unexpectedly present in %s", field, raw)
			}
			continue
		}
		if !strings.Contains(string(raw), field) {
			t.Fatalf("required field %s missing from %s", field, raw)
		}
	}
}
