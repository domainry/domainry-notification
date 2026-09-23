package inboxstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/domainry/domainry-foundation/mutation"
	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-orm/query"
	"github.com/domainry/domainry-orm/sqlhost"
)

var _ inbox.SavedViewStore = (*Store)(nil)

const (
	notificationUserSettingsTable = "_notification_user_settings"
	savedViewSettingKind          = "saved_view"
)

func (s *Store) ListSavedViews(ctx context.Context, workspaceID notification.WorkspaceID, recipientID notification.UserID) ([]inbox.SavedView, error) {
	if workspaceID == "" || recipientID == "" {
		return nil, fmt.Errorf("notification saved-view owner identity is required")
	}
	if err := s.requireWorkspace(workspaceID); err != nil {
		return nil, err
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	predicate := query.And(query.Equal("recipient_user_id", recipientID.String()), query.Equal("setting_kind", savedViewSettingKind))
	queryValue, args, err := query.NewWorkspaceSelectBuilder(s.Renderer, notificationUserSettingsTable, workspaceID.String()).Columns("payload_json").Where(predicate).OrderBy(query.Ascending("setting_key")).Build()
	if err != nil {
		return nil, err
	}
	rows, err := s.Database.QueryContext(ctx, queryValue, args...)
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

func (s *Store) SaveSavedView(ctx context.Context, workspaceID notification.WorkspaceID, recipientID notification.UserID, value inbox.SavedView) (inbox.SavedView, error) {
	value.Key = strings.TrimSpace(value.Key)
	if workspaceID == "" || recipientID == "" || value.Key == "" {
		return inbox.SavedView{}, fmt.Errorf("notification saved-view identity is required")
	}
	if err := s.requireWorkspace(workspaceID); err != nil {
		return inbox.SavedView{}, err
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	raw, err := json.Marshal(value)
	if err != nil {
		return inbox.SavedView{}, fmt.Errorf("encode notification inbox saved view: %w", err)
	}
	tx, err := s.Database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return inbox.SavedView{}, err
	}
	defer tx.Rollback()
	exists, err := s.savedViewExists(ctx, tx, workspaceID, recipientID, value.Key)
	if err != nil {
		return inbox.SavedView{}, err
	}
	if !exists {
		_, err = s.WorkspaceInsert(ctx, tx, workspaceID.String(), notificationUserSettingsTable, []string{"workspace_id", "recipient_user_id", "setting_kind", "setting_key", "payload_json", "updated_by", "created_at", "updated_at"},
			workspaceID.String(), recipientID.String(), savedViewSettingKind, value.Key, string(raw), recipientID.String(), value.CreatedAt, value.UpdatedAt)
		if err != nil {
			return inbox.SavedView{}, fmt.Errorf("insert notification inbox saved view: %w", err)
		}
		return value, tx.Commit()
	}
	predicate := query.And(query.Equal("recipient_user_id", recipientID.String()), query.Equal("setting_kind", savedViewSettingKind), query.Equal("setting_key", value.Key))
	queryValue, args, err := query.NewWorkspaceUpdateBuilder(s.Renderer, notificationUserSettingsTable, workspaceID.String()).Set("payload_json", string(raw)).Set("updated_by", recipientID.String()).Set("updated_at", value.UpdatedAt).Where(predicate).Build()
	if err != nil {
		return inbox.SavedView{}, err
	}
	result, err := tx.ExecContext(ctx, queryValue, args...)
	if err != nil {
		return inbox.SavedView{}, fmt.Errorf("update notification inbox saved view: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return inbox.SavedView{}, err
	}
	if count != 1 {
		return inbox.SavedView{}, mutation.MutationConflict("notification_inbox_saved_view", value.Key, mutation.MutationConflictOptimistic, nil)
	}
	return value, tx.Commit()
}

func (s *Store) DeleteSavedView(ctx context.Context, workspaceID notification.WorkspaceID, recipientID notification.UserID, key string) (bool, error) {
	key = strings.TrimSpace(key)
	if workspaceID == "" || recipientID == "" || key == "" {
		return false, fmt.Errorf("notification saved-view identity is required")
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
	exists, err := s.savedViewExists(ctx, tx, workspaceID, recipientID, key)
	if err != nil || !exists {
		return false, err
	}
	predicate := query.And(query.Equal("recipient_user_id", recipientID.String()), query.Equal("setting_kind", savedViewSettingKind), query.Equal("setting_key", key))
	queryValue, args, err := query.NewWorkspaceDeleteBuilder(s.Renderer, notificationUserSettingsTable, workspaceID.String()).Where(predicate).Build()
	if err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx, queryValue, args...)
	if err != nil {
		return false, fmt.Errorf("delete notification inbox saved view: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return false, err
	}
	return true, tx.Commit()
}

func (s *Store) savedViewExists(ctx context.Context, queryer sqlhost.Queryer, workspaceID notification.WorkspaceID, recipientID notification.UserID, key string) (bool, error) {
	predicate := query.And(query.Equal("recipient_user_id", recipientID.String()), query.Equal("setting_kind", savedViewSettingKind), query.Equal("setting_key", key))
	statement, args, err := query.NewWorkspaceSelectBuilder(s.Renderer, notificationUserSettingsTable, workspaceID.String()).Columns("setting_key").Where(predicate).Build()
	if err != nil {
		return false, err
	}
	var foundKey string
	err = queryer.QueryRowContext(ctx, statement, args...).Scan(&foundKey)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
