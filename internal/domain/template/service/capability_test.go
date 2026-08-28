package template_test

import (
	"errors"
	"testing"

	"github.com/domainry/domainry-notification/internal/domain/template/service"
)

func TestCapabilitiesAreImmutableAndProviderOwned(t *testing.T) {
	wantValidation := errors.New("provider template rejected")
	providers := []template.Provider{{
		Capability:               template.Capability{Channel: "collaboration", Provider: "example", SupportsMarkdown: true, MaxActions: 3},
		ValidateProviderTemplate: func(*template.ProviderTemplate) error { return wantValidation },
	}}
	catalog, err := template.NewCapabilities(providers)
	if err != nil {
		t.Fatal(err)
	}
	providers[0].Capability.Provider = "changed"
	capability, found := catalog.Resolve("collaboration", "example")
	if !found || capability.Provider != "example" || capability.MaxActions != 3 {
		t.Fatalf("capability=%+v found=%v", capability, found)
	}
	if err := catalog.ValidateProviderTemplate("collaboration", "example", &template.ProviderTemplate{}); !errors.Is(err, wantValidation) {
		t.Fatalf("provider validation error=%v", err)
	}
	listed := catalog.List()
	listed[0].Provider = "mutated"
	if capability, _ := catalog.Resolve("collaboration", "example"); capability.Provider != "example" {
		t.Fatal("catalog was mutated through List")
	}
}

func TestCapabilitiesRejectInvalidAndDuplicateEntries(t *testing.T) {
	if _, err := template.NewCapabilities([]template.Provider{{}}); err == nil {
		t.Fatal("empty capability was accepted")
	}
	provider := template.Provider{Capability: template.Capability{Channel: "email"}}
	if _, err := template.NewCapabilities([]template.Provider{provider, provider}); err == nil {
		t.Fatal("duplicate capability was accepted")
	}
}
