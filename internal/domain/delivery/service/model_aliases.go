package delivery

import deliverymodel "github.com/domainry/domainry-notification/internal/domain/delivery/model"
import deliverypolicy "github.com/domainry/domainry-notification/internal/domain/delivery/policy"
import deliveryrepository "github.com/domainry/domainry-notification/internal/domain/delivery/repository"

type Policy = deliverymodel.Policy
type RecipientPreference = deliverymodel.RecipientPreference
type Reservation = deliverymodel.Reservation
type Evaluation = deliverymodel.Evaluation
type Decision = deliverymodel.Decision
type Plan = deliverymodel.Plan
type PolicyStore = deliveryrepository.PolicyStore
type PlanStore = deliveryrepository.PlanStore
type PolicyEvaluator = deliverypolicy.PolicyEvaluator
