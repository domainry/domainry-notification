package persistence

import (
	"context"

	"github.com/domainry/domainry-notification-sdk/modulehost"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"
	deliverystore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/delivery"
	eventstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/event"
	inboxstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/inbox"
	lifecyclestore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/lifecycle"
	migrationstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/migration"
	templatestore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/template"
	"github.com/domainry/domainry-orm/sqlhost"
)

// WorkspaceScope attaches the tenant scope required by a host database layer
// such as PostgreSQL RLS. A host without context-based scoping returns ctx.
type WorkspaceScope interface {
	Context(context.Context, notification.WorkspaceID) context.Context
}

// QueueScopeIndex remains host-owned because it is shared by every Runtime
// worker, not notification state. Register must use the supplied executor so it
// can participate in the same transaction as the durable task.
type QueueScopeIndex interface {
	Register(context.Context, sqlhost.Executor, notification.WorkKind, notification.WorkspaceID, string) error
	Workspaces(context.Context, sqlhost.Queryer, notification.WorkKind, int) ([]notification.WorkspaceID, error)
}

type Config struct {
	Database       sqlhost.Database
	Dialect        modulehost.Dialect
	WorkspaceScope WorkspaceScope
	QueueScopes    QueueScopeIndex
	Clock          notification.Clock
}

type Store struct {
	*inboxPersistence
	*deliveryPersistence
	*eventPersistence
	*templatePersistence
	*lifecyclePersistence
	*migrationPersistence
}

func New(config Config) (*Store, error) {
	if config.Database == nil || config.Dialect == nil || config.WorkspaceScope == nil || config.QueueScopes == nil || config.Clock == nil {
		return nil, ErrIncompleteConfig
	}
	sqlStore := base.NewSQLStore(config.Database, config.Dialect)
	return &Store{
		inboxPersistence: &inboxPersistence{inboxstore.New(inboxstore.Config{
			SQLStore: sqlStore, WorkspaceScope: config.WorkspaceScope, Clock: config.Clock,
		})},
		deliveryPersistence: &deliveryPersistence{deliverystore.New(deliverystore.Config{
			SQLStore: sqlStore, WorkspaceScope: config.WorkspaceScope, QueueScopes: config.QueueScopes,
		})},
		eventPersistence: &eventPersistence{eventstore.New(eventstore.Config{
			SQLStore: sqlStore, WorkspaceScope: config.WorkspaceScope, QueueScopes: config.QueueScopes,
		})},
		templatePersistence:  &templatePersistence{templatestore.New(templatestore.Config{SQLStore: sqlStore, Clock: config.Clock})},
		lifecyclePersistence: &lifecyclePersistence{lifecyclestore.New(lifecyclestore.Config{SQLStore: sqlStore})},
		migrationPersistence: &migrationPersistence{migrationstore.New(migrationstore.Config{SQLStore: sqlStore, WorkspaceScope: config.WorkspaceScope})},
	}, nil
}

// deliveryPersistence gives the composed delivery store a distinct embedding
// name while still promoting its persistence methods through Store.
type deliveryPersistence struct{ *deliverystore.Store }
type inboxPersistence struct{ *inboxstore.Store }
type eventPersistence struct{ *eventstore.Store }
type templatePersistence struct{ *templatestore.Store }
type lifecyclePersistence struct{ *lifecyclestore.Store }
type migrationPersistence struct{ *migrationstore.Store }
