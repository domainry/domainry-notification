package eventstore

import (
	"context"

	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/shared"
	"github.com/domainry/domainry-orm/sqlhost"
)

type WorkspaceScope interface {
	Context(context.Context, notification.WorkspaceID) context.Context
}
type QueueScopeIndex interface {
	Register(context.Context, sqlhost.Executor, notification.WorkKind, notification.WorkspaceID, string) error
	Workspaces(context.Context, sqlhost.Queryer, notification.WorkKind, int) ([]notification.WorkspaceID, error)
}
type Config struct {
	SQLStore       *base.SQLStore
	WorkspaceScope WorkspaceScope
	QueueScopes    QueueScopeIndex
}
type Store struct {
	*base.SQLStore
	workspaceScope WorkspaceScope
	queueScopes    QueueScopeIndex
}

var (
	ErrLeaseLost           = shared.ErrLeaseLost
	ErrIdempotencyConflict = shared.ErrIdempotencyConflict
)

func New(config Config) *Store {
	return &Store{SQLStore: config.SQLStore, workspaceScope: config.WorkspaceScope, queueScopes: config.QueueScopes}
}
