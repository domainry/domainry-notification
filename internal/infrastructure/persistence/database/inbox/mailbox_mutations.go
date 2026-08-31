package inboxstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/domainry/domainry-foundation/mutation"
	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-orm/query"
)

var _ inbox.MailboxStore = (*Store)(nil)

func (s *Store) SetRead(ctx context.Context, queryValue inbox.Query, itemID, readAt, updatedAt string) (inbox.Item, bool, error) {
	return s.updateInboxPersonalState(ctx, queryValue, itemID, "read_at", readAt, updatedAt)
}

func (s *Store) SetArchived(ctx context.Context, queryValue inbox.Query, itemID, archivedAt, updatedAt string) (inbox.Item, bool, error) {
	return s.updateInboxPersonalState(ctx, queryValue, itemID, "archived_at", archivedAt, updatedAt)
}

func (s *Store) updateInboxPersonalState(ctx context.Context, queryValue inbox.Query, itemID, column, value, updatedAt string) (inbox.Item, bool, error) {
	queryValue, err := normalizePersonalMailboxMutation(queryValue)
	if err != nil {
		return inbox.Item{}, false, err
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" || strings.TrimSpace(updatedAt) == "" {
		return inbox.Item{}, false, fmt.Errorf("notification inbox mutation identity and timestamp are required")
	}
	ctx = s.workspaceScope.Context(ctx, queryValue.WorkspaceID)
	statement, args, err := query.NewWorkspaceUpdateBuilder(s.Renderer, "_notification_inbox_items", queryValue.WorkspaceID.String()).Set(column, strings.TrimSpace(value)).Set("updated_at", strings.TrimSpace(updatedAt)).Where(query.And(mailboxAccessPredicate(queryValue), query.Equal("id", itemID))).Build()
	if err != nil {
		return inbox.Item{}, false, err
	}
	result, err := s.Database.ExecContext(ctx, statement, args...)
	if err != nil {
		return inbox.Item{}, false, fmt.Errorf("update notification inbox personal state: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return inbox.Item{}, false, err
	}
	return s.GetItem(ctx, queryValue, itemID)
}

func (s *Store) MarkAllRead(ctx context.Context, queryValue inbox.Query, readAt string) (int, error) {
	queryValue, err := normalizePersonalMailboxMutation(queryValue)
	if err != nil {
		return 0, err
	}
	readAt = strings.TrimSpace(readAt)
	if readAt == "" {
		return 0, fmt.Errorf("notification inbox read timestamp is required")
	}
	ctx = s.workspaceScope.Context(ctx, queryValue.WorkspaceID)
	statement, args, err := query.NewWorkspaceUpdateBuilder(s.Renderer, "_notification_inbox_items", queryValue.WorkspaceID.String()).Set("read_at", readAt).Set("updated_at", readAt).Where(query.And(mailboxPredicate(queryValue, false), query.Equal("read_at", ""), query.LessThanOrEqual("updated_at", readAt))).Build()
	if err != nil {
		return 0, err
	}
	result, err := s.Database.ExecContext(ctx, statement, args...)
	if err != nil {
		return 0, fmt.Errorf("mark notification inbox items read: %w", err)
	}
	count, err := result.RowsAffected()
	return int(count), err
}

func (s *Store) AcknowledgeAlert(ctx context.Context, queryValue inbox.Query, itemID string, actor notification.UserID, acknowledgedAt string) (inbox.Item, bool, error) {
	queryValue, err := normalizePersonalMailboxMutation(queryValue)
	if err != nil {
		return inbox.Item{}, false, err
	}
	itemID, acknowledgedAt = strings.TrimSpace(itemID), strings.TrimSpace(acknowledgedAt)
	if itemID == "" || actor == "" || actor != queryValue.ViewerUserID || acknowledgedAt == "" {
		return inbox.Item{}, false, fmt.Errorf("notification alert acknowledgement identity is invalid")
	}
	ctx = s.workspaceScope.Context(ctx, queryValue.WorkspaceID)
	tx, err := s.Database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return inbox.Item{}, false, err
	}
	defer tx.Rollback()
	item, found, err := s.getInboxItem(ctx, tx, queryValue, itemID)
	if err != nil || !found {
		return item, found, err
	}
	if item.GroupKey == "" || item.AlertState == "" || item.AlertState == inbox.AlertResolved {
		return inbox.Item{}, false, fmt.Errorf("notification alert is not firing")
	}
	if item.AlertState != inbox.AlertAcknowledged {
		groupUpdate, groupArgs, buildErr := query.NewWorkspaceUpdateBuilder(s.Renderer, "_notification_alert_groups", item.WorkspaceID.String()).Set("state", string(inbox.AlertAcknowledged)).Set("acknowledged_at", acknowledgedAt).Set("acknowledged_by", actor.String()).Set("updated_at", acknowledgedAt).Where(query.And(query.Equal("recipient_user_id", item.RecipientUserID.String()), query.Equal("surface", string(item.Surface)), query.Equal("group_key", item.GroupKey), query.Equal("state", "firing"))).Build()
		if buildErr != nil {
			return inbox.Item{}, false, buildErr
		}
		result, updateErr := tx.ExecContext(ctx, groupUpdate, groupArgs...)
		if updateErr != nil {
			return inbox.Item{}, false, fmt.Errorf("acknowledge notification alert group: %w", updateErr)
		}
		count, countErr := result.RowsAffected()
		if countErr != nil {
			return inbox.Item{}, false, countErr
		}
		if count != 1 {
			return inbox.Item{}, false, mutation.MutationConflict("notification_alert_group", item.GroupKey, mutation.MutationConflictOptimistic, nil)
		}
		itemUpdate, itemArgs, buildErr := query.NewWorkspaceUpdateBuilder(s.Renderer, "_notification_inbox_items", item.WorkspaceID.String()).Set("alert_state", string(inbox.AlertAcknowledged)).Set("updated_at", acknowledgedAt).Where(query.And(query.Equal("recipient_user_id", item.RecipientUserID.String()), query.Equal("surface", string(item.Surface)), query.Equal("id", item.ID))).Build()
		if buildErr != nil {
			return inbox.Item{}, false, buildErr
		}
		if _, updateErr = tx.ExecContext(ctx, itemUpdate, itemArgs...); updateErr != nil {
			return inbox.Item{}, false, fmt.Errorf("acknowledge notification inbox item: %w", updateErr)
		}
	}
	if err := tx.Commit(); err != nil {
		return inbox.Item{}, false, err
	}
	return s.GetItem(ctx, queryValue, itemID)
}

func normalizePersonalMailboxMutation(queryValue inbox.Query) (inbox.Query, error) {
	queryValue, err := normalizeMailboxStoreQuery(queryValue)
	if err != nil {
		return queryValue, err
	}
	if queryValue.Scope != inbox.ScopeMine || queryValue.ViewerUserID == "" || len(queryValue.RecipientUserIDs) != 1 || queryValue.RecipientUserIDs[0] != queryValue.ViewerUserID {
		return queryValue, fmt.Errorf("notification mailbox mutation requires the viewer's personal scope")
	}
	return queryValue, nil
}
