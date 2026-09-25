package inboxstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/domainry/domainry-foundation/mutation"
	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-orm/query"
	"github.com/domainry/domainry-orm/sqlhost"
)

var _ inbox.DelegationStore = (*Store)(nil)

var delegationColumns = []string{"id", "workspace_id", "owner_user_id", "delegate_user_id", "starts_at", "ends_at", "enabled", "created_at", "updated_at"}

func (s *Store) ListDelegations(ctx context.Context, workspaceID notification.WorkspaceID, ownerID notification.UserID) ([]inbox.Delegation, error) {
	if workspaceID == "" || ownerID == "" {
		return nil, fmt.Errorf("notification inbox delegation owner identity is required")
	}
	if err := s.requireWorkspace(workspaceID); err != nil {
		return nil, err
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	predicate := query.Equal("owner_user_id", ownerID.String())
	queryValue, args, err := query.NewWorkspaceSelectBuilder(s.Renderer, "_notification_inbox_delegations", workspaceID.String()).Columns(delegationColumns...).Where(predicate).OrderBy(query.Ascending("created_at")).Build()
	if err != nil {
		return nil, err
	}
	rows, err := s.Database.QueryContext(ctx, queryValue, args...)
	if err != nil {
		return nil, fmt.Errorf("list notification inbox delegations: %w", err)
	}
	defer rows.Close()
	values := []inbox.Delegation{}
	for rows.Next() {
		value, scanErr := scanDelegation(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) SaveDelegation(ctx context.Context, value inbox.Delegation) (inbox.Delegation, error) {
	if value.ID == "" || value.WorkspaceID == "" || value.OwnerUserID == "" || value.DelegateUserID == "" {
		return value, fmt.Errorf("notification inbox delegation identity is required")
	}
	if err := s.requireWorkspace(value.WorkspaceID); err != nil {
		return value, err
	}
	ctx = s.workspaceScope.Context(ctx, value.WorkspaceID)
	tx, err := s.Database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return value, err
	}
	defer tx.Rollback()
	exists, err := s.delegationExists(ctx, tx, value.WorkspaceID, value.OwnerUserID, value.ID)
	if err != nil {
		return value, err
	}
	if !exists {
		_, err = s.WorkspaceInsert(ctx, tx, value.WorkspaceID.String(), "_notification_inbox_delegations", delegationColumns, value.ID, value.WorkspaceID.String(),
			value.OwnerUserID.String(), value.DelegateUserID.String(), notification.TimestampMillis(value.StartsAt), notification.TimestampMillis(value.EndsAt), value.Enabled, notification.TimestampMillis(value.CreatedAt), notification.TimestampMillis(value.UpdatedAt))
		if err != nil {
			return value, fmt.Errorf("insert notification inbox delegation: %w", err)
		}
		return value, tx.Commit()
	}
	predicate := query.And(query.Equal("owner_user_id", value.OwnerUserID.String()), query.Equal("id", value.ID))
	queryValue, args, err := query.NewWorkspaceUpdateBuilder(s.Renderer, "_notification_inbox_delegations", value.WorkspaceID.String()).Set("delegate_user_id", value.DelegateUserID.String()).Set("starts_at", notification.TimestampMillis(value.StartsAt)).Set("ends_at", notification.TimestampMillis(value.EndsAt)).Set("enabled", value.Enabled).Set("updated_at", notification.TimestampMillis(value.UpdatedAt)).Where(predicate).Build()
	if err != nil {
		return value, err
	}
	result, err := tx.ExecContext(ctx, queryValue, args...)
	if err != nil {
		return value, fmt.Errorf("update notification inbox delegation: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return value, err
	}
	if count != 1 {
		return value, mutation.MutationConflict("notification_inbox_delegation", value.ID, mutation.MutationConflictOptimistic, nil)
	}
	return value, tx.Commit()
}

func (s *Store) DeleteDelegation(ctx context.Context, workspaceID notification.WorkspaceID, ownerID notification.UserID, delegationID string) (bool, error) {
	delegationID = strings.TrimSpace(delegationID)
	if workspaceID == "" || ownerID == "" || delegationID == "" {
		return false, fmt.Errorf("notification inbox delegation identity is required")
	}
	if err := s.requireWorkspace(workspaceID); err != nil {
		return false, err
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	tx, err := s.Database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	exists, err := s.delegationExists(ctx, tx, workspaceID, ownerID, delegationID)
	if err != nil || !exists {
		return false, err
	}
	predicate := query.And(query.Equal("owner_user_id", ownerID.String()), query.Equal("id", delegationID))
	queryValue, args, err := query.NewWorkspaceDeleteBuilder(s.Renderer, "_notification_inbox_delegations", workspaceID.String()).Where(predicate).Build()
	if err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx, queryValue, args...)
	if err != nil {
		return false, fmt.Errorf("delete notification inbox delegation: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return false, err
	}
	return true, tx.Commit()
}

func (s *Store) ListActiveDelegatedOwnerIDs(ctx context.Context, workspaceID notification.WorkspaceID, delegateID notification.UserID, now string) ([]notification.UserID, error) {
	if workspaceID == "" || delegateID == "" || strings.TrimSpace(now) == "" {
		return nil, fmt.Errorf("notification active delegation query identity is required")
	}
	if err := s.requireWorkspace(workspaceID); err != nil {
		return nil, err
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	nowMillis := notification.TimestampMillis(now)
	predicate := query.And(query.Equal("delegate_user_id", delegateID.String()), query.Equal("enabled", true), query.Or(query.Equal("starts_at", int64(0)), query.LessThanOrEqual("starts_at", nowMillis)), query.Or(query.Equal("ends_at", int64(0)), query.GreaterThan("ends_at", nowMillis)))
	queryValue, args, err := query.NewWorkspaceSelectBuilder(s.Renderer, "_notification_inbox_delegations", workspaceID.String()).Columns("owner_user_id").Distinct().Where(predicate).OrderBy(query.Ascending("owner_user_id")).Build()
	if err != nil {
		return nil, err
	}
	rows, err := s.Database.QueryContext(ctx, queryValue, args...)
	if err != nil {
		return nil, fmt.Errorf("list active notification inbox delegations: %w", err)
	}
	defer rows.Close()
	values := []notification.UserID{}
	for rows.Next() {
		var owner notification.UserID
		if err := rows.Scan(&owner); err != nil {
			return nil, err
		}
		values = append(values, owner)
	}
	return values, rows.Err()
}

func (s *Store) delegationExists(ctx context.Context, queryer sqlhost.Queryer, workspaceID notification.WorkspaceID, ownerID notification.UserID, delegationID string) (bool, error) {
	predicate := query.And(query.Equal("owner_user_id", ownerID.String()), query.Equal("id", delegationID))
	statement, args, err := query.NewWorkspaceSelectBuilder(s.Renderer, "_notification_inbox_delegations", workspaceID.String()).Columns("id").Where(predicate).Build()
	if err != nil {
		return false, err
	}
	var id string
	err = queryer.QueryRowContext(ctx, statement, args...).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func scanDelegation(row scanner) (inbox.Delegation, error) {
	var value inbox.Delegation
	var startsAt, endsAt, createdAt, updatedAt int64
	err := row.Scan(&value.ID, &value.WorkspaceID, &value.OwnerUserID, &value.DelegateUserID, &startsAt, &endsAt,
		&value.Enabled, &createdAt, &updatedAt)
	value.StartsAt, value.EndsAt = notification.MillisTimestamp(startsAt), notification.MillisTimestamp(endsAt)
	value.CreatedAt, value.UpdatedAt = notification.MillisTimestamp(createdAt), notification.MillisTimestamp(updatedAt)
	return value, err
}
