package template

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	stableKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`)
	tokenPattern     = regexp.MustCompile(`\{\{\s*([a-z][a-z0-9_.-]*)\s*\}\}`)
)

var supportedVariableTypes = map[string]bool{
	"boolean": true, "date": true, "datetime": true, "email": true,
	"number": true, "text": true, "url": true,
}

const fallbackLimit = 5

// Validator validates portable template semantics against a host-composed,
// immutable provider capability catalog.
type Validator struct {
	capabilities *Capabilities
}

func NewValidator(capabilities *Capabilities) (*Validator, error) {
	if capabilities == nil {
		return nil, fmt.Errorf("notification template capabilities are required")
	}
	return &Validator{capabilities: capabilities}, nil
}

func (v *Validator) ValidateAll(templates []Template) error {
	seen := map[string]bool{}
	for _, value := range templates {
		if seen[value.Key] {
			return invalid("backend.notification.template_key_duplicate", "template_key", value.Key)
		}
		seen[value.Key] = true
	}
	for index := range templates {
		if err := v.Validate(templates[index]); err != nil {
			return fmt.Errorf("notification_templates[%d]: %w", index, err)
		}
		for _, fallback := range templates[index].Fallbacks {
			if !seen[fallback.TemplateKey] {
				return invalid("backend.notification.fallback_template_not_found", "template_key", templates[index].Key, "fallback_template_key", fallback.TemplateKey)
			}
		}
	}
	return nil
}

func (v *Validator) Validate(value Template) error {
	if v == nil || v.capabilities == nil {
		return notificationCapabilitiesUnavailable()
	}
	if !stableKeyPattern.MatchString(strings.TrimSpace(value.Key)) {
		return invalid("backend.notification.template_key_invalid", "template_key", value.Key)
	}
	if strings.TrimSpace(value.Name) == "" {
		return invalid("backend.notification.template_name_required", "template_key", value.Key)
	}
	channel, provider := strings.TrimSpace(value.Channel), strings.TrimSpace(value.Provider)
	capability, known := v.capabilities.Resolve(channel, provider)
	if !known {
		return invalid("backend.notification.template_provider_unsupported", "channel", channel, "provider", provider)
	}
	if strings.TrimSpace(value.Status) != "published" {
		return invalid("backend.notification.template_status_invalid", "status", value.Status)
	}
	if value.Version < 1 {
		return invalid("backend.notification.template_version_invalid", "template_key", value.Key)
	}
	defaultLocale := strings.TrimSpace(value.DefaultLocale)
	if defaultLocale == "" {
		return invalid("backend.notification.template_default_locale_required", "template_key", value.Key)
	}
	if len(value.Locales) == 0 {
		return invalid("backend.notification.template_locales_required", "template_key", value.Key)
	}
	if _, found := value.Locales[defaultLocale]; !found {
		return invalid("backend.notification.template_default_locale_missing", "template_key", value.Key, "locale", defaultLocale)
	}
	variables, err := validateVariables(value)
	if err != nil {
		return err
	}
	if err := validateFallbacks(value); err != nil {
		return err
	}
	locales := make([]string, 0, len(value.Locales))
	for locale := range value.Locales {
		locales = append(locales, locale)
	}
	sort.Strings(locales)
	for _, locale := range locales {
		if err := v.validateContent(value, locale, value.Locales[locale], variables, capability); err != nil {
			return err
		}
	}
	return nil
}

func (v *Validator) ValidateEditable(value Template) error {
	status := strings.TrimSpace(value.Status)
	if status != "draft" && status != "published" {
		return invalid("backend.notification.template_status_invalid", "status", value.Status)
	}
	value.Status, value.ContentHash = "published", ""
	return v.Validate(value)
}

func validateVariables(value Template) (map[string]Variable, error) {
	variables := make(map[string]Variable, len(value.Variables))
	for _, variable := range value.Variables {
		key := strings.TrimSpace(variable.Key)
		if !stableKeyPattern.MatchString(key) {
			return nil, invalid("backend.notification.template_variable_key_invalid", "template_key", value.Key, "variable", key)
		}
		if _, exists := variables[key]; exists {
			return nil, invalid("backend.notification.template_variable_duplicate", "template_key", value.Key, "variable", key)
		}
		variable.Key, variable.Type = key, strings.TrimSpace(variable.Type)
		if !supportedVariableTypes[variable.Type] {
			return nil, invalid("backend.notification.template_variable_type_unsupported", "template_key", value.Key, "variable", key, "type", variable.Type)
		}
		variables[key] = variable
	}
	return variables, nil
}

func validateFallbacks(value Template) error {
	if len(value.Fallbacks) > fallbackLimit {
		return invalid("backend.notification.fallback_limit_exceeded", "template_key", value.Key)
	}
	seen := map[string]bool{}
	for _, fallback := range value.Fallbacks {
		target, connector := strings.TrimSpace(fallback.TemplateKey), strings.TrimSpace(fallback.ConnectorKey)
		connection, operation := strings.TrimSpace(fallback.ConnectionKey), strings.TrimSpace(fallback.Operation)
		if target == value.Key || !stableKeyPattern.MatchString(target) || !stableKeyPattern.MatchString(connector) || connection == "" || operation == "" {
			return invalid("backend.notification.fallback_invalid", "template_key", value.Key)
		}
		identity := connector + "\x00" + connection + "\x00" + operation + "\x00" + target
		if seen[identity] {
			return invalid("backend.notification.fallback_duplicate", "template_key", value.Key)
		}
		seen[identity] = true
	}
	return nil
}

func (v *Validator) validateContent(value Template, locale string, content Content, variables map[string]Variable, capability Capability) error {
	if strings.TrimSpace(locale) == "" {
		return invalid("backend.notification.template_locale_invalid", "template_key", value.Key)
	}
	if capability.Channel == "email" && strings.TrimSpace(content.Subject) == "" {
		return invalid("backend.notification.template_subject_required", "template_key", value.Key, "locale", locale)
	}
	if strings.ContainsAny(content.Subject+content.Title, "\r\n") {
		return invalid("backend.notification.template_subject_invalid", "template_key", value.Key, "locale", locale)
	}
	if capability.Channel == "email" && strings.TrimSpace(content.Text) == "" && strings.TrimSpace(content.HTML) == "" {
		return invalid("backend.notification.template_body_required", "template_key", value.Key, "locale", locale)
	}
	if capability.Channel != "email" && strings.TrimSpace(content.Text) == "" && strings.TrimSpace(content.Markdown) == "" {
		return invalid("backend.notification.template_body_required", "template_key", value.Key, "locale", locale)
	}
	if strings.TrimSpace(content.HTML) != "" && !capability.SupportsHTML {
		return invalid("backend.notification.template_channel_mismatch", "template_key", value.Key, "locale", locale, "field", "html")
	}
	if strings.TrimSpace(content.Markdown) != "" && !capability.SupportsMarkdown {
		return invalid("backend.notification.template_channel_mismatch", "template_key", value.Key, "locale", locale, "field", "markdown")
	}
	if len(content.Facts) > capability.MaxFacts || len(content.Actions) > capability.MaxActions {
		return invalid("backend.notification.template_components_limit", "template_key", value.Key, "locale", locale)
	}
	sources := []string{content.Subject, content.Title, content.Text, content.HTML, content.Markdown}
	for _, fact := range content.Facts {
		if strings.TrimSpace(fact.Key) == "" || strings.TrimSpace(fact.Value) == "" || !capability.SupportsFacts {
			return invalid("backend.notification.template_fact_invalid", "template_key", value.Key, "locale", locale)
		}
		sources = append(sources, fact.Key, fact.Value)
	}
	for _, action := range content.Actions {
		style := strings.TrimSpace(action.Style)
		if strings.TrimSpace(action.Label) == "" || strings.TrimSpace(action.URL) == "" || !capability.SupportsURLActions || (style != "" && style != "primary" && style != "secondary" && style != "danger") {
			return invalid("backend.notification.template_action_invalid", "template_key", value.Key, "locale", locale)
		}
		sources = append(sources, action.Label, action.URL)
	}
	if content.ProviderTemplate != nil {
		if !capability.SupportsProviderTemplate {
			return invalid("backend.notification.provider_template_unsupported", "template_key", value.Key, "locale", locale, "provider", value.Provider)
		}
		if len(content.Facts) > 0 || len(content.Actions) > 0 {
			return invalid("backend.notification.provider_template_components_conflict", "template_key", value.Key, "locale", locale)
		}
		if err := v.capabilities.ValidateProviderTemplate(value.Channel, value.Provider, content.ProviderTemplate); err != nil {
			return invalid("backend.notification.provider_template_invalid", "template_key", value.Key, "locale", locale, "provider", value.Provider)
		}
		for _, component := range content.ProviderTemplate.Components {
			sources = append(sources, component.Parameters...)
		}
	}
	for _, source := range sources {
		if err := validateTokens(value.Key, locale, source, variables); err != nil {
			return err
		}
	}
	return nil
}

func validateTokens(templateKey, locale, source string, variables map[string]Variable) error {
	matches := tokenPattern.FindAllStringSubmatch(source, -1)
	consumed := tokenPattern.ReplaceAllString(source, "")
	if strings.Contains(consumed, "{{") || strings.Contains(consumed, "}}") {
		return invalid("backend.notification.template_syntax_invalid", "template_key", templateKey, "locale", locale)
	}
	for _, match := range matches {
		root := strings.Split(match[1], ".")[0]
		if _, found := variables[root]; !found {
			return invalid("backend.notification.template_variable_unknown", "template_key", templateKey, "locale", locale, "variable", root)
		}
	}
	return nil
}

func ContentHash(value Template) string {
	value.ContentHash = ""
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func ValueHash(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func notificationCapabilitiesUnavailable() error {
	return invalid("backend.notification.template_provider_unsupported")
}
