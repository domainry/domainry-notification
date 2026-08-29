// Package base contains the database primitives shared by notification stores.
// It deliberately has no notification business semantics and does not select a
// database driver; the assembly layer injects the renderer chosen by the host.
package base

import (
	"context"
	"database/sql"
	"strings"

	"github.com/domainry/domainry-notification-sdk/modulehost"
	"github.com/domainry/domainry-orm/builder"
	"github.com/domainry/domainry-orm/sqlhost"
)

// SQLStore is the single database dependency used by notification persistence.
// Database-specific behavior is supplied through Renderer by the composition
// root, so business stores never inspect a driver name.
type SQLStore struct {
	Database sqlhost.Database
	Renderer modulehost.Dialect
}

func NewSQLStore(database sqlhost.Database, renderer modulehost.Dialect) *SQLStore {
	return &SQLStore{Database: database, Renderer: renderer}
}

func (s *SQLStore) Columns(columns []string) string {
	quoted := make([]string, len(columns))
	for index, column := range columns {
		quoted[index] = s.Renderer.Identifier(column)
	}
	return strings.Join(quoted, ", ")
}

// Insert builds and executes one portable INSERT statement. Business stores
// provide only table/column/value semantics; placeholder and identifier rules
// remain owned by the ORM renderer.
func (s *SQLStore) Insert(ctx context.Context, executor sqlhost.Executor, table string, columns []string, values ...any) (sql.Result, error) {
	statement, arguments, err := builder.NewInsertBuilder(s.Renderer, table).Columns(columns...).Values(values...).Build()
	if err != nil {
		return nil, err
	}
	return executor.ExecContext(ctx, statement, arguments...)
}
