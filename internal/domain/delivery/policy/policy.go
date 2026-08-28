package policy

import (
	"context"

	deliverymodel "github.com/domainry/domainry-notification/internal/domain/delivery/model"
)

// PolicyEvaluator is consumed by channel orchestration immediately before an
// immutable dispatch is accepted. deliverymodel.Evaluation owns preference, rate, dedupe,
// quiet-hours, and fallback-order decisions.
type PolicyEvaluator interface {
	EvaluateDelivery(context.Context, deliverymodel.Evaluation) (deliverymodel.Decision, error)
}
