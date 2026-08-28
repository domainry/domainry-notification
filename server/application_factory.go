package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	identitysdk "github.com/domainry/domainry-identity-sdk"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/contract"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	"github.com/domainry/domainry-notification/module"
)

// SQLApplicationFactory assembles the same Notification domain application as
// Module mode over the service-owned, application-isolated SQL persistence.
type SQLApplicationFactory struct {
	persistence *SQLPersistence
	options     SQLApplicationFactoryOptions
}

type SQLApplicationFactoryOptions struct {
	Persistence               *SQLPersistence
	Catalog                   modulehost.Catalog
	Clock                     modulehost.Clock
	WorkerID                  string
	WorkNotifier              modulehost.WorkNotifier
	AudienceResolver          modulehost.AudienceResolver
	DeliveryGateway           modulehost.DeliveryGateway
	DeliveryMetrics           modulehost.DeliveryMetrics
	ProviderTemplateValidator modulehost.ProviderTemplateValidator
}

func NewSQLApplicationFactory(options SQLApplicationFactoryOptions) (*SQLApplicationFactory, error) {
	if options.Persistence == nil || options.Persistence.Database() == nil {
		return nil, fmt.Errorf("Notification SaaS persistence is required")
	}
	if strings.TrimSpace(options.Catalog.DefaultLocale) == "" {
		return nil, fmt.Errorf("Notification SaaS catalog default locale is required")
	}
	if options.DeliveryGateway == nil {
		return nil, fmt.Errorf("Notification SaaS Delivery Gateway is required")
	}
	if options.Clock == nil {
		options.Clock = wallClock{}
	}
	if strings.TrimSpace(options.WorkerID) == "" {
		return nil, fmt.Errorf("Notification SaaS worker identity is required")
	}
	if options.WorkNotifier == nil {
		options.WorkNotifier = discardWorkNotifier{}
	}
	if options.AudienceResolver == nil {
		options.AudienceResolver = snapshotOnlyAudienceResolver{}
	}
	return &SQLApplicationFactory{persistence: options.Persistence, options: options}, nil
}

func (f *SQLApplicationFactory) OpenSaaS(ctx context.Context, application notificationsdk.ApplicationRef, identity identitysdk.Binding) (notificationsdk.Binding, error) {
	if f == nil || f.persistence == nil {
		return nil, fmt.Errorf("Notification SaaS Application Factory is unavailable")
	}
	if identity == nil || identity.Directory() == nil {
		return nil, fmt.Errorf("Notification SaaS Identity Directory is required")
	}
	dialect, err := f.persistence.PrepareApplication(ctx, application)
	if err != nil {
		return nil, err
	}
	host := &saasApplicationHost{
		application: application,
		database:    f.persistence.Database(),
		dialect:     dialect,
		identity:    identity,
		catalog:     f.options.Catalog,
		clock:       f.options.Clock,
		workerID:    f.options.WorkerID + ":" + applicationTablePrefix(application),
		notifier:    f.options.WorkNotifier,
		directory:   identityRecipientDirectory{application: application, directory: identity.Directory()},
		audiences:   f.options.AudienceResolver,
		gateway:     f.options.DeliveryGateway,
		metrics:     f.options.DeliveryMetrics,
		validator:   f.options.ProviderTemplateValidator,
	}
	return module.NewFactory(module.Options{}).OpenSaaSApplication(ctx, application, host)
}

func (f *SQLApplicationFactory) Close(context.Context) error {
	if f == nil || f.persistence == nil {
		return nil
	}
	return f.persistence.Close()
}

type saasApplicationHost struct {
	application notificationsdk.ApplicationRef
	database    modulehost.Database
	dialect     modulehost.Dialect
	identity    identitysdk.Binding
	catalog     modulehost.Catalog
	clock       modulehost.Clock
	workerID    string
	notifier    modulehost.WorkNotifier
	directory   modulehost.RecipientDirectory
	audiences   modulehost.AudienceResolver
	gateway     modulehost.DeliveryGateway
	metrics     modulehost.DeliveryMetrics
	validator   modulehost.ProviderTemplateValidator
}

func (h *saasApplicationHost) Database() modulehost.Database { return h.database }
func (h *saasApplicationHost) Dialect() modulehost.Dialect   { return h.dialect }
func (h *saasApplicationHost) WorkspaceScope() modulehost.WorkspaceScope {
	return exactWorkspaceScope{workspaceID: h.application.WorkspaceID}
}
func (h *saasApplicationHost) QueueScopes() modulehost.QueueScopeIndex {
	return exactQueueScope{workspaceID: h.application.WorkspaceID}
}
func (h *saasApplicationHost) Identity() identitysdk.Binding                     { return h.identity }
func (h *saasApplicationHost) Clock() modulehost.Clock                           { return h.clock }
func (h *saasApplicationHost) WorkerID() string                                  { return h.workerID }
func (h *saasApplicationHost) Catalog() modulehost.Catalog                       { return h.catalog }
func (h *saasApplicationHost) WorkNotifier() modulehost.WorkNotifier             { return h.notifier }
func (h *saasApplicationHost) RecipientDirectory() modulehost.RecipientDirectory { return h.directory }
func (h *saasApplicationHost) AudienceResolver() modulehost.AudienceResolver     { return h.audiences }
func (h *saasApplicationHost) DeliveryGateway() modulehost.DeliveryGateway       { return h.gateway }
func (h *saasApplicationHost) DeliveryMetrics() modulehost.DeliveryMetrics       { return h.metrics }
func (h *saasApplicationHost) ProviderTemplateValidator() modulehost.ProviderTemplateValidator {
	return h.validator
}

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now().UTC() }

type discardWorkNotifier struct{}

func (discardWorkNotifier) Notify(context.Context, modulehost.WorkLocator) {}

type exactWorkspaceScope struct{ workspaceID string }

func (s exactWorkspaceScope) Context(ctx context.Context, workspaceID string) context.Context {
	if strings.TrimSpace(workspaceID) == strings.TrimSpace(s.workspaceID) {
		return ctx
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	return cancelled
}

type exactQueueScope struct{ workspaceID string }

func (s exactQueueScope) Register(_ context.Context, _ modulehost.Executor, _ string, workspaceID, _ string) error {
	if strings.TrimSpace(workspaceID) != strings.TrimSpace(s.workspaceID) {
		return fmt.Errorf("Notification SaaS queue workspace scope mismatch")
	}
	return nil
}
func (s exactQueueScope) Workspaces(_ context.Context, _ modulehost.Queryer, _ string, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	return []string{s.workspaceID}, nil
}

type identityRecipientDirectory struct {
	application notificationsdk.ApplicationRef
	directory   identitysdk.Directory
}

func (d identityRecipientDirectory) FindRecipient(ctx context.Context, workspaceID, userID string) (modulehost.Recipient, bool, error) {
	if strings.TrimSpace(workspaceID) != d.application.WorkspaceID || strings.TrimSpace(userID) == "" {
		return modulehost.Recipient{}, false, nil
	}
	user, found, err := d.directory.FindUser(ctx, identitysdk.UserLookup{Application: identitysdk.ApplicationScope{
		TenantID: identitysdk.TenantID(d.application.TenantID), WorkspaceID: identitysdk.WorkspaceID(d.application.WorkspaceID), ApplicationKey: identitysdk.ApplicationKey(d.application.ApplicationKey),
	}, UserID: identitysdk.SubjectID(userID)})
	if err != nil || !found {
		return modulehost.Recipient{}, found, err
	}
	return modulehost.Recipient{ID: user.ID, Email: user.Email, Locale: user.Locale, Timezone: user.Timezone}, true, nil
}

// Runtime-business audience callbacks are deliberately unavailable in SaaS.
// Publications must carry recipient snapshots or use Identity-owned facts.
type snapshotOnlyAudienceResolver struct{}

func (snapshotOnlyAudienceResolver) ResolveAudience(context.Context, string, contract.NotificationEvent) ([]string, error) {
	return nil, fmt.Errorf("Notification SaaS requires a recipient snapshot for Runtime-business audiences")
}

var _ ApplicationFactory = (*SQLApplicationFactory)(nil)
var _ modulehost.Host = (*saasApplicationHost)(nil)
