// Package module provides the in-process Notification SDK Factory. It borrows
// the Runtime Host database and Identity Binding and owns neither lifecycle.
package module

import (
	"context"
	"fmt"
	"strings"
	"time"

	shareddefinition "github.com/domainry/domainry-foundation/definition"
	"github.com/domainry/domainry-foundation/modulehttp"
	sharedoperation "github.com/domainry/domainry-foundation/operation"
	"github.com/domainry/domainry-foundation/requestcontext"
	identitysdk "github.com/domainry/domainry-identity-sdk"
	identityprincipal "github.com/domainry/domainry-identity-sdk/authorization/principal"
	metadatasdk "github.com/domainry/domainry-metadata-sdk"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/contract"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	notificationapplication "github.com/domainry/domainry-notification/internal/application"
	appdelivery "github.com/domainry/domainry-notification/internal/application/delivery"
	appinbox "github.com/domainry/domainry-notification/internal/application/inbox"
	apptemplate "github.com/domainry/domainry-notification/internal/application/template"
	"github.com/domainry/domainry-notification/internal/domain/delivery/service"
	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-notification/internal/domain/template/service"
	sqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence"
	operationstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/operation"
	notificationhttp "github.com/domainry/domainry-notification/internal/transport/http/module"
)

type Options struct{}

func OptionsFromEnvironment() Options { return Options{} }

type Factory struct{ options Options }

func NewFactory(options Options) *Factory { return &Factory{options: options} }
func (f *Factory) Open(context.Context, notificationsdk.ApplicationRef) (notificationsdk.Binding, error) {
	return nil, &notificationsdk.Error{StatusCode: 500, Code: "notification.module_host_required"}
}

func (f *Factory) OpenModule(ctx context.Context, application notificationsdk.ApplicationRef, host modulehost.Host) (notificationsdk.Binding, error) {
	return f.openHosted(ctx, application, host, notificationsdk.DeploymentModeModule)
}

// OpenSaaSApplication assembles the same source-owned domain behavior over a
// standalone service host. It is for Notification's server composition only;
// remote SDK clients still receive a Remote Binding with no LocalWorkers.
func (f *Factory) OpenSaaSApplication(ctx context.Context, application notificationsdk.ApplicationRef, host modulehost.Host) (notificationsdk.Binding, error) {
	return f.openHosted(ctx, application, host, notificationsdk.DeploymentModeSaaS)
}

func (f *Factory) openHosted(ctx context.Context, application notificationsdk.ApplicationRef, host modulehost.Host, mode notificationsdk.DeploymentMode) (notificationsdk.Binding, error) {
	if err := application.Validate(); err != nil {
		return nil, err
	}
	if mode != notificationsdk.DeploymentModeModule && mode != notificationsdk.DeploymentModeSaaS {
		return nil, fmt.Errorf("notification deployment mode %q is unsupported", mode)
	}
	if host == nil || host.Database() == nil || host.Dialect() == nil || host.Migrations() == nil || host.WorkspaceScope() == nil || host.QueueScopes() == nil || host.Identity() == nil || host.Identity().Principals() == nil || host.Clock() == nil || strings.TrimSpace(host.WorkerID()) == "" || host.WorkNotifier() == nil || host.RecipientResolver() == nil || host.DeliveryGateway() == nil {
		return nil, fmt.Errorf("notification Module host is incomplete")
	}
	archiveHost, ok := host.(modulehost.RetentionArchiveStoreHost)
	if !ok || archiveHost.RetentionArchiveStore() == nil {
		return nil, fmt.Errorf("notification shared Lifecycle retention archive store is required")
	}
	if mode == notificationsdk.DeploymentModeModule {
		registrar := host.Migrations()
		migrations, err := sqlstore.SchemaMigrations(sqlstore.Driver(registrar.Driver()), registrar.Schema(), "")
		if err != nil {
			return nil, err
		}
		hostMigrations := make([]modulehost.SchemaMigration, len(migrations))
		for index, migration := range migrations {
			hostMigrations[index] = modulehost.SchemaMigration{Version: migration.Version, Name: migration.Name, Statements: append([]string(nil), migration.Statements...)}
		}
		if err := registrar.ApplyOwnedMigrations(ctx, sqlstore.MigrationOwner, hostMigrations); err != nil {
			return nil, fmt.Errorf("apply Notification Module migrations: %w", err)
		}
	}
	installationID := strings.TrimSpace(application.ApplicationKey)
	if mode == notificationsdk.DeploymentModeSaaS {
		installationID = "notification-saas:" + strings.TrimSpace(application.WorkspaceID) + ":" + installationID
	}
	definitionKernel, err := shareddefinition.Open(ctx, installationID, host.Database(), host.Dialect(), host.Migrations())
	if err != nil {
		return nil, fmt.Errorf("open Notification Definition persistence: %w", err)
	}
	definitions := metadatasdk.AdaptDefinitionStore(definitionKernel)
	operationKernel, err := sharedoperation.Open(ctx, host.Database(), host.Dialect(), host.Migrations())
	if err != nil {
		return nil, fmt.Errorf("open Notification Operations persistence: %w", err)
	}
	operations, err := operationstore.New(operationKernel)
	if err != nil {
		return nil, err
	}
	store, err := sqlstore.New(sqlstore.Config{Database: host.Database(), Dialect: host.Dialect(), WorkspaceScope: workspaceScopeAdapter{host.WorkspaceScope()}, QueueScopes: queueScopeAdapter{host.QueueScopes()}, Clock: host.Clock(), WorkspaceID: notification.WorkspaceID(application.WorkspaceID), DefinitionStore: definitions, OperationStore: operations, ControlStore: operations, ArchiveStore: archiveHost.RetentionArchiveStore()})
	if err != nil {
		return nil, err
	}
	catalog := host.Catalog()
	capabilities := make([]template.Provider, len(catalog.TemplateCapabilities))
	for index, value := range catalog.TemplateCapabilities {
		provider := template.Provider{Capability: template.Capability{Channel: value.Channel, Provider: value.Provider, SupportsHTML: value.SupportsHTML, SupportsMarkdown: value.SupportsMarkdown, SupportsFacts: value.SupportsFacts, SupportsURLActions: value.SupportsURLActions, SupportsProviderTemplate: value.SupportsProviderTemplate, MaxFacts: value.MaxFacts, MaxActions: value.MaxActions}}
		if value.SupportsProviderTemplate {
			validator := host.ProviderTemplateValidator()
			if validator == nil {
				return nil, fmt.Errorf("provider template validator is required for %s/%s", value.Channel, value.Provider)
			}
			channel, providerKey := value.Channel, value.Provider
			provider.ValidateProviderTemplate = func(input *template.ProviderTemplate) error {
				wire, convertErr := convert[contract.NotificationProviderTemplate](input)
				if convertErr != nil {
					return convertErr
				}
				return validator.ValidateProviderTemplate(channel, providerKey, wire)
			}
		}
		capabilities[index] = provider
	}
	templateCapabilities, err := template.NewCapabilities(capabilities)
	if err != nil {
		return nil, err
	}
	templateValidator, err := template.NewValidator(templateCapabilities)
	if err != nil {
		return nil, err
	}
	installedTemplates, err := convertSlice[template.Template](catalog.Templates)
	if err != nil {
		return nil, err
	}
	templateEngine, err := template.NewEngine(catalog.DefaultLocale, installedTemplates, templateValidator, recipientResolverAdapter{host.RecipientResolver()})
	if err != nil {
		return nil, err
	}
	templateManager, err := template.NewManager(template.ManagerDependencies{Store: store, Engine: templateEngine, Validator: templateValidator})
	if err != nil {
		return nil, err
	}
	notifier := workNotifierAdapter{host.WorkNotifier()}
	publicationProcessor, err := apptemplate.NewPublicationProcessor(apptemplate.PublicationProcessorDependencies{
		Store: store, Manager: templateManager, Clock: host.Clock(), WorkerID: host.WorkerID(), WorkNotifier: notifier,
		AuthorizeResumed: func(ctx context.Context, actor string) (bool, error) {
			resolution, err := host.Identity().Principals().Resolve(requestcontext.WithWorkspaceID(ctx, application.WorkspaceID), identitysdk.PrincipalResolutionRequest{
				SubjectID: identitysdk.SubjectID(strings.TrimSpace(actor)),
			})
			if err != nil {
				return false, err
			}
			principal := resolution.Principal
			principal.AccessBundle = &resolution.AccessBundle
			return principal.Known && principal.WorkspaceID == application.WorkspaceID && principal.UserID == strings.TrimSpace(actor) && principal.HasPermission(notificationapplication.ActionPublicationsApprove), nil
		},
	})
	if err != nil {
		return nil, err
	}
	inboxConfiguration, err := inbox.NewConfiguration(catalog.ExternalChannels)
	if err != nil {
		return nil, err
	}
	inboxValidator, err := inbox.NewValidator(inboxConfiguration)
	if err != nil {
		return nil, err
	}
	eventTypes, err := convertSlice[inbox.EventType](catalog.EventTypes)
	if err != nil {
		return nil, err
	}
	rules, err := convertSlice[inbox.Rule](catalog.Rules)
	if err != nil {
		return nil, err
	}
	eventCatalog, err := inbox.NewCatalog(inboxValidator, eventTypes, rules)
	if err != nil {
		return nil, err
	}
	compiler, err := inbox.NewCompiler(eventCatalog, inboxValidator, host.Clock())
	if err != nil {
		return nil, err
	}
	publisher, err := inbox.NewPublisher(compiler, store, notifier)
	if err != nil {
		return nil, err
	}
	inboxProcessor, err := appinbox.NewProcessor(appinbox.ProcessorDependencies{Events: store, Clock: host.Clock(), WorkerID: host.WorkerID(), Audiences: audienceAdapter{host.AudienceResolver()}, RecipientLocale: recipientLocaleAdapter{host.RecipientResolver()}, WorkNotifier: notifier})
	if err != nil {
		return nil, err
	}
	policyManager, err := delivery.NewPolicyManager(delivery.PolicyManagerDependencies{Store: store, Clock: host.Clock()})
	if err != nil {
		return nil, err
	}
	deliveryProcessor, err := appdelivery.NewProcessor(appdelivery.ProcessorDependencies{Plans: store, Renderer: templateEngine, Dispatcher: deliveryGatewayAdapter{host.DeliveryGateway()}, Policy: policyManager, Clock: host.Clock(), WorkerID: host.WorkerID()})
	if err != nil {
		return nil, err
	}
	mailbox, err := inbox.NewMailboxManager(inbox.MailboxManagerDependencies{Validator: inboxValidator, Mailboxes: store, SavedViews: store, Delegations: store, Metrics: store, Clock: host.Clock()})
	if err != nil {
		return nil, err
	}
	actions, err := inbox.NewActionResolver(mailbox, eventCatalog, host.Clock())
	if err != nil {
		return nil, err
	}
	principalResolver, err := identityprincipal.NewResolver(host.Identity(), identityprincipal.Options{Clock: host.Clock(), MaxCacheTTL: time.Minute})
	if err != nil {
		return nil, err
	}
	b := &binding{application: application, mode: mode, identity: host.Identity(), principals: principalResolver, templates: templateManager, publications: publicationProcessor, engine: templateEngine, publisher: publisher, compiler: compiler, store: store, inboxProcessor: inboxProcessor, policy: policyManager, deliveryProcessor: deliveryProcessor, mailbox: mailbox, actions: actions, catalog: eventCatalog, eventTypes: eventTypes, rules: rules, metrics: host.DeliveryMetrics(), clock: host.Clock(), templateCapabilities: append([]contract.NotificationTemplateCapability(nil), catalog.TemplateCapabilities...)}
	if err := b.RefreshPublished(ctx); err != nil {
		return nil, err
	}
	if mode == notificationsdk.DeploymentModeModule {
		adapter, err := notificationhttp.NewAdapter(b)
		if err != nil {
			return nil, err
		}
		b.SetHTTPAdapters([]modulehttp.Adapter{adapter})
	}
	return b, nil
}

var _ notificationsdk.Factory = (*Factory)(nil)
var _ modulehost.Factory = (*Factory)(nil)
