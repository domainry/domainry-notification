// Package capability exposes Notification's source-owned capability contract
// for explicit Plane composition. It does not open stores, workers, or HTTP.
package capability

import (
	"github.com/domainry/domainry-foundation/modulecapability"
)

// Inputs is intentionally empty: source-owned Notification capability truth
// cannot vary with a Runtime host, database, or Module/SaaS topology.
type Inputs struct{}

// Open returns the same immutable binding used by Module and SaaS assemblies.
func Open(inputs Inputs) (*modulecapability.StaticBinding, error) {
	return openContract(inputs)
}
