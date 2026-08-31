// Package httpapi exposes Notification's product HTTP contract independently
// from whether its SDK Binding is local or remote.
package httpapi

import (
	foundationhttp "github.com/domainry/domainry-foundation/modulehttp"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
	notificationhttp "github.com/domainry/domainry-notification/internal/transport/http/module"
)

// NewSurface adapts a Notification Binding to the module-owned product HTTP
// paths. The same Surface is used with Module and SaaS Bindings.
func NewSurface(binding notificationsdk.Binding) (foundationhttp.Surface, error) {
	return notificationhttp.NewSurface(binding)
}
