package testkit

import (
	"context"
	"database/sql"
	"testing"
	"time"

	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/sqlstore"
	ormdialect "github.com/domainry/domainry-orm/dialect"
	"github.com/domainry/domainry-orm/sqlhost"
	_ "modernc.org/sqlite"
)

type scope struct{}

func (scope) Context(ctx context.Context, _ notification.WorkspaceID) context.Context { return ctx }

type clock struct{ value time.Time }

func (c clock) Now() time.Time { return c.value }

type queueScopes struct{}

func (*queueScopes) Register(context.Context, sqlhost.Executor, notification.WorkKind, notification.WorkspaceID, string) error {
	return nil
}
func (*queueScopes) Workspaces(context.Context, sqlhost.Queryer, notification.WorkKind, int) ([]notification.WorkspaceID, error) {
	return nil, nil
}

func OpenMigrated(t *testing.T) (*sql.DB, *sqlstore.Store) {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	migrations, err := sqlstore.SchemaMigrations(sqlstore.SQLite, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		for _, statement := range migration.Statements {
			if _, err := database.Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	dialect, _ := ormdialect.ParseRenderer("sqlite", "", "")
	store, err := sqlstore.New(sqlstore.Config{Database: database, Dialect: dialect, WorkspaceScope: scope{}, QueueScopes: &queueScopes{}, Clock: clock{value: time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC)}})
	if err != nil {
		t.Fatal(err)
	}
	return database, store
}
