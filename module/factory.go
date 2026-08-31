// Package module exposes the in-process Notification module composition
// boundary. Domain, application, persistence, and assembly implementations
// remain internal to the Notification module.
package module

import (
	notificationsdk "github.com/domainry/domainry-notification-sdk"
	notificationmodule "github.com/domainry/domainry-notification/internal/assembly/module"
	notificationsaas "github.com/domainry/domainry-notification/internal/assembly/saas"
)

type Options = notificationmodule.Options
type Factory = notificationmodule.Factory

func OptionsFromEnvironment() Options { return notificationmodule.OptionsFromEnvironment() }

func NewFactory(options Options) *Factory { return notificationmodule.NewFactory(options) }

// NewSaaSFactory keeps SaaS product HTTP ownership in Notification while the
// supplied SDK Factory remains responsible for remote service calls.
func NewSaaSFactory(remote notificationsdk.Factory) notificationsdk.Factory {
	return notificationsaas.NewRemoteFactory(remote)
}
