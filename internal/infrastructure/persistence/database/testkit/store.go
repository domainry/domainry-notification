package testkit

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	shareddefinition "github.com/domainry/domainry-foundation/definition"
	metadatasdk "github.com/domainry/domainry-metadata-sdk"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	sqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/artifactkernel"
	operationstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/operation"
	retentionarchivestore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/retentionarchive"
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

type metadataMigrationRegistrar struct{ database *sql.DB }

func (*metadataMigrationRegistrar) Driver() string { return "sqlite" }
func (*metadataMigrationRegistrar) Schema() string { return "" }
func (r *metadataMigrationRegistrar) ApplyOwnedMigrations(ctx context.Context, owner string, migrations []shareddefinition.SchemaMigration) error {
	if owner != shareddefinition.MigrationOwner {
		return fmt.Errorf("unexpected test migration owner %q", owner)
	}
	for _, migration := range migrations {
		for _, statement := range migration.Statements {
			if _, err := r.database.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
	}
	return nil
}

func OpenMigrated(t *testing.T) (*sql.DB, *sqlstore.Store) {
	database, store, _ := OpenMigratedWithArtifactContent(t)
	return database, store
}

func OpenMigratedWithArtifactContent(t *testing.T) (*sql.DB, *sqlstore.Store, *artifactkernel.ContentFiles) {
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
	operationMigrations, err := sqlstore.SharedOperationSchemaMigrations(sqlstore.SQLite, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range operationMigrations {
		for _, statement := range migration.Statements {
			if _, err := database.Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	operations, err := operationstore.New(base.NewSQLStore(database, dialect))
	if err != nil {
		t.Fatal(err)
	}
	archiveMigrations, err := sqlstore.SharedArtifactSchemaMigrations(sqlstore.SQLite, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range archiveMigrations {
		for _, statement := range migration.Statements {
			if _, err := database.Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	artifactStore, err := artifactkernel.NewStore(database, dialect)
	if err != nil {
		t.Fatal(err)
	}
	artifactContent, err := artifactkernel.NewContentFiles(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = artifactContent.Close() })
	archives, err := retentionarchivestore.New(artifactStore, artifactContent)
	if err != nil {
		t.Fatal(err)
	}
	definitionKernel, err := shareddefinition.Open(t.Context(), "notification-test:"+t.Name(), database, dialect, &metadataMigrationRegistrar{database: database})
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlstore.New(sqlstore.Config{
		Database: database, Dialect: dialect, WorkspaceScope: scope{}, QueueScopes: &queueScopes{},
		Clock: clock{value: time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC)}, WorkspaceID: "workspace-1",
		DefinitionStore: metadatasdk.AdaptDefinitionStore(definitionKernel), OperationStore: operations, ControlStore: operations, ArchiveStore: archives,
	})
	if err != nil {
		t.Fatal(err)
	}
	return database, store, artifactContent
}
