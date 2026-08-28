package repository

import (
	"context"

	deliverymodel "github.com/domainry/domainry-notification/internal/domain/delivery/model"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

type PolicyStore interface {
	GetPolicy(context.Context) (deliverymodel.Policy, error)
	SavePolicy(context.Context, deliverymodel.Policy) (deliverymodel.Policy, error)
	ListRecipientPreferences(context.Context, notification.WorkspaceID) ([]deliverymodel.RecipientPreference, error)
	GetRecipientPreference(context.Context, notification.WorkspaceID, notification.UserID) (deliverymodel.RecipientPreference, bool, error)
	SaveRecipientPreference(context.Context, notification.WorkspaceID, deliverymodel.RecipientPreference) (deliverymodel.RecipientPreference, error)
	ReserveBatch(context.Context, notification.WorkspaceID, []deliverymodel.Reservation, int, int) error
}

type PlanStore interface {
	GetPlan(context.Context, notification.WorkspaceID, string) (deliverymodel.Plan, bool, error)
	ListDuePlans(context.Context, string, int) ([]deliverymodel.Plan, error)
	ClaimPlan(context.Context, notification.WorkspaceID, string, string, string, string) (deliverymodel.Plan, bool, error)
	IsActionTerminal(context.Context, deliverymodel.Plan) (bool, error)
	CancelPlan(context.Context, deliverymodel.Plan, string, string) error
	CompletePlan(context.Context, deliverymodel.Plan, string, string) error
	CompletePlanBatch(context.Context, []deliverymodel.Plan, string, string) error
	RetryPlan(context.Context, deliverymodel.Plan, string, string, string) error
	FailPlan(context.Context, deliverymodel.Plan, string, string) error
}
