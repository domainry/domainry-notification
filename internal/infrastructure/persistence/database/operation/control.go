package operationstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/domainry/domainry-notification-sdk/modulehost"
	"github.com/domainry/domainry-orm/query"
)

const controlTableName = "_operation_controls"

var controlColumns = []string{
	"system_purpose", "control_kind", "owner", "state", "reason", "reference", "updated_by", "revision", "updated_at",
}

func (s *Store) GetOperationControl(ctx context.Context, purpose, kind, owner string) (modulehost.OperationControl, bool, error) {
	purpose, kind, owner = strings.TrimSpace(purpose), strings.TrimSpace(kind), strings.TrimSpace(owner)
	if s == nil || purpose == "" || kind == "" || owner == "" {
		return modulehost.OperationControl{}, false, fmt.Errorf("operation control identity is required")
	}
	statement, args, err := query.NewSelectBuilder(s.Renderer, controlTableName).
		Columns(controlColumns...).
		Where(query.And(query.Equal("system_purpose", purpose), query.Equal("control_kind", kind), query.Equal("owner", owner))).
		Build()
	if err != nil {
		return modulehost.OperationControl{}, false, err
	}
	value, err := scanControl(s.Database.QueryRowContext(ctx, statement, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return modulehost.OperationControl{}, false, nil
	}
	return value, err == nil, err
}

func (s *Store) PutOperationControl(ctx context.Context, value modulehost.OperationControl, expectedRevision int64) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("operation control store is unavailable")
	}
	if err := value.Validate(); err != nil {
		return false, err
	}
	if expectedRevision < 0 || value.Revision != expectedRevision+1 {
		return false, fmt.Errorf("operation control revision is invalid")
	}
	if expectedRevision == 0 {
		statement, args, err := query.NewInsertBuilder(s.Renderer, controlTableName).
			Columns(controlColumns...).
			Values(controlValues(value)...).
			Build()
		if err != nil {
			return false, err
		}
		if _, err := s.Database.ExecContext(ctx, statement, args...); err != nil {
			if _, found, readErr := s.GetOperationControl(ctx, value.SystemPurpose, value.Kind, value.Owner); readErr == nil && found {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}
	statement, args, err := query.NewUpdateBuilder(s.Renderer, controlTableName).
		Set("state", strings.TrimSpace(value.State)).
		Set("reason", value.Reason).
		Set("reference", value.Reference).
		Set("updated_by", strings.TrimSpace(value.UpdatedBy)).
		Set("revision", value.Revision).
		Set("updated_at", value.UpdatedAt.UTC().Format(time.RFC3339Nano)).
		Where(query.And(
			query.Equal("system_purpose", strings.TrimSpace(value.SystemPurpose)),
			query.Equal("control_kind", strings.TrimSpace(value.Kind)),
			query.Equal("owner", strings.TrimSpace(value.Owner)),
			query.Equal("revision", expectedRevision),
		)).Build()
	if err != nil {
		return false, err
	}
	result, err := s.Database.ExecContext(ctx, statement, args...)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

func controlValues(value modulehost.OperationControl) []any {
	return []any{
		strings.TrimSpace(value.SystemPurpose), strings.TrimSpace(value.Kind), strings.TrimSpace(value.Owner), strings.TrimSpace(value.State),
		value.Reason, value.Reference, strings.TrimSpace(value.UpdatedBy), value.Revision, value.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func scanControl(row scanner) (modulehost.OperationControl, error) {
	var value modulehost.OperationControl
	var updatedAt string
	err := row.Scan(&value.SystemPurpose, &value.Kind, &value.Owner, &value.State, &value.Reason, &value.Reference, &value.UpdatedBy, &value.Revision, &updatedAt)
	if err != nil {
		return value, err
	}
	value.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	return value, err
}

var _ modulehost.OperationControlStore = (*Store)(nil)
