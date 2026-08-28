package validation

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	sdkcontract "github.com/domainry/domainry-notification-sdk/contract"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	templatemodel "github.com/domainry/domainry-notification/internal/domain/template/model"
)

var tokenPattern = regexp.MustCompile(`\{\{\s*([a-z][a-z0-9_.-]*)\s*\}\}`)

// Validator preserves Notification's source API while delegating portable
// wire semantics to the SDK contract used by Runtime and Remote clients.
type Validator struct {
	contract *sdkcontract.NotificationTemplateValidator
}

func NewValidator(capabilities *Capabilities) (*Validator, error) {
	if capabilities == nil {
		return nil, fmt.Errorf("notification template capabilities are required")
	}
	providers := make([]sdkcontract.NotificationTemplateProvider, 0, len(capabilities.entries))
	for _, source := range capabilities.entries {
		capability, err := convertTemplateValidationValue[sdkcontract.NotificationTemplateCapability](source.capability)
		if err != nil {
			return nil, err
		}
		provider := sdkcontract.NotificationTemplateProvider{Capability: capability}
		if source.validate != nil {
			validate := source.validate
			provider.ValidateProviderTemplate = func(value *sdkcontract.NotificationProviderTemplate) error {
				converted, err := convertTemplateValidationValue[templatemodel.ProviderTemplate](value)
				if err != nil {
					return err
				}
				return validate(&converted)
			}
		}
		providers = append(providers, provider)
	}
	catalog, err := sdkcontract.NewNotificationTemplateCapabilities(providers)
	if err != nil {
		return nil, err
	}
	validator, err := sdkcontract.NewNotificationTemplateValidator(catalog)
	if err != nil {
		return nil, err
	}
	return &Validator{contract: validator}, nil
}

func (v *Validator) ValidateAll(values []templatemodel.Template) error {
	converted := make([]sdkcontract.NotificationTemplate, len(values))
	for index := range values {
		value, err := convertTemplateValidationValue[sdkcontract.NotificationTemplate](values[index])
		if err != nil {
			return err
		}
		converted[index] = value
	}
	return mapTemplateValidationError(v.contract.ValidateAll(converted))
}

func (v *Validator) Validate(value templatemodel.Template) error {
	converted, err := convertTemplateValidationValue[sdkcontract.NotificationTemplate](value)
	if err != nil {
		return err
	}
	if v == nil || v.contract == nil {
		return notificationCapabilitiesUnavailable()
	}
	return mapTemplateValidationError(v.contract.Validate(converted))
}

func (v *Validator) ValidateEditable(value templatemodel.Template) error {
	converted, err := convertTemplateValidationValue[sdkcontract.NotificationTemplate](value)
	if err != nil {
		return err
	}
	if v == nil || v.contract == nil {
		return notificationCapabilitiesUnavailable()
	}
	return mapTemplateValidationError(v.contract.ValidateEditable(converted))
}

func convertTemplateValidationValue[To any](value any) (To, error) {
	var result To
	raw, err := json.Marshal(value)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, err
	}
	return result, nil
}

func mapTemplateValidationError(err error) error {
	if err == nil {
		return nil
	}
	var validation *sdkcontract.TemplateValidationError
	if !errors.As(err, &validation) {
		return err
	}
	params := make(map[string]any, len(validation.Params))
	for key, value := range validation.Params {
		params[key] = value
	}
	return notification.NewError(notification.ErrorInvalid, validation.Code, nil, params)
}

func ContentHash(value templatemodel.Template) string {
	converted, _ := convertTemplateValidationValue[sdkcontract.NotificationTemplate](value)
	return sdkcontract.NotificationTemplateContentHash(converted)
}

func ValueHash(value any) string { return sdkcontract.NotificationValueHash(value) }

func notificationCapabilitiesUnavailable() error {
	return notification.NewError(notification.ErrorInvalid, "backend.notification.template_provider_unsupported", nil, nil)
}
