package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	ormdialect "github.com/domainry/domainry-orm/dialect"
	"github.com/domainry/domainry-orm/query"
	ormschema "github.com/domainry/domainry-orm/schema"
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
		ormschema.Column("name", ormschema.TextKey(191)).NotNull(),
		ormschema.Column("checksum", ormschema.TextKey(64)).NotNull(),
		ormschema.Column("dirty", ormschema.Boolean()).NotNull(),
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
	statement, args, err := query.NewSelectBuilder(l.renderer, LedgerTable).Columns("namespace").Where(query.NotLike("namespace", "shared/%")).OrderBy(query.Ascending("namespace")).Limit(1).Build()
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

func (l Ledger) State(ctx context.Context, queryer Queryer, namespace string, version uint) (string, bool, bool, error) {
	statement, args, err := query.NewSelectBuilder(l.renderer, LedgerTable).Columns("checksum", "dirty").Where(query.And(
		query.Equal("namespace", namespace),
		query.Equal("version", version),
	)).Build()
	if err != nil {
		return "", false, false, fmt.Errorf("build Notification SaaS migration ledger query: %w", err)
	}
	var checksum string
	var dirty bool
	if err := queryer.QueryRowContext(ctx, statement, args...).Scan(&checksum, &dirty); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, false, nil
		}
		return "", false, false, fmt.Errorf("read Notification SaaS migration ledger: %w", err)
	}
	return checksum, dirty, true, nil
}

func (l Ledger) RecordDirty(ctx context.Context, executor Executor, namespace string, version uint, name, checksum string) error {
	statement, args, err := query.NewInsertBuilder(l.renderer, LedgerTable).
		Columns("namespace", "version", "name", "checksum", "dirty", "applied_at").
		Values(namespace, version, name, checksum, true, "").Build()
	if err != nil {
		return fmt.Errorf("build Notification SaaS migration ledger insert: %w", err)
	}
	if _, err := executor.ExecContext(ctx, statement, args...); err != nil {
		return fmt.Errorf("record Notification SaaS migration ledger: %w", err)
	}
	return nil
}

func (l Ledger) Complete(ctx context.Context, executor Executor, namespace string, version uint, checksum, appliedAt string) error {
	statement, args, err := query.NewUpdateBuilder(l.renderer, LedgerTable).
		Set("dirty", false).Set("applied_at", appliedAt).
		Where(query.And(
			query.Equal("namespace", namespace), query.Equal("version", version),
			query.Equal("checksum", checksum), query.Equal("dirty", true),
		)).Build()
	if err != nil {
		return fmt.Errorf("build Notification SaaS migration completion: %w", err)
	}
	result, err := executor.ExecContext(ctx, statement, args...)
	if err != nil {
		return fmt.Errorf("complete Notification SaaS migration ledger: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return fmt.Errorf("complete Notification SaaS migration ledger: affected=%d err=%v", affected, err)
	}
	return nil
}
