package validation

import (
	"fmt"
	"sort"
	"strings"

	templatemodel "github.com/domainry/domainry-notification/internal/domain/template/model"
)

// Capability describes portable content supported by one channel/provider
// pair. Concrete Connector modules contribute these declarations.
type Capability struct {
	Channel                  string `json:"channel"`
	Provider                 string `json:"provider,omitempty"`
	SupportsHTML             bool   `json:"supports_html"`
	SupportsMarkdown         bool   `json:"supports_markdown"`
	SupportsFacts            bool   `json:"supports_facts"`
	SupportsURLActions       bool   `json:"supports_url_actions"`
	SupportsProviderTemplate bool   `json:"supports_provider_template"`
	MaxFacts                 int    `json:"max_facts"`
	MaxActions               int    `json:"max_actions"`
}

// ProviderTemplateValidation is implemented by provider-owned code when an
// approved provider template has rules beyond portable notification semantics.
type ProviderTemplateValidation func(*templatemodel.ProviderTemplate) error

// Provider contributes one capability and its optional provider-specific
// template validation. Values are copied into an immutable Catalog.
type Provider struct {
	Capability               Capability
	ValidateProviderTemplate ProviderTemplateValidation
}

type capabilityEntry struct {
	capability Capability
	validate   ProviderTemplateValidation
}

// Capabilities is an immutable channel/provider catalog. Immutability prevents
// validation behavior from changing while templates or events are processed.
type Capabilities struct {
	entries map[string]capabilityEntry
}

func NewCapabilities(providers []Provider) (*Capabilities, error) {
	entries := make(map[string]capabilityEntry, len(providers))
	for _, provider := range providers {
		capability := provider.Capability
		capability.Channel = strings.TrimSpace(capability.Channel)
		capability.Provider = strings.TrimSpace(capability.Provider)
		if capability.Channel == "" || capability.MaxFacts < 0 || capability.MaxActions < 0 {
			return nil, fmt.Errorf("notification template capability is invalid")
		}
		if capability.SupportsProviderTemplate && provider.ValidateProviderTemplate == nil {
			return nil, fmt.Errorf("notification provider-template validator is required for %q", capabilityKey(capability.Channel, capability.Provider))
		}
		key := capabilityKey(capability.Channel, capability.Provider)
		if _, exists := entries[key]; exists {
			return nil, fmt.Errorf("notification template capability %q is duplicated", key)
		}
		entries[key] = capabilityEntry{capability: capability, validate: provider.ValidateProviderTemplate}
	}
	return &Capabilities{entries: entries}, nil
}

func (c *Capabilities) Resolve(channel, provider string) (Capability, bool) {
	if c == nil {
		return Capability{}, false
	}
	entry, found := c.entries[capabilityKey(channel, provider)]
	return entry.capability, found
}

func (c *Capabilities) ValidateProviderTemplate(channel, provider string, value *templatemodel.ProviderTemplate) error {
	if value == nil {
		return nil
	}
	if c == nil {
		return fmt.Errorf("notification template capability is unavailable")
	}
	entry, found := c.entries[capabilityKey(channel, provider)]
	if !found {
		return fmt.Errorf("notification template capability is unavailable")
	}
	if entry.validate == nil {
		return nil
	}
	return entry.validate(value)
}

func (c *Capabilities) List() []Capability {
	if c == nil {
		return nil
	}
	result := make([]Capability, 0, len(c.entries))
	for _, entry := range c.entries {
		result = append(result, entry.capability)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Channel == result[j].Channel {
			return result[i].Provider < result[j].Provider
		}
		return result[i].Channel < result[j].Channel
	})
	return result
}

func capabilityKey(channel, provider string) string {
	return strings.TrimSpace(channel) + "/" + strings.TrimSpace(provider)
}
