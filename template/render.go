package template

import (
	"fmt"
	"html"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func ValidateRenderVariables(value Template, supplied map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(value.Variables))
	allowed := make(map[string]bool, len(value.Variables))
	for _, variable := range value.Variables {
		allowed[strings.TrimSpace(variable.Key)] = true
	}
	for key := range supplied {
		if !allowed[strings.TrimSpace(key)] {
			return nil, invalid("backend.notification.template_variable_unknown", "template_key", value.Key, "variable", key)
		}
	}
	for _, variable := range value.Variables {
		suppliedValue, exists := supplied[variable.Key]
		if variable.Required && (!exists || suppliedValue == nil || strings.TrimSpace(fmt.Sprint(suppliedValue)) == "") {
			return nil, invalid("backend.notification.template_variable_required", "template_key", value.Key, "variable", variable.Key)
		}
		if !exists || suppliedValue == nil {
			continue
		}
		if err := validateVariableValue(variable, suppliedValue); err != nil {
			return nil, invalid("backend.notification.template_variable_invalid", "template_key", value.Key, "variable", variable.Key, "type", variable.Type)
		}
		result[variable.Key] = suppliedValue
	}
	return result, nil
}

// RenderRestricted expands only declared dotted variable paths. It does not
// execute functions, control flow, includes, or arbitrary template code.
func RenderRestricted(source string, values map[string]any, escapeHTML bool) (string, error) {
	if source == "" {
		return "", nil
	}
	var renderErr error
	rendered := tokenPattern.ReplaceAllStringFunc(source, func(token string) string {
		match := tokenPattern.FindStringSubmatch(token)
		value, found := resolveVariablePath(values, match[1])
		if !found {
			renderErr = fmt.Errorf("missing variable %s", match[1])
			return ""
		}
		text := fmt.Sprint(value)
		if escapeHTML {
			return html.EscapeString(text)
		}
		return text
	})
	if renderErr != nil || strings.Contains(rendered, "{{") || strings.Contains(rendered, "}}") {
		return "", fmt.Errorf("notification template render failed")
	}
	return rendered, nil
}

func RenderProviderTemplate(value *ProviderTemplate, variables map[string]any) (*ProviderTemplate, error) {
	if value == nil {
		return nil, nil
	}
	rendered := &ProviderTemplate{Name: value.Name, Language: value.Language, Components: make([]ProviderTemplateComponent, 0, len(value.Components))}
	for _, component := range value.Components {
		next := ProviderTemplateComponent{Type: component.Type, SubType: component.SubType, Index: component.Index, Parameters: make([]string, 0, len(component.Parameters))}
		for _, parameter := range component.Parameters {
			result, err := RenderRestricted(parameter, variables, false)
			if err != nil {
				return nil, err
			}
			next.Parameters = append(next.Parameters, result)
		}
		rendered.Components = append(rendered.Components, next)
	}
	return rendered, nil
}

func validateVariableValue(variable Variable, value any) error {
	text := strings.TrimSpace(fmt.Sprint(value))
	switch variable.Type {
	case "boolean":
		_, err := strconv.ParseBool(text)
		return err
	case "number":
		_, err := strconv.ParseFloat(text, 64)
		return err
	case "date":
		_, err := time.Parse("2006-01-02", text)
		return err
	case "datetime":
		_, err := time.Parse(time.RFC3339, text)
		return err
	case "email":
		_, err := mail.ParseAddress(text)
		return err
	case "url":
		parsed, err := url.ParseRequestURI(text)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("invalid URL")
		}
	}
	return nil
}

func resolveVariablePath(values map[string]any, path string) (any, bool) {
	var current any = values
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}
