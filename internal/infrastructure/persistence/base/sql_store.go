package base

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/domainry/domainry-notification-sdk/modulehost"
	"github.com/domainry/domainry-orm/query"
	"github.com/domainry/domainry-orm/sqlhost"
)

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

func (s *SQLStore) Insert(ctx context.Context, executor sqlhost.Executor, table string, columns []string, values ...any) (sql.Result, error) {
	if err := validateTimestampValues(columns, values); err != nil {
		return nil, err
	}
	statement, arguments, err := query.NewInsertBuilder(s.Renderer, table).Columns(columns...).Values(values...).Build()
	if err != nil {
		return nil, err
	}
	return executor.ExecContext(ctx, statement, arguments...)
}

func (s *SQLStore) WorkspaceInsert(ctx context.Context, executor sqlhost.Executor, workspaceID, table string, columns []string, values ...any) (sql.Result, error) {
	if err := validateTimestampValues(columns, values); err != nil {
		return nil, err
	}
	workspaceColumn := -1
	for index, column := range columns {
		if strings.EqualFold(strings.TrimSpace(column), query.WorkspaceIDColumn) {
			workspaceColumn = index
			break
		}
	}
	if workspaceColumn < 0 || len(values) != len(columns) || strings.TrimSpace(fmt.Sprint(values[workspaceColumn])) != strings.TrimSpace(workspaceID) {
		return nil, fmt.Errorf("notification workspace insert has inconsistent workspace_id")
	}
	ownedColumns := append([]string(nil), columns[:workspaceColumn]...)
	ownedColumns = append(ownedColumns, columns[workspaceColumn+1:]...)
	ownedValues := append([]any(nil), values[:workspaceColumn]...)
	ownedValues = append(ownedValues, values[workspaceColumn+1:]...)
	statement, arguments, err := query.NewWorkspaceInsertBuilder(s.Renderer, table, workspaceID).Columns(ownedColumns...).Values(ownedValues...).Build()
	if err != nil {
		return nil, err
	}
	return executor.ExecContext(ctx, statement, arguments...)
}

func validateTimestampValues(columns []string, values []any) error {
	for index, column := range columns {
		if index >= len(values) || !strings.HasSuffix(strings.ToLower(strings.TrimSpace(column)), "_at") || values[index] == nil {
			continue
		}
		if _, ok := values[index].(int64); !ok {
			return fmt.Errorf("notification timestamp column %s requires Unix milliseconds", column)
		}
	}
	return nil
}
