package deliverystore_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	sqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence"
	"github.com/domainry/domainry-orm/sqlhost"
)

type passthroughScope struct{}

func (passthroughScope) Context(ctx context.Context, _ notification.WorkspaceID) context.Context {
	return ctx
}

type storeClock struct{ value time.Time }

func (c storeClock) Now() time.Time { return c.value }

type queueScopes struct{}

func (*queueScopes) Register(context.Context, sqlhost.Executor, notification.WorkKind, notification.WorkspaceID, string) error {
	return nil
}

func (*queueScopes) Workspaces(context.Context, sqlhost.Queryer, notification.WorkKind, int) ([]notification.WorkspaceID, error) {
	return nil, nil
}

func materializationStore(t *testing.T) (*sql.DB, *sqlstore.Store, *queueScopes) {
	t.Helper()
	database, store := migratedStore(t)
	return database, store, &queueScopes{}
}
