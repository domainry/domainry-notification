package delivery

import (
	"context"

	"github.com/domainry/domainry-notification"
)

type PolicyStore interface {
	GetPolicy(context.Context) (Policy, error)
	SavePolicy(context.Context, Policy) (Policy, error)
	ListRecipientPreferences(context.Context, notification.WorkspaceID) ([]RecipientPreference, error)
	GetRecipientPreference(context.Context, notification.WorkspaceID, notification.UserID) (RecipientPreference, bool, error)
	SaveRecipientPreference(context.Context, notification.WorkspaceID, RecipientPreference) (RecipientPreference, error)
	Reserve(context.Context, notification.WorkspaceID, Reservation, int, int) error
}

type PlanStore interface {
	GetPlan(context.Context, notification.WorkspaceID, string) (Plan, bool, error)
	ListDuePlans(context.Context, string, int) ([]Plan, error)
	ClaimPlan(context.Context, notification.WorkspaceID, string, string, string, string) (Plan, bool, error)
	IsActionTerminal(context.Context, Plan) (bool, error)
	CancelPlan(context.Context, Plan, string, string) error
	CompletePlan(context.Context, Plan, string, string) error
	CompletePlanBatch(context.Context, []Plan, string, string) error
	RetryPlan(context.Context, Plan, string, string, string) error
	FailPlan(context.Context, Plan, string, string) error
}
