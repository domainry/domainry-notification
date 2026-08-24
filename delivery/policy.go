package delivery

import "context"

// PolicyEvaluator is consumed by template and channel orchestration when a
// host needs to extend the module's persisted policy with external constraints.
type PolicyEvaluator interface {
	EvaluateDelivery(context.Context, Evaluation) (Decision, error)
}
