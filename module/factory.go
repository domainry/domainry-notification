// Package module exposes the in-process Notification module composition
// boundary. Domain, application, persistence, and assembly implementations
// remain internal to the Notification module.
package module

import notificationmodule "github.com/domainry/domainry-notification/internal/assembly/module"

type Options = notificationmodule.Options
type Factory = notificationmodule.Factory

func OptionsFromEnvironment() Options { return notificationmodule.OptionsFromEnvironment() }

func NewFactory(options Options) *Factory { return notificationmodule.NewFactory(options) }
