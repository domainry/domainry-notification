package saas

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	sqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"
	ormdialect "github.com/domainry/domainry-orm/dialect"
)

const migrationLedgerTable = "_schema_migrations"

// SQLPersistence owns the standalone Notification SaaS database lifecycle and
// its migration history. Runtime databases must never be passed here.
type SQLPersistence struct {
	database *sql.DB
	driver   sqlstore.Driver
	locker   base.MigrationLocker
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
	if _, err := ormdialect.ParseRenderer(string(options.Driver), options.Schema, ""); err != nil {
		return nil, err
	}
	locker, err := sqlstore.MigrationLocker(options.Driver)
	if err != nil {
		return nil, err
	}
	return &SQLPersistence{database: options.Database, driver: options.Driver, locker: locker, schema: strings.TrimSpace(options.Schema), owns: options.OwnsDatabase}, nil
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
	prefix := applicationTablePrefix(application)
	dialect, err := ormdialect.ParseRenderer(string(p.driver), p.schema, prefix)
	if err != nil {
		return nil, err
	}
	migrations, err := sqlstore.ApplicationSchemaMigrations(p.driver, p.schema, prefix, sqlstore.ApplicationScope{
		TenantID: application.TenantID, WorkspaceID: application.WorkspaceID, ApplicationKey: application.ApplicationKey,
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
	release, err := p.locker.Acquire(ctx, connection, namespace)
	if err != nil {
		return nil, err
	}
	defer func() { _ = release(context.Background()) }()
	if err := p.ensureLedger(ctx, connection); err != nil {
		return nil, err
	}
	for _, migration := range migrations {
		if err := p.applyMigration(ctx, connection, namespace, migration); err != nil {
			return nil, err
		}
	}
	return dialect, nil
}

func (p *SQLPersistence) Close() error {
	if p == nil || !p.owns || p.database == nil {
		return nil
	}
	err := p.database.Close()
	p.database = nil
	return err
}

type migrationConnection interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

func (p *SQLPersistence) ensureLedger(ctx context.Context, connection migrationConnection) error {
	dialect, err := ormdialect.ParseRenderer(string(p.driver), p.schema, "")
	if err != nil {
		return err
	}
	statement := "CREATE TABLE IF NOT EXISTS " + dialect.Table(migrationLedgerTable) + " (" +
		dialect.Identifier("namespace") + " VARCHAR(512) NOT NULL, " +
		dialect.Identifier("version") + " BIGINT NOT NULL, " +
		dialect.Identifier("checksum") + " VARCHAR(64) NOT NULL, " +
		dialect.Identifier("applied_at") + " VARCHAR(64) NOT NULL, PRIMARY KEY (" + dialect.Identifier("namespace") + ", " + dialect.Identifier("version") + "))"
	if _, err := connection.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("create Notification SaaS migration ledger: %w", err)
	}
	return nil
}

func (p *SQLPersistence) applyMigration(ctx context.Context, connection migrationConnection, namespace string, migration sqlstore.SchemaMigration) error {
	checksum := migrationChecksum(migration)
	existing, found, err := p.migrationChecksum(ctx, connection, namespace, migration.Version)
	if err != nil {
		return err
	}
	if found {
		if existing != checksum {
			return fmt.Errorf("Notification SaaS migration %s/%d checksum mismatch", namespace, migration.Version)
		}
		return nil
	}
	tx, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Notification SaaS migration %s/%d: %w", namespace, migration.Version, err)
	}
	defer func() { _ = tx.Rollback() }()
	// Recheck inside the transaction so repeated opens never replay DDL.
	existing, found, err = p.migrationChecksum(ctx, tx, namespace, migration.Version)
	if err != nil {
		return err
	}
	if found {
		if existing != checksum {
			return fmt.Errorf("Notification SaaS migration %s/%d checksum mismatch", namespace, migration.Version)
		}
		return tx.Commit()
	}
	for _, statement := range migration.Statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply Notification SaaS migration %s/%d (%s): %w", namespace, migration.Version, migration.Name, err)
		}
	}
	dialect, err := ormdialect.ParseRenderer(string(p.driver), p.schema, "")
	if err != nil {
		return err
	}
	insert := dialect.Insert(migrationLedgerTable, []string{"namespace", "version", "checksum", "applied_at"})
	if _, err := tx.ExecContext(ctx, insert, namespace, migration.Version, checksum, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
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

func (p *SQLPersistence) migrationChecksum(ctx context.Context, queryer migrationQueryer, namespace string, version uint) (string, bool, error) {
	dialect, err := ormdialect.ParseRenderer(string(p.driver), p.schema, "")
	if err != nil {
		return "", false, err
	}
	query := "SELECT " + dialect.Identifier("checksum") + " FROM " + dialect.Table(migrationLedgerTable) + " WHERE " + dialect.Identifier("namespace") + " = " + dialect.Placeholder(1) + " AND " + dialect.Identifier("version") + " = " + dialect.Placeholder(2)
	var checksum string
	if err := queryer.QueryRowContext(ctx, query, namespace, version).Scan(&checksum); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read Notification SaaS migration ledger: %w", err)
	}
	return checksum, true, nil
}

func applicationTablePrefix(application notificationsdk.ApplicationRef) string {
	sum := sha256.Sum256([]byte(applicationKey(application)))
	return "notification_" + hex.EncodeToString(sum[:8]) + "_"
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
