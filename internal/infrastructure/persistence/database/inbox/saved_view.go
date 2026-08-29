package inboxstore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-orm/builder"
)

var _ inbox.SavedViewStore = (*Store)(nil)

func (s *Store) ListSavedViews(ctx context.Context, workspaceID notification.WorkspaceID, recipientID notification.UserID, surface notification.Surface) ([]inbox.SavedView, error) {
	if workspaceID == "" || recipientID == "" || surface == "" {
		return nil, fmt.Errorf("notification saved-view owner identity is required")
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	query, args, err := builder.NewWorkspaceSelectBuilder(s.Renderer, "notification_inbox_saved_views", workspaceID.String()).Columns("payload_json").Where(builder.And(builder.Equal("recipient_user_id", recipientID.String()), builder.Equal("surface", string(surface)))).OrderBy(builder.Ascending("view_key")).Build()
	if err != nil {
		return nil, err
	}
	rows, err := s.Database.QueryContext(ctx, query, args...)
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
	query, args, err := builder.NewWorkspaceUpdateBuilder(s.Renderer, "notification_inbox_saved_views", workspaceID.String()).Set("payload_json", string(raw)).Set("updated_at", value.UpdatedAt).Where(builder.And(builder.Equal("recipient_user_id", recipientID.String()), builder.Equal("surface", string(surface)), builder.Equal("view_key", value.Key))).Build()
	if err != nil {
		return inbox.SavedView{}, err
	}
	result, err := s.Database.ExecContext(ctx, query, args...)
	if err != nil {
		return inbox.SavedView{}, fmt.Errorf("update notification inbox saved view: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return inbox.SavedView{}, err
	}
	if count == 0 {
		_, err = s.WorkspaceInsert(ctx, s.Database, workspaceID.String(), "notification_inbox_saved_views", []string{"workspace_id", "recipient_user_id", "surface", "view_key", "payload_json", "created_at", "updated_at"},
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
	query, args, err := builder.NewWorkspaceDeleteBuilder(s.Renderer, "notification_inbox_saved_views", workspaceID.String()).Where(builder.And(builder.Equal("recipient_user_id", recipientID.String()), builder.Equal("surface", string(surface)), builder.Equal("view_key", key))).Build()
	if err != nil {
		return false, err
	}
	result, err := s.Database.ExecContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("delete notification inbox saved view: %w", err)
	}
	count, err := result.RowsAffected()
	return count == 1, err
}
