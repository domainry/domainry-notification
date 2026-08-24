package sqlstore_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/domainry/domainry-notification"
	"github.com/domainry/domainry-notification/inbox"
	"github.com/domainry/domainry-notification/sqlstore"
)

type fakeResult struct{}

func (fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeResult) RowsAffected() (int64, error) { return 1, nil }

type fakeDatabase struct {
	query string
	args  []any
	scope string
}

func (d *fakeDatabase) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	d.query, d.args = query, append([]any(nil), args...)
	d.scope, _ = ctx.Value(scopeContextKey{}).(string)
	return fakeResult{}, nil
}
func (*fakeDatabase) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, fmt.Errorf("unexpected query")
}
func (*fakeDatabase) QueryRowContext(context.Context, string, ...any) *sql.Row { return nil }
func (*fakeDatabase) BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error) {
	return nil, fmt.Errorf("unexpected transaction")
}

type fakeDialect struct{}

func (fakeDialect) Identifier(value string) string { return `"` + value + `"` }
func (fakeDialect) Table(value string) string      { return `"` + value + `"` }
func (fakeDialect) Placeholder(int) string         { return "?" }
func (fakeDialect) Insert(table string, columns []string) string {
	return "INSERT INTO " + table + " (" + strings.Join(columns, ",") + ") VALUES (" + strings.TrimSuffix(strings.Repeat("?,", len(columns)), ",") + ")"
}

type scopeContextKey struct{}
type fakeWorkspaceScope struct{}

func (fakeWorkspaceScope) Context(ctx context.Context, workspace notification.WorkspaceID) context.Context {
	return context.WithValue(ctx, scopeContextKey{}, workspace.String())
}

type fakeQueueScopes struct {
	registered bool
	executor   sqlstore.Executor
}

func (s *fakeQueueScopes) Register(_ context.Context, executor sqlstore.Executor, kind notification.WorkKind, workspace notification.WorkspaceID, _ string) error {
	s.registered, s.executor = kind == notification.WorkInboxEvent && workspace == "workspace-1", executor
	return nil
}
func (*fakeQueueScopes) Workspaces(context.Context, sqlstore.Queryer, notification.WorkKind, int) ([]notification.WorkspaceID, error) {
	return nil, nil
}

func TestInsertEventUsesCallerExecutorAndHostScopeAdapters(t *testing.T) {
	database, scopes := &fakeDatabase{}, &fakeQueueScopes{}
	store, err := sqlstore.New(sqlstore.Config{Database: database, Dialect: fakeDialect{}, WorkspaceScope: fakeWorkspaceScope{}, QueueScopes: scopes, Clock: storeClock{}})
	if err != nil {
		t.Fatal(err)
	}
	event := inbox.Event{ID: "event-1", WorkspaceID: "workspace-1", Source: "workflow", SourceEventID: "source-1", Status: inbox.EventQueued, UpdatedAt: "now"}
	if err := store.InsertEvent(t.Context(), database, event); err != nil {
		t.Fatal(err)
	}
	if !scopes.registered || scopes.executor != database {
		t.Fatal("queue scope was not registered through the caller executor")
	}
	if database.scope != "workspace-1" || !strings.HasPrefix(database.query, "INSERT INTO notification_events") {
		t.Fatalf("scope=%q query=%q", database.scope, database.query)
	}
	if len(database.args) != 15 || database.args[0] != "event-1" || database.args[1] != "workspace-1" {
		t.Fatalf("args=%v", database.args)
	}
}

func TestStoreRequiresEveryHostDatabaseBoundary(t *testing.T) {
	if _, err := sqlstore.New(sqlstore.Config{}); err != sqlstore.ErrIncompleteConfig {
		t.Fatalf("error=%v", err)
	}
}
