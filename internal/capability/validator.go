// Package capability owns Notification's topology-neutral capability
// candidate validator. It composes only immutable SDK/domain validation rules
// and never opens persistence, workers, authorization, or network transports.
package capability

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/domainry/domainry-foundation/modulecapability"
	"github.com/domainry/domainry-notification-sdk/contract"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

type ownerValidator struct {
	templates *contract.NotificationTemplateValidator
}

// NewOwnerValidator builds the exact pure validator used by the public
// capability binding. Provider capabilities and product surfaces come from the
// Notification SDK, not from the currently mounted Runtime topology.
func NewOwnerValidator() (modulecapability.Validator, error) {
	templateCatalog, err := modulehost.DefaultTemplateCapabilityCatalog()
	if err != nil {
		return nil, fmt.Errorf("build Notification template capability catalog: %w", err)
	}
	templateValidator, err := contract.NewNotificationTemplateValidator(templateCatalog)
	if err != nil {
		return nil, err
	}
	validator := &ownerValidator{templates: templateValidator}
	return validator.Validate, nil
}

func (v *ownerValidator) Validate(_ context.Context, request modulecapability.ValidationRequest) (modulecapability.ValidationResult, error) {
	invalid := func(rule, field string, err error) (modulecapability.ValidationResult, error) {
		message := "Notification candidate is invalid"
		if err != nil && strings.TrimSpace(err.Error()) != "" {
			message = err.Error()
		}
		return modulecapability.ValidationResult{Diagnostics: []modulecapability.Diagnostic{{
			Owner: "notification", RuleKey: normalizeRuleKey(rule), Severity: modulecapability.SeverityError, FieldPath: field, Message: message,
		}}}, nil
	}
	switch request.Kind {
	case "notification.template":
		var value contract.NotificationTemplate
		if err := modulecapability.DecodeKeyedAuthoringValue(request.Candidate, "key", &value); err != nil {
			return invalid("notification.candidate.invalid_json", "$.candidate.value", err)
		}
		if strings.TrimSpace(value.Key) != request.Candidate.Key {
			return invalid("notification.template.source_key_invalid", "$.candidate.value.key", fmt.Errorf("template value key must equal the source fragment key"))
		}
		if err := v.templates.ValidateEditable(value); err != nil {
			return invalid(validationRule(err, "notification.template.invalid"), "$.candidate.value", err)
		}
	case "notification.event_type":
		var value contract.NotificationEventType
		if err := modulecapability.DecodeKeyedAuthoringValue(request.Candidate, "key", &value); err != nil {
			return invalid("notification.candidate.invalid_json", "$.candidate.value", err)
		}
		if strings.TrimSpace(value.Key) != request.Candidate.Key {
			return invalid("notification.event_type.source_key_invalid", "$.candidate.value.key", fmt.Errorf("event type value key must equal the source fragment key"))
		}
		if _, err := contract.ValidateEventType(value); err != nil {
			return invalid(validationRule(err, "notification.event_type.invalid"), "$.candidate.value", err)
		}
	case "notification.rule":
		var value contract.NotificationRule
		if err := modulecapability.DecodeKeyedAuthoringValue(request.Candidate, "event_type_key", &value); err != nil {
			return invalid("notification.candidate.invalid_json", "$.candidate.value", err)
		}
		if strings.TrimSpace(value.EventTypeKey) != request.Candidate.Key {
			return invalid("notification.rule.source_key_invalid", "$.candidate.value.event_type_key", fmt.Errorf("rule event_type_key must equal the source fragment key"))
		}
		eventTypes := []contract.NotificationEventType{}
		for _, fragment := range modulecapability.ReferencedFragments(request, "notification_event_types") {
			var eventType contract.NotificationEventType
			if err := modulecapability.DecodeKeyedAuthoringValue(fragment, "key", &eventType); err != nil {
				return invalid("notification.rule.context_invalid", "$.referenced_context", err)
			}
			eventTypes = append(eventTypes, eventType)
		}
		if err := contract.ValidateEventTypes(eventTypes, []contract.NotificationRule{value}); err != nil {
			return invalid(validationRule(err, "notification.rule.invalid"), "$.candidate.value", err)
		}
	default:
		return modulecapability.ValidationResult{}, &modulecapability.Error{StatusCode: 400, Code: "module_capability.validation_scope_invalid"}
	}
	return modulecapability.ValidationResult{Diagnostics: []modulecapability.Diagnostic{}}, nil
}

func validationRule(err error, fallback string) string {
	if code := strings.TrimSpace(notification.ErrorCode(err)); code != "" {
		return code
	}
	var templateError *contract.TemplateValidationError
	if errors.As(err, &templateError) && templateError != nil && strings.TrimSpace(templateError.Code) != "" {
		return templateError.Code
	}
	return fallback
}

func normalizeRuleKey(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "backend."))
	var result strings.Builder
	for _, character := range value {
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '.' || character == '_' || character == '-' {
			result.WriteRune(character)
		} else {
			result.WriteByte('_')
		}
	}
	if result.Len() == 0 {
		return "notification.candidate.invalid"
	}
	return result.String()
}
