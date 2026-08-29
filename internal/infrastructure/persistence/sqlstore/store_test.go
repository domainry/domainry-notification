package sqlstore_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/sqlstore"
	ormdialect "github.com/domainry/domainry-orm/dialect"
	_ "modernc.org/sqlite"
)

func migratedStore(t *testing.T) (*sql.DB, *sqlstore.Store) {
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
	store, err := sqlstore.New(sqlstore.Config{
		Database: database, Dialect: dialect, WorkspaceScope: passthroughScope{}, QueueScopes: &queueScopes{},
		Clock: storeClock{value: time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return database, store
}
