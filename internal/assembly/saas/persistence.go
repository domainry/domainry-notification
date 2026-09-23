package saas

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	shareddefinition "github.com/domainry/domainry-foundation/definition"
	sharedoperation "github.com/domainry/domainry-foundation/operation"
	metadatasdk "github.com/domainry/domainry-metadata-sdk"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	sqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/artifactkernel"
	storemigration "github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/migration"
	operationstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/operation"
	retentionarchivestore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/retentionarchive"
	"github.com/domainry/domainry-orm/sqlhost"
)

// SQLPersistence owns the standalone Notification SaaS database lifecycle and
// its migration history. Runtime databases must never be passed here.
type SQLPersistence struct {
	database *sql.DB
	driver   sqlstore.Driver
	engine   sqlstore.DatabaseEngine
	schema   string
	owns     bool
	mu       sync.Mutex
}

type SQLPersistenceOptions struct {
	Database     *sql.DB
	Driver       sqlstore.Driver
	Schema       string
	OwnsDatabase bool
}

func NewSQLPersistence(options SQLPersistenceOptions) (*SQLPersistence, error) {
	if options.Database == nil {
		return nil, fmt.Errorf("Notification SaaS database is required")
	}
	engine, err := sqlstore.NewEngine(options.Driver)
	if err != nil {
		return nil, err
	}
	return &SQLPersistence{database: options.Database, driver: options.Driver, engine: engine, schema: strings.TrimSpace(options.Schema), owns: options.OwnsDatabase}, nil
}

func (p *SQLPersistence) Database() *sql.DB {
	if p == nil {
		return nil
	}
	return p.database
}

// PrepareApplication applies immutable migrations and returns the dialect for
// one exact physical application namespace. A process mutex plus a
// database-session advisory lock serialize both same-process and multi-instance
// migration attempts.
func (p *SQLPersistence) PrepareApplication(ctx context.Context, application notificationsdk.ApplicationRef) (modulehost.Dialect, error) {
	if p == nil || p.database == nil {
		return nil, fmt.Errorf("Notification SaaS persistence is unavailable")
	}
	if ctx == nil {
		return nil, fmt.Errorf("Notification SaaS migration context is required")
	}
	if err := application.Validate(); err != nil {
		return nil, err
	}
	const tablePrefix = ""
	dialect, err := p.engine.Renderer(p.schema, tablePrefix)
	if err != nil {
		return nil, err
	}
	migrations, err := sqlstore.ApplicationSchemaMigrations(p.driver, p.schema, tablePrefix, sqlstore.ApplicationScope{
		WorkspaceID: application.WorkspaceID, ApplicationKey: application.ApplicationKey,
	})
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	connection, err := p.database.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("open Notification SaaS migration connection: %w", err)
	}
	defer connection.Close()
	namespace := applicationKey(application)
	// One standalone deployment owns one physical database. Use a database-wide
	// migration lock: application-specific locks would allow two applications to
	// race while both target the same unprefixed tables.
	release, err := p.engine.Acquire(ctx, connection, "notification-saas-schema")
	if err != nil {
		return nil, err
	}
	defer func() { _ = release(context.Background()) }()
	if err := p.ensureLedger(ctx, connection); err != nil {
		return nil, err
	}
	if err := p.ensureApplicationBinding(ctx, connection, namespace); err != nil {
		return nil, err
	}
	for _, migration := range migrations {
		if err := p.applyMigration(ctx, connection, namespace, migration); err != nil {
			return nil, err
		}
	}
	return dialect, nil
}

// PrepareDefinitionStore opens the Foundation Definition kernel over the same
// physical database and migration ledger as Notification. The installation is
// bound to the one exact SaaS application accepted by PrepareApplication.
func (p *SQLPersistence) PrepareDefinitionStore(ctx context.Context, application notificationsdk.ApplicationRef, dialect modulehost.Dialect) (metadatasdk.DefinitionStore, error) {
	if p == nil || p.database == nil || dialect == nil {
		return nil, fmt.Errorf("Notification SaaS shared Definition persistence is unavailable")
	}
	store, err := shareddefinition.Open(ctx, "notification-saas:"+applicationKey(application), p.database, dialect, p.definitionMigrations())
	if err != nil {
		return nil, fmt.Errorf("open Notification SaaS shared Definition store: %w", err)
	}
	return metadatasdk.AdaptDefinitionStore(store), nil
}

// PrepareManagedOperationStore installs the canonical shared Operations table
// for standalone SaaS. Module mode receives this store from the Runtime host.
func (p *SQLPersistence) PrepareManagedOperationStore(ctx context.Context, dialect modulehost.Dialect) (modulehost.ManagedOperationStore, error) {
	store, err := p.prepareOperationStore(ctx, dialect)
	if err != nil {
		return nil, err
	}
	return store, nil
}

func (p *SQLPersistence) PrepareOperationControlStore(ctx context.Context, dialect modulehost.Dialect) (modulehost.OperationControlStore, error) {
	store, err := p.prepareOperationStore(ctx, dialect)
	if err != nil {
		return nil, err
	}
	return store, nil
}

func (p *SQLPersistence) prepareOperationStore(ctx context.Context, dialect modulehost.Dialect) (*operationstore.Store, error) {
	if p == nil || p.database == nil || dialect == nil {
		return nil, fmt.Errorf("Notification SaaS shared Operation persistence is unavailable")
	}
	kernel, err := sharedoperation.Open(ctx, p.database, dialect, p.definitionMigrations())
	if err != nil {
		return nil, err
	}
	return operationstore.New(kernel)
}

func (p *SQLPersistence) PrepareRetentionArchiveStore(ctx context.Context, dialect modulehost.Dialect, content retentionarchivestore.Content) (modulehost.RetentionArchiveStore, error) {
	if p == nil || p.database == nil || dialect == nil || content == nil {
		return nil, fmt.Errorf("Notification SaaS shared Lifecycle archive persistence is unavailable")
	}
	migrations, err := sqlstore.SharedArtifactSchemaMigrations(p.driver, p.schema)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	connection, err := p.database.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("open Notification SaaS shared Lifecycle migration connection: %w", err)
	}
	defer connection.Close()
	release, err := p.engine.Acquire(ctx, connection, "notification-saas-schema")
	if err != nil {
		return nil, err
	}
	defer func() { _ = release(context.Background()) }()
	if err := p.ensureLedger(ctx, connection); err != nil {
		return nil, err
	}
	for _, migration := range migrations {
		if err := p.applyMigration(ctx, connection, "shared/artifacts", migration); err != nil {
			return nil, err
		}
	}
	artifacts, err := artifactkernel.NewStore(p.database, dialect)
	if err != nil {
		return nil, err
	}
	return retentionarchivestore.New(artifacts, content)
}

type definitionMigrations struct{ persistence *SQLPersistence }

func (p *SQLPersistence) definitionMigrations() definitionMigrations {
	return definitionMigrations{persistence: p}
}

func (m definitionMigrations) Driver() string { return string(m.persistence.driver) }
func (m definitionMigrations) Schema() string { return m.persistence.schema }
func (m definitionMigrations) ApplyOwnedMigrations(ctx context.Context, owner string, migrations []shareddefinition.SchemaMigration) error {
	owner = strings.TrimSpace(owner)
	if owner != shareddefinition.MigrationOwner && owner != sharedoperation.MigrationOwner {
		return fmt.Errorf("Notification SaaS cannot install shared migration owner %q", owner)
	}
	p := m.persistence
	p.mu.Lock()
	defer p.mu.Unlock()
	connection, err := p.database.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open Notification SaaS shared migration connection: %w", err)
	}
	defer connection.Close()
	release, err := p.engine.Acquire(ctx, connection, "notification-saas-schema")
	if err != nil {
		return err
	}
	defer func() { _ = release(context.Background()) }()
	if err := p.ensureLedger(ctx, connection); err != nil {
		return err
	}
	for _, migration := range migrations {
		if err := p.applyMigration(ctx, connection, owner, migration); err != nil {
			return err
		}
	}
	return nil
}

func (p *SQLPersistence) ensureApplicationBinding(ctx context.Context, connection migrationQueryer, namespace string) error {
	renderer, err := p.engine.Renderer(p.schema, "")
	if err != nil {
		return err
	}
	return storemigration.NewLedger(renderer).ValidateApplicationBinding(ctx, connection, namespace)
}

func (p *SQLPersistence) Close() error {
	if p == nil || !p.owns || p.database == nil {
		return nil
	}
	err := p.database.Close()
	p.database = nil
	return err
}

func (p *SQLPersistence) ensureLedger(ctx context.Context, connection sqlhost.Database) error {
	renderer, err := p.engine.Renderer(p.schema, "")
	if err != nil {
		return err
	}
	return storemigration.NewLedger(renderer).Ensure(ctx, connection)
}

func (p *SQLPersistence) applyMigration(ctx context.Context, connection sqlhost.Database, namespace string, migration sqlstore.SchemaMigration) error {
	checksum := migrationChecksum(migration)
	existing, dirty, found, err := p.migrationState(ctx, connection, namespace, migration.Version)
	if err != nil {
		return err
	}
	if found {
		if dirty {
			return fmt.Errorf("Notification SaaS migration %s/%d is dirty", namespace, migration.Version)
		}
		if existing != checksum {
			return fmt.Errorf("Notification SaaS migration %s/%d checksum mismatch", namespace, migration.Version)
		}
		return nil
	}
	renderer, err := p.engine.Renderer(p.schema, "")
	if err != nil {
		return err
	}
	ledger := storemigration.NewLedger(renderer)
	if err := ledger.RecordDirty(ctx, connection, namespace, migration.Version, strings.TrimSpace(migration.Name), checksum); err != nil {
		return fmt.Errorf("record dirty Notification SaaS migration %s/%d: %w", namespace, migration.Version, err)
	}
	tx, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Notification SaaS migration %s/%d: %w", namespace, migration.Version, err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, statement := range migration.Statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply Notification SaaS migration %s/%d (%s): %w", namespace, migration.Version, migration.Name, err)
		}
	}
	if err := ledger.Complete(ctx, tx, namespace, migration.Version, checksum, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("record Notification SaaS migration %s/%d: %w", namespace, migration.Version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Notification SaaS migration %s/%d: %w", namespace, migration.Version, err)
	}
	return nil
}

type migrationQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (p *SQLPersistence) migrationState(ctx context.Context, queryer migrationQueryer, namespace string, version uint) (string, bool, bool, error) {
	renderer, err := p.engine.Renderer(p.schema, "")
	if err != nil {
		return "", false, false, err
	}
	return storemigration.NewLedger(renderer).State(ctx, queryer, namespace, version)
}

func migrationChecksum(migration sqlstore.SchemaMigration) string {
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "%d\x00%s\x00", migration.Version, migration.Name)
	for _, statement := range migration.Statements {
		_, _ = hash.Write([]byte(statement))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
