package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	ormschema "github.com/domainry/domainry-orm/schema"

	ormdialect "github.com/domainry/domainry-orm/dialect"
	ormbuilder "github.com/domainry/domainry-orm/query"
)

const LedgerTable = "_schema_migrations"

type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type Queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Ledger struct {
	renderer ormdialect.Renderer
}

func NewLedger(renderer ormdialect.Renderer) Ledger {
	return Ledger{renderer: renderer}
}

func (l Ledger) Ensure(ctx context.Context, executor Executor) error {
	statement, args, err := ormschema.NewTable(l.renderer, LedgerTable).IfNotExists().Columns(
		ormschema.Column("namespace", ormschema.TextKey(512)).NotNull(),
		ormschema.Column("version", ormschema.BigInt()).NotNull(),
		ormschema.Column("checksum", ormschema.TextKey(64)).NotNull(),
		ormschema.Column("applied_at", ormschema.TextKey(64)).NotNull(),
	).PrimaryKey("namespace", "version").Build()
	if err != nil {
		return fmt.Errorf("build Notification SaaS migration ledger: %w", err)
	}
	if _, err := executor.ExecContext(ctx, statement, args...); err != nil {
		return fmt.Errorf("create Notification SaaS migration ledger: %w", err)
	}
	return nil
}

func (l Ledger) ValidateApplicationBinding(ctx context.Context, queryer Queryer, namespace string) error {
	statement, args, err := ormbuilder.NewSelectBuilder(l.renderer, LedgerTable).Columns("namespace").OrderBy(ormbuilder.Ascending("namespace")).Limit(1).Build()
	if err != nil {
		return fmt.Errorf("build Notification SaaS application binding query: %w", err)
	}
	var existing string
	if err := queryer.QueryRowContext(ctx, statement, args...).Scan(&existing); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("read Notification SaaS application binding: %w", err)
	}
	if existing != namespace {
		return fmt.Errorf("Notification SaaS database is already bound to application %s", existing)
	}
	return nil
}

func (l Ledger) Checksum(ctx context.Context, queryer Queryer, namespace string, version uint) (string, bool, error) {
	statement, args, err := ormbuilder.NewSelectBuilder(l.renderer, LedgerTable).Columns("checksum").Where(ormbuilder.And(
		ormbuilder.Equal("namespace", namespace),
		ormbuilder.Equal("version", version),
	)).Build()
	if err != nil {
		return "", false, fmt.Errorf("build Notification SaaS migration ledger query: %w", err)
	}
	var checksum string
	if err := queryer.QueryRowContext(ctx, statement, args...).Scan(&checksum); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read Notification SaaS migration ledger: %w", err)
	}
	return checksum, true, nil
}

func (l Ledger) Record(ctx context.Context, executor Executor, namespace string, version uint, checksum, appliedAt string) error {
	statement, args, err := ormbuilder.NewInsertBuilder(l.renderer, LedgerTable).Columns("namespace", "version", "checksum", "applied_at").Values(namespace, version, checksum, appliedAt).Build()
	if err != nil {
		return fmt.Errorf("build Notification SaaS migration ledger insert: %w", err)
	}
	if _, err := executor.ExecContext(ctx, statement, args...); err != nil {
		return fmt.Errorf("record Notification SaaS migration ledger: %w", err)
	}
	return nil
}
