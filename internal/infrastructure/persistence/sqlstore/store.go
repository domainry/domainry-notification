package sqlstore

import (
	"context"

	"github.com/domainry/domainry-notification-sdk/modulehost"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	inboxstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/sqlstore/inbox"
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
	*inboxstore.Store
	database       sqlhost.Database
	dialect        modulehost.Dialect
	workspaceScope WorkspaceScope
	queueScopes    QueueScopeIndex
	clock          notification.Clock
}

func New(config Config) (*Store, error) {
	if config.Database == nil || config.Dialect == nil || config.WorkspaceScope == nil || config.QueueScopes == nil || config.Clock == nil {
		return nil, ErrIncompleteConfig
	}
	return &Store{
		Store: inboxstore.New(inboxstore.Config{
			Database: config.Database, Dialect: config.Dialect, WorkspaceScope: config.WorkspaceScope, Clock: config.Clock,
		}),
		database: config.Database, dialect: config.Dialect, workspaceScope: config.WorkspaceScope, queueScopes: config.QueueScopes, clock: config.Clock,
	}, nil
}
