package module

import (
	"fmt"

	actioncontract "github.com/domainry/domainry-foundation/action"
	"github.com/domainry/domainry-foundation/modulehttp"
	"github.com/domainry/domainry-notification-sdk/contract"
	notificationapplication "github.com/domainry/domainry-notification/internal/application"
)

type templateReviewInput struct {
	Reason string `json:"reason"`
}

type templatePreviewDraftInput struct {
	Template   contract.NotificationTemplate `json:"template"`
	Locale     string                        `json:"locale,omitempty"`
	Recipients []string                      `json:"recipients,omitempty"`
	Variables  map[string]any                `json:"variables,omitempty"`
}

type templateDraftInput struct {
	Template          contract.NotificationTemplate `json:"template"`
	ExpectedUpdatedAt string                        `json:"expected_updated_at,omitempty"`
}

type templatePublicationInput struct {
	ScheduledFor      string `json:"scheduled_for,omitempty"`
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty"`
}

type templateLifecycleInput struct {
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty"`
}

type templatePreviewInput struct {
	Locale     string         `json:"locale,omitempty"`
	Recipients []string       `json:"recipients,omitempty"`
	Variables  map[string]any `json:"variables,omitempty"`
}

func notificationRoutes() ([]modulehttp.Route, error) {
	definitions, err := notificationapplication.AuthorizationActions()
	if err != nil {
		return nil, err
	}
	routes := make([]modulehttp.Route, 0, len(definitions))
	for _, definition := range definitions {
		if definition.HTTP == nil {
			continue
		}
		route, err := modulehttp.RouteFromAction(definition)
		if err != nil {
			return nil, fmt.Errorf("project Notification Action %q: %w", definition.Key, err)
		}
		routes = append(routes, route)
	}
	return routes, nil
}

// AuthorizationActions returns a detached projection for build-time clients.
func AuthorizationActions() ([]actioncontract.ActionDefinition, error) {
	return notificationapplication.AuthorizationActions()
}
