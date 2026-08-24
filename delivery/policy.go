package delivery

import "context"

// PolicyEvaluator is consumed by channel orchestration immediately before an
// immutable dispatch is accepted. Evaluation owns preference, rate, dedupe,
// quiet-hours, and fallback-order decisions.
type PolicyEvaluator interface {
	EvaluateDelivery(context.Context, Evaluation) (Decision, error)
}
