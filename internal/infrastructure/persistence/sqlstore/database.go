package sqlstore

import (
	"context"

	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-orm/sqlhost"
)

type Executor = sqlhost.Executor
type Queryer = sqlhost.Queryer
type Database = sqlhost.Database

// Dialect supplies only SQL construction that differs between the host's
// supported databases. Implementations must quote identifiers defensively.
type Dialect interface {
	Identifier(string) string
	Table(string) string
	Placeholder(int) string
	Insert(string, []string) string
}

// WorkspaceScope attaches the tenant scope required by a host database layer
// such as PostgreSQL RLS. A host without context-based scoping returns ctx.
type WorkspaceScope interface {
	Context(context.Context, notification.WorkspaceID) context.Context
}

// QueueScopeIndex remains host-owned because it is shared by every Runtime
// worker, not notification state. Register must use the supplied executor so it
// can participate in the same transaction as the durable task.
type QueueScopeIndex interface {
	Register(context.Context, Executor, notification.WorkKind, notification.WorkspaceID, string) error
	Workspaces(context.Context, Queryer, notification.WorkKind, int) ([]notification.WorkspaceID, error)
}

type Config struct {
	Database       Database
	Dialect        Dialect
	WorkspaceScope WorkspaceScope
	QueueScopes    QueueScopeIndex
	Clock          notification.Clock
}

type Store struct {
	database       Database
	dialect        Dialect
	workspaceScope WorkspaceScope
	queueScopes    QueueScopeIndex
	clock          notification.Clock
}

func New(config Config) (*Store, error) {
	if config.Database == nil || config.Dialect == nil || config.WorkspaceScope == nil || config.QueueScopes == nil || config.Clock == nil {
		return nil, ErrIncompleteConfig
	}
	return &Store{database: config.Database, dialect: config.Dialect, workspaceScope: config.WorkspaceScope, queueScopes: config.QueueScopes, clock: config.Clock}, nil
}
