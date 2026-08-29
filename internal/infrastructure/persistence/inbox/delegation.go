package inboxstore

import (
	"context"
	"fmt"
	"strings"

	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

var _ inbox.DelegationStore = (*Store)(nil)

var delegationColumns = []string{"id", "workspace_id", "owner_user_id", "delegate_user_id", "surface", "starts_at", "ends_at", "enabled", "created_at", "updated_at"}

func (s *Store) ListDelegations(ctx context.Context, workspaceID notification.WorkspaceID, ownerID notification.UserID, surface notification.Surface) ([]inbox.Delegation, error) {
	if workspaceID == "" || ownerID == "" || surface == "" {
		return nil, fmt.Errorf("notification inbox delegation owner identity is required")
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	query := "SELECT " + s.columns(delegationColumns) + " FROM " + s.dialect.Table("notification_inbox_delegations") + " WHERE " +
		s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(1) + " AND " + s.dialect.Identifier("owner_user_id") + " = " + s.dialect.Placeholder(2) +
		" AND " + s.dialect.Identifier("surface") + " = " + s.dialect.Placeholder(3) + " ORDER BY " + s.dialect.Identifier("created_at") + " ASC"
	rows, err := s.database.QueryContext(ctx, query, workspaceID.String(), ownerID.String(), string(surface))
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
	query := "UPDATE " + s.dialect.Table("notification_inbox_delegations") + " SET " + s.dialect.Identifier("delegate_user_id") + " = " + s.dialect.Placeholder(1) +
		", " + s.dialect.Identifier("starts_at") + " = " + s.dialect.Placeholder(2) + ", " + s.dialect.Identifier("ends_at") + " = " + s.dialect.Placeholder(3) +
		", " + s.dialect.Identifier("enabled") + " = " + s.dialect.Placeholder(4) + ", " + s.dialect.Identifier("updated_at") + " = " + s.dialect.Placeholder(5) +
		" WHERE " + s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(6) + " AND " + s.dialect.Identifier("owner_user_id") + " = " + s.dialect.Placeholder(7) +
		" AND " + s.dialect.Identifier("id") + " = " + s.dialect.Placeholder(8)
	result, err := s.database.ExecContext(ctx, query, value.DelegateUserID.String(), value.StartsAt, value.EndsAt, value.Enabled, value.UpdatedAt,
		value.WorkspaceID.String(), value.OwnerUserID.String(), value.ID)
	if err != nil {
		return value, fmt.Errorf("update notification inbox delegation: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return value, err
	}
	if count == 0 {
		_, err = s.database.ExecContext(ctx, s.dialect.Insert("notification_inbox_delegations", delegationColumns), value.ID, value.WorkspaceID.String(),
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
	query := "DELETE FROM " + s.dialect.Table("notification_inbox_delegations") + " WHERE " + s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(1) +
		" AND " + s.dialect.Identifier("owner_user_id") + " = " + s.dialect.Placeholder(2) + " AND " + s.dialect.Identifier("id") + " = " + s.dialect.Placeholder(3)
	result, err := s.database.ExecContext(ctx, query, workspaceID.String(), ownerID.String(), delegationID)
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
	query := "SELECT DISTINCT " + s.dialect.Identifier("owner_user_id") + " FROM " + s.dialect.Table("notification_inbox_delegations") + " WHERE " +
		s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(1) + " AND " + s.dialect.Identifier("delegate_user_id") + " = " + s.dialect.Placeholder(2) +
		" AND " + s.dialect.Identifier("surface") + " = " + s.dialect.Placeholder(3) + " AND " + s.dialect.Identifier("enabled") + " = " + s.dialect.Placeholder(4) +
		" AND (" + s.dialect.Identifier("starts_at") + " = '' OR " + s.dialect.Identifier("starts_at") + " <= " + s.dialect.Placeholder(5) + ") AND (" +
		s.dialect.Identifier("ends_at") + " = '' OR " + s.dialect.Identifier("ends_at") + " > " + s.dialect.Placeholder(6) + ") ORDER BY " + s.dialect.Identifier("owner_user_id") + " ASC"
	rows, err := s.database.QueryContext(ctx, query, workspaceID.String(), delegateID.String(), string(surface), true, now, now)
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
