// Package module provides the in-process Notification SDK Factory. It borrows
// the Runtime Host database and Identity Binding and owns neither lifecycle.
package module

import (
	"context"
	"fmt"
	"strings"
	"time"

	identityprincipal "github.com/domainry/domainry-identity-sdk/authorization/principal"
	"github.com/domainry/domainry-notification"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/contract"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	"github.com/domainry/domainry-notification/delivery"
	"github.com/domainry/domainry-notification/inbox"
	"github.com/domainry/domainry-notification/sqlstore"
	"github.com/domainry/domainry-notification/template"
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
	if host == nil || host.Database() == nil || host.Dialect() == nil || host.WorkspaceScope() == nil || host.QueueScopes() == nil || host.Identity() == nil || host.Clock() == nil || strings.TrimSpace(host.WorkerID()) == "" || host.WorkNotifier() == nil || host.RecipientDirectory() == nil || host.DeliveryGateway() == nil {
		return nil, fmt.Errorf("notification Module host is incomplete")
	}
	if mode == notificationsdk.DeploymentModeModule {
		migrationHost, ok := host.(modulehost.MigrationHost)
		if !ok || migrationHost.Migrations() == nil {
			return nil, fmt.Errorf("notification Module migration host is required")
		}
		registrar := migrationHost.Migrations()
		migrations, err := sqlstore.SchemaMigrations(sqlstore.Driver(registrar.Driver()), registrar.Schema(), "")
		if err != nil {
			return nil, err
		}
		baseline, err := sqlstore.ModuleSchemaBaseline(sqlstore.Driver(registrar.Driver()), "")
		if err != nil {
			return nil, err
		}
		hostBaseline := modulehost.SchemaBaseline{Tables: make([]modulehost.SchemaTable, len(baseline.Tables))}
		for tableIndex, table := range baseline.Tables {
			hostTable := modulehost.SchemaTable{Name: table.Name, Columns: make([]modulehost.SchemaColumn, len(table.Columns)), Indexes: make([]modulehost.SchemaIndex, len(table.Indexes))}
			for columnIndex, column := range table.Columns {
				hostTable.Columns[columnIndex] = modulehost.SchemaColumn{Name: column.Name, Type: column.Type, Nullable: column.Nullable, PrimaryKey: column.PrimaryKey}
			}
			for indexIndex, index := range table.Indexes {
				hostTable.Indexes[indexIndex] = modulehost.SchemaIndex{Name: index.Name, Unique: index.Unique, Columns: append([]string(nil), index.Columns...)}
			}
			hostBaseline.Tables[tableIndex] = hostTable
		}
		hostMigrations := make([]modulehost.SchemaMigration, len(migrations))
		for index, migration := range migrations {
			hostMigrations[index] = modulehost.SchemaMigration{Version: migration.Version, Name: migration.Name, Statements: append([]string(nil), migration.Statements...)}
			if migration.Version == 1 {
				value := hostBaseline
				hostMigrations[index].Baseline = &value
			}
		}
		if err := registrar.ApplyOwnedMigrations(ctx, "notification", hostMigrations); err != nil {
			return nil, fmt.Errorf("apply Notification Module migrations: %w", err)
		}
	}
	store, err := sqlstore.New(sqlstore.Config{Database: host.Database(), Dialect: host.Dialect(), WorkspaceScope: workspaceScopeAdapter{host.WorkspaceScope()}, QueueScopes: queueScopeAdapter{host.QueueScopes()}, Clock: host.Clock()})
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
	templateEngine, err := template.NewEngine(catalog.DefaultLocale, installedTemplates, templateValidator, recipientDirectoryAdapter{host.RecipientDirectory()})
	if err != nil {
		return nil, err
	}
	templateManager, err := template.NewManager(template.ManagerDependencies{Store: store, Engine: templateEngine, Validator: templateValidator})
	if err != nil {
		return nil, err
	}
	notifier := workNotifierAdapter{host.WorkNotifier()}
	publicationProcessor, err := template.NewPublicationProcessor(template.PublicationProcessorDependencies{Store: store, Manager: templateManager, Clock: host.Clock(), WorkerID: host.WorkerID(), WorkNotifier: notifier})
	if err != nil {
		return nil, err
	}
	surfaces := make([]string, len(catalog.Surfaces))
	copy(surfaces, catalog.Surfaces)
	configurationSurfaces := make([]notification.Surface, len(surfaces))
	for i := range surfaces {
		configurationSurfaces[i] = notification.Surface(surfaces[i])
	}
	inboxConfiguration, err := inbox.NewConfiguration(configurationSurfaces, catalog.ExternalChannels)
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
	inboxProcessor, err := inbox.NewProcessor(inbox.ProcessorDependencies{Events: store, Clock: host.Clock(), WorkerID: host.WorkerID(), Audiences: audienceAdapter{host.AudienceResolver()}, RecipientLocale: recipientLocaleAdapter{host.RecipientDirectory()}, WorkNotifier: notifier})
	if err != nil {
		return nil, err
	}
	policyManager, err := delivery.NewPolicyManager(delivery.PolicyManagerDependencies{Store: store, Clock: host.Clock()})
	if err != nil {
		return nil, err
	}
	deliveryProcessor, err := delivery.NewProcessor(delivery.ProcessorDependencies{Plans: store, Renderer: templateEngine, Dispatcher: deliveryGatewayAdapter{host.DeliveryGateway()}, Policy: policyManager, Clock: host.Clock(), WorkerID: host.WorkerID()})
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
	return b, nil
}

var _ notificationsdk.Factory = (*Factory)(nil)
var _ modulehost.Factory = (*Factory)(nil)
