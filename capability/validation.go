package capability

import (
	"context"
	"encoding/json"
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

// newOwnerValidator builds the exact pure validator used by the public
// capability binding. Provider capabilities and semantic routes come from the
// Notification SDK, not from the currently mounted Runtime topology.
func newOwnerValidator() (modulecapability.Validator, error) {
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
		applyAuthoringPublicationDefaults(&value.Status, &value.Version)
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
		applyAuthoringEventDefaults(&value)
		if _, err := contract.ValidateEventType(value); err != nil {
			return invalid(validationRule(err, "notification.event_type.invalid"), "$.candidate.value", err)
		}
		if !hasProjectRecordSemanticRoute(value.Actions) {
			diagnostics := []modulecapability.Diagnostic{}
			for _, fragment := range modulecapability.ReferencedFragments(request, "actions") {
				if !actionReferencesNotificationEvent(fragment, request.Candidate.Key) {
					continue
				}
				diagnostics = append(diagnostics, modulecapability.Diagnostic{
					Owner: "notification", RuleKey: "notification.event_type.project_record_action_required", Severity: modulecapability.SeverityError,
					FieldPath: "$.candidate.value.actions",
					Message:   fmt.Sprintf("Project notification event type %s is referenced by Action %s and must publish a project_record semantic route action with an explicit route_key", request.Candidate.Key, fragment.Key),
					Params: map[string]string{
						"event_type": request.Candidate.Key, "action_key": fragment.Key, "required_resource_type": "project_record", "route_key_inference": "forbidden",
					},
				})
			}
			if len(diagnostics) > 0 {
				return modulecapability.ValidationResult{Diagnostics: diagnostics}, nil
			}
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
			applyAuthoringEventDefaults(&eventType)
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

func hasProjectRecordSemanticRoute(actions []contract.NotificationInboxActionDescriptor) bool {
	for _, action := range actions {
		if strings.TrimSpace(action.ResourceType) == "project_record" && strings.TrimSpace(action.RouteKey) != "" {
			return true
		}
	}
	return false
}

func actionReferencesNotificationEvent(fragment modulecapability.AuthoringFragment, eventType string) bool {
	var action struct {
		Handler *struct {
			NotificationEventTypes []string `json:"notification_event_types"`
		} `json:"handler"`
	}
	if json.Unmarshal(fragment.Value, &action) != nil || action.Handler == nil {
		return false
	}
	for _, candidate := range action.Handler.NotificationEventTypes {
		if strings.TrimSpace(candidate) == eventType {
			return true
		}
	}
	return false
}

func applyAuthoringPublicationDefaults(status *string, version *int) {
	if strings.TrimSpace(*status) == "" {
		*status = "published"
	}
	if *version == 0 {
		*version = 1
	}
}

func applyAuthoringEventDefaults(value *contract.NotificationEventType) {
	applyAuthoringPublicationDefaults(&value.Status, &value.Version)
	for index := range value.Actions {
		if strings.TrimSpace(value.Actions[index].Kind) == "" {
			value.Actions[index].Kind = "route"
		}
	}
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
