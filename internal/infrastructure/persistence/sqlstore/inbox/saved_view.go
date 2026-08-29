package inboxstore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

var _ inbox.SavedViewStore = (*Store)(nil)

func (s *Store) ListSavedViews(ctx context.Context, workspaceID notification.WorkspaceID, recipientID notification.UserID, surface notification.Surface) ([]inbox.SavedView, error) {
	if workspaceID == "" || recipientID == "" || surface == "" {
		return nil, fmt.Errorf("notification saved-view owner identity is required")
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	query := "SELECT " + s.dialect.Identifier("payload_json") + " FROM " + s.dialect.Table("notification_inbox_saved_views") + " WHERE " +
		s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(1) + " AND " + s.dialect.Identifier("recipient_user_id") + " = " + s.dialect.Placeholder(2) +
		" AND " + s.dialect.Identifier("surface") + " = " + s.dialect.Placeholder(3) + " ORDER BY " + s.dialect.Identifier("view_key") + " ASC"
	rows, err := s.database.QueryContext(ctx, query, workspaceID.String(), recipientID.String(), string(surface))
	if err != nil {
		return nil, fmt.Errorf("list notification inbox saved views: %w", err)
	}
	defer rows.Close()
	values := []inbox.SavedView{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var value inbox.SavedView
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return nil, fmt.Errorf("decode notification inbox saved view: %w", err)
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) SaveSavedView(ctx context.Context, workspaceID notification.WorkspaceID, recipientID notification.UserID, surface notification.Surface, value inbox.SavedView) (inbox.SavedView, error) {
	value.Key = strings.TrimSpace(value.Key)
	if workspaceID == "" || recipientID == "" || surface == "" || value.Key == "" {
		return inbox.SavedView{}, fmt.Errorf("notification saved-view identity is required")
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	raw, err := json.Marshal(value)
	if err != nil {
		return inbox.SavedView{}, fmt.Errorf("encode notification inbox saved view: %w", err)
	}
	query := "UPDATE " + s.dialect.Table("notification_inbox_saved_views") + " SET " + s.dialect.Identifier("payload_json") + " = " + s.dialect.Placeholder(1) +
		", " + s.dialect.Identifier("updated_at") + " = " + s.dialect.Placeholder(2) + " WHERE " + s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(3) +
		" AND " + s.dialect.Identifier("recipient_user_id") + " = " + s.dialect.Placeholder(4) + " AND " + s.dialect.Identifier("surface") + " = " + s.dialect.Placeholder(5) +
		" AND " + s.dialect.Identifier("view_key") + " = " + s.dialect.Placeholder(6)
	result, err := s.database.ExecContext(ctx, query, string(raw), value.UpdatedAt, workspaceID.String(), recipientID.String(), string(surface), value.Key)
	if err != nil {
		return inbox.SavedView{}, fmt.Errorf("update notification inbox saved view: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return inbox.SavedView{}, err
	}
	if count == 0 {
		_, err = s.database.ExecContext(ctx, s.dialect.Insert("notification_inbox_saved_views", []string{"workspace_id", "recipient_user_id", "surface", "view_key", "payload_json", "created_at", "updated_at"}),
			workspaceID.String(), recipientID.String(), string(surface), value.Key, string(raw), value.CreatedAt, value.UpdatedAt)
		if err != nil {
			return inbox.SavedView{}, fmt.Errorf("insert notification inbox saved view: %w", err)
		}
	}
	return value, nil
}

func (s *Store) DeleteSavedView(ctx context.Context, workspaceID notification.WorkspaceID, recipientID notification.UserID, surface notification.Surface, key string) (bool, error) {
	key = strings.TrimSpace(key)
	if workspaceID == "" || recipientID == "" || surface == "" || key == "" {
		return false, fmt.Errorf("notification saved-view identity is required")
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	query := "DELETE FROM " + s.dialect.Table("notification_inbox_saved_views") + " WHERE " + s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(1) +
		" AND " + s.dialect.Identifier("recipient_user_id") + " = " + s.dialect.Placeholder(2) + " AND " + s.dialect.Identifier("surface") + " = " + s.dialect.Placeholder(3) +
		" AND " + s.dialect.Identifier("view_key") + " = " + s.dialect.Placeholder(4)
	result, err := s.database.ExecContext(ctx, query, workspaceID.String(), recipientID.String(), string(surface), key)
	if err != nil {
		return false, fmt.Errorf("delete notification inbox saved view: %w", err)
	}
	count, err := result.RowsAffected()
	return count == 1, err
}
