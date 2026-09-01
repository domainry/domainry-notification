package saas

import (
	"context"
	"fmt"

	actioncontract "github.com/domainry/domainry-foundation/action"
	"github.com/domainry/domainry-foundation/modulehttp"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
	notificationapplication "github.com/domainry/domainry-notification/internal/application"
	notificationhttp "github.com/domainry/domainry-notification/internal/transport/http/module"
)

// RemoteFactory decorates the SDK Remote Factory with Notification-owned
// product HTTP. Runtime remains unaware of Notification's concrete transport.
type RemoteFactory struct {
	remote notificationsdk.Factory
}

func NewRemoteFactory(remote notificationsdk.Factory) *RemoteFactory {
	return &RemoteFactory{remote: remote}
}

func (f *RemoteFactory) Open(ctx context.Context, application notificationsdk.ApplicationRef) (notificationsdk.Binding, error) {
	if f == nil || f.remote == nil {
		return nil, fmt.Errorf("Notification SaaS Remote Factory is required")
	}
	binding, err := f.remote.Open(ctx, application)
	if err != nil {
		return nil, err
	}
	surface, err := notificationhttp.NewSurface(binding)
	if err != nil {
		_ = binding.Close(context.WithoutCancel(ctx))
		return nil, err
	}
	return newBindingWithHTTPSurface(binding, surface), nil
}

type bindingWithHTTPSurface struct {
	notificationsdk.Binding
	systemTemplates notificationsdk.SystemTemplates
	systemSubjects  notificationsdk.SystemSubjects
	systemRetention notificationsdk.SystemRetention
	systemMigration notificationsdk.SystemMigration
	surfaces        []modulehttp.Surface
}

func newBindingWithHTTPSurface(binding notificationsdk.Binding, surface modulehttp.Surface) *bindingWithHTTPSurface {
	result := &bindingWithHTTPSurface{Binding: binding, surfaces: []modulehttp.Surface{surface}}
	if value, ok := binding.(notificationsdk.SystemTemplateBinding); ok {
		result.systemTemplates = value.SystemTemplates()
	}
	if value, ok := binding.(notificationsdk.SystemSubjectBinding); ok {
		result.systemSubjects = value.SystemSubjects()
	}
	if value, ok := binding.(notificationsdk.SystemRetentionBinding); ok {
		result.systemRetention = value.SystemRetention()
	}
	if value, ok := binding.(notificationsdk.SystemMigrationBinding); ok {
		result.systemMigration = value.SystemMigration()
	}
	return result
}

func (b *bindingWithHTTPSurface) SystemTemplates() notificationsdk.SystemTemplates {
	return b.systemTemplates
}
func (b *bindingWithHTTPSurface) SystemSubjects() notificationsdk.SystemSubjects {
	return b.systemSubjects
}
func (b *bindingWithHTTPSurface) SystemRetention() notificationsdk.SystemRetention {
	return b.systemRetention
}
func (b *bindingWithHTTPSurface) SystemMigration() notificationsdk.SystemMigration {
	return b.systemMigration
}
func (b *bindingWithHTTPSurface) HTTPSurfaces() []modulehttp.Surface {
	return append([]modulehttp.Surface(nil), b.surfaces...)
}
func (b *bindingWithHTTPSurface) AuthorizationActions() ([]actioncontract.ActionDefinition, error) {
	return notificationapplication.AuthorizationActions()
}

var _ notificationsdk.Factory = (*RemoteFactory)(nil)
var _ notificationsdk.SystemTemplateBinding = (*bindingWithHTTPSurface)(nil)
var _ notificationsdk.SystemSubjectBinding = (*bindingWithHTTPSurface)(nil)
var _ notificationsdk.SystemRetentionBinding = (*bindingWithHTTPSurface)(nil)
var _ notificationsdk.SystemMigrationBinding = (*bindingWithHTTPSurface)(nil)
var _ modulehttp.Provider = (*bindingWithHTTPSurface)(nil)
var _ actioncontract.Provider = (*bindingWithHTTPSurface)(nil)
