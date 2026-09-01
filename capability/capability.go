// Package capability exposes Notification's source-owned capability contract
// for explicit Plane composition. It does not open stores, workers, or HTTP.
package capability

import (
	"github.com/domainry/domainry-foundation/modulecapability"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	internalcapability "github.com/domainry/domainry-notification/internal/capability"
	notificationhttp "github.com/domainry/domainry-notification/internal/transport/http/module"
)

// Inputs is intentionally empty: source-owned Notification capability truth
// cannot vary with a Runtime host, database, or Module/SaaS topology.
type Inputs struct{}

// Open returns the same immutable binding used by Module and SaaS assemblies.
func Open(inputs Inputs) (*modulecapability.StaticBinding, error) {
	_ = inputs
	validator, err := internalcapability.NewOwnerValidator()
	if err != nil {
		return nil, err
	}
	return notificationhttp.NewCapabilityBinding(modulehost.DefaultProviderCapabilities(), validator)
}
