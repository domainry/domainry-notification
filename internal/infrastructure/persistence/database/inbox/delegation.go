package inboxstore

import (
	"context"
	"fmt"
	"strings"

	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-orm/query"
)

var _ inbox.DelegationStore = (*Store)(nil)

var delegationColumns = []string{"id", "workspace_id", "owner_user_id", "delegate_user_id", "surface", "starts_at", "ends_at", "enabled", "created_at", "updated_at"}

func (s *Store) ListDelegations(ctx context.Context, workspaceID notification.WorkspaceID, ownerID notification.UserID, surface notification.Surface) ([]inbox.Delegation, error) {
	if workspaceID == "" || ownerID == "" || surface == "" {
		return nil, fmt.Errorf("notification inbox delegation owner identity is required")
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	queryValue, args, err := query.NewWorkspaceSelectBuilder(s.Renderer, "_notification_inbox_delegations", workspaceID.String()).Columns(delegationColumns...).Where(query.And(query.Equal("owner_user_id", ownerID.String()), query.Equal("surface", string(surface)))).OrderBy(query.Ascending("created_at")).Build()
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
	if value.ID == "" || value.WorkspaceID == "" || value.OwnerUserID == "" || value.DelegateUserID == "" || value.Surface == "" {
		return value, fmt.Errorf("notification inbox delegation identity is required")
	}
	ctx = s.workspaceScope.Context(ctx, value.WorkspaceID)
	queryValue, args, err := query.NewWorkspaceUpdateBuilder(s.Renderer, "_notification_inbox_delegations", value.WorkspaceID.String()).Set("delegate_user_id", value.DelegateUserID.String()).Set("starts_at", value.StartsAt).Set("ends_at", value.EndsAt).Set("enabled", value.Enabled).Set("updated_at", value.UpdatedAt).Where(query.And(query.Equal("owner_user_id", value.OwnerUserID.String()), query.Equal("id", value.ID))).Build()
	if err != nil {
		return value, err
	}
	result, err := s.Database.ExecContext(ctx, queryValue, args...)
	if err != nil {
		return value, fmt.Errorf("update notification inbox delegation: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return value, err
	}
	if count == 0 {
		_, err = s.WorkspaceInsert(ctx, s.Database, value.WorkspaceID.String(), "_notification_inbox_delegations", delegationColumns, value.ID, value.WorkspaceID.String(),
			value.OwnerUserID.String(), value.DelegateUserID.String(), string(value.Surface), value.StartsAt, value.EndsAt, value.Enabled, value.CreatedAt, value.UpdatedAt)
		if err != nil {
			return value, fmt.Errorf("insert notification inbox delegation: %w", err)
		}
	}
	return value, nil
}

func (s *Store) DeleteDelegation(ctx context.Context, workspaceID notification.WorkspaceID, ownerID notification.UserID, delegationID string) (bool, error) {
	delegationID = strings.TrimSpace(delegationID)
	if workspaceID == "" || ownerID == "" || delegationID == "" {
		return false, fmt.Errorf("notification inbox delegation identity is required")
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	queryValue, args, err := query.NewWorkspaceDeleteBuilder(s.Renderer, "_notification_inbox_delegations", workspaceID.String()).Where(query.And(query.Equal("owner_user_id", ownerID.String()), query.Equal("id", delegationID))).Build()
	if err != nil {
		return false, err
	}
	result, err := s.Database.ExecContext(ctx, queryValue, args...)
	if err != nil {
		return false, fmt.Errorf("delete notification inbox delegation: %w", err)
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (s *Store) ListActiveDelegatedOwnerIDs(ctx context.Context, workspaceID notification.WorkspaceID, delegateID notification.UserID, surface notification.Surface, now string) ([]notification.UserID, error) {
	if workspaceID == "" || delegateID == "" || surface == "" || strings.TrimSpace(now) == "" {
		return nil, fmt.Errorf("notification active delegation query identity is required")
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	predicate := query.And(query.Equal("delegate_user_id", delegateID.String()), query.Equal("surface", string(surface)), query.Equal("enabled", true), query.Or(query.Equal("starts_at", ""), query.LessThanOrEqual("starts_at", now)), query.Or(query.Equal("ends_at", ""), query.GreaterThan("ends_at", now)))
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

func scanDelegation(row scanner) (inbox.Delegation, error) {
	var value inbox.Delegation
	err := row.Scan(&value.ID, &value.WorkspaceID, &value.OwnerUserID, &value.DelegateUserID, &value.Surface, &value.StartsAt, &value.EndsAt,
		&value.Enabled, &value.CreatedAt, &value.UpdatedAt)
	return value, err
}
