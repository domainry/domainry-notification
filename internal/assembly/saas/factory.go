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
	adapter, err := notificationhttp.NewAdapter(binding)
	if err != nil {
		_ = binding.Close(context.WithoutCancel(ctx))
		return nil, err
	}
	return newBindingWithHTTPAdapter(binding, adapter), nil
}

type bindingWithHTTPAdapter struct {
	notificationsdk.Binding
	systemTemplates notificationsdk.SystemTemplates
	systemSubjects  notificationsdk.SystemSubjects
	systemRetention notificationsdk.SystemRetention
	systemMigration notificationsdk.SystemMigration
	adapters        []modulehttp.Adapter
}

func newBindingWithHTTPAdapter(binding notificationsdk.Binding, adapter modulehttp.Adapter) *bindingWithHTTPAdapter {
	result := &bindingWithHTTPAdapter{Binding: binding, adapters: []modulehttp.Adapter{adapter}}
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

func (b *bindingWithHTTPAdapter) SystemTemplates() notificationsdk.SystemTemplates {
	return b.systemTemplates
}
func (b *bindingWithHTTPAdapter) SystemSubjects() notificationsdk.SystemSubjects {
	return b.systemSubjects
}
func (b *bindingWithHTTPAdapter) SystemRetention() notificationsdk.SystemRetention {
	return b.systemRetention
}
func (b *bindingWithHTTPAdapter) SystemMigration() notificationsdk.SystemMigration {
	return b.systemMigration
}
func (b *bindingWithHTTPAdapter) HTTPAdapters() []modulehttp.Adapter {
	return append([]modulehttp.Adapter(nil), b.adapters...)
}
func (b *bindingWithHTTPAdapter) AuthorizationActions() ([]actioncontract.ActionDefinition, error) {
	return notificationapplication.AuthorizationActions()
}

var _ notificationsdk.Factory = (*RemoteFactory)(nil)
var _ notificationsdk.SystemTemplateBinding = (*bindingWithHTTPAdapter)(nil)
var _ notificationsdk.SystemSubjectBinding = (*bindingWithHTTPAdapter)(nil)
var _ notificationsdk.SystemRetentionBinding = (*bindingWithHTTPAdapter)(nil)
var _ notificationsdk.SystemMigrationBinding = (*bindingWithHTTPAdapter)(nil)
var _ modulehttp.Provider = (*bindingWithHTTPAdapter)(nil)
var _ actioncontract.Provider = (*bindingWithHTTPAdapter)(nil)
