package inboxstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

var _ inbox.MailboxStore = (*Store)(nil)

func (s *Store) SetRead(ctx context.Context, query inbox.Query, itemID, readAt, updatedAt string) (inbox.Item, bool, error) {
	return s.updateInboxPersonalState(ctx, query, itemID, "read_at", readAt, updatedAt)
}

func (s *Store) SetArchived(ctx context.Context, query inbox.Query, itemID, archivedAt, updatedAt string) (inbox.Item, bool, error) {
	return s.updateInboxPersonalState(ctx, query, itemID, "archived_at", archivedAt, updatedAt)
}

func (s *Store) updateInboxPersonalState(ctx context.Context, query inbox.Query, itemID, column, value, updatedAt string) (inbox.Item, bool, error) {
	query, err := normalizePersonalMailboxMutation(query)
	if err != nil {
		return inbox.Item{}, false, err
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" || strings.TrimSpace(updatedAt) == "" {
		return inbox.Item{}, false, fmt.Errorf("notification inbox mutation identity and timestamp are required")
	}
	ctx = s.workspaceScope.Context(ctx, query.WorkspaceID)
	clauses, args, position := s.mailboxAccessWhere(query, 3)
	clauses = append(clauses, s.dialect.Identifier("id")+" = "+s.dialect.Placeholder(position))
	args = append([]any{strings.TrimSpace(value), strings.TrimSpace(updatedAt)}, append(args, itemID)...)
	statement := "UPDATE " + s.dialect.Table("notification_inbox_items") + " SET " + s.dialect.Identifier(column) + " = " + s.dialect.Placeholder(1) +
		", " + s.dialect.Identifier("updated_at") + " = " + s.dialect.Placeholder(2) + " WHERE " + strings.Join(clauses, " AND ")
	result, err := s.database.ExecContext(ctx, statement, args...)
	if err != nil {
		return inbox.Item{}, false, fmt.Errorf("update notification inbox personal state: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return inbox.Item{}, false, err
	}
	return s.GetItem(ctx, query, itemID)
}

func (s *Store) MarkAllRead(ctx context.Context, query inbox.Query, readAt string) (int, error) {
	query, err := normalizePersonalMailboxMutation(query)
	if err != nil {
		return 0, err
	}
	readAt = strings.TrimSpace(readAt)
	if readAt == "" {
		return 0, fmt.Errorf("notification inbox read timestamp is required")
	}
	ctx = s.workspaceScope.Context(ctx, query.WorkspaceID)
	where, args := s.mailboxWhere(query, false, 3)
	args = append([]any{readAt, readAt}, args...)
	boundaryPosition := len(args) + 1
	args = append(args, readAt)
	statement := "UPDATE " + s.dialect.Table("notification_inbox_items") + " SET " + s.dialect.Identifier("read_at") + " = " + s.dialect.Placeholder(1) +
		", " + s.dialect.Identifier("updated_at") + " = " + s.dialect.Placeholder(2) + " WHERE " + where + " AND " + s.dialect.Identifier("read_at") + " = '' AND " +
		s.dialect.Identifier("updated_at") + " <= " + s.dialect.Placeholder(boundaryPosition)
	result, err := s.database.ExecContext(ctx, statement, args...)
	if err != nil {
		return 0, fmt.Errorf("mark notification inbox items read: %w", err)
	}
	count, err := result.RowsAffected()
	return int(count), err
}

func (s *Store) AcknowledgeAlert(ctx context.Context, query inbox.Query, itemID string, actor notification.UserID, acknowledgedAt string) (inbox.Item, bool, error) {
	query, err := normalizePersonalMailboxMutation(query)
	if err != nil {
		return inbox.Item{}, false, err
	}
	itemID, acknowledgedAt = strings.TrimSpace(itemID), strings.TrimSpace(acknowledgedAt)
	if itemID == "" || actor == "" || actor != query.ViewerUserID || acknowledgedAt == "" {
		return inbox.Item{}, false, fmt.Errorf("notification alert acknowledgement identity is invalid")
	}
	ctx = s.workspaceScope.Context(ctx, query.WorkspaceID)
	tx, err := s.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return inbox.Item{}, false, err
	}
	defer tx.Rollback()
	item, found, err := s.getInboxItem(ctx, tx, query, itemID)
	if err != nil || !found {
		return item, found, err
	}
	if item.GroupKey == "" || item.AlertState == "" || item.AlertState == inbox.AlertResolved {
		return inbox.Item{}, false, fmt.Errorf("notification alert is not firing")
	}
	if item.AlertState != inbox.AlertAcknowledged {
		groupUpdate := "UPDATE " + s.dialect.Table("notification_alert_groups") + " SET " + s.dialect.Identifier("state") + " = " + s.dialect.Placeholder(1) +
			", " + s.dialect.Identifier("acknowledged_at") + " = " + s.dialect.Placeholder(2) + ", " + s.dialect.Identifier("acknowledged_by") + " = " + s.dialect.Placeholder(3) +
			", " + s.dialect.Identifier("updated_at") + " = " + s.dialect.Placeholder(4) + " WHERE " + s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(5) +
			" AND " + s.dialect.Identifier("recipient_user_id") + " = " + s.dialect.Placeholder(6) + " AND " + s.dialect.Identifier("surface") + " = " + s.dialect.Placeholder(7) +
			" AND " + s.dialect.Identifier("group_key") + " = " + s.dialect.Placeholder(8) + " AND " + s.dialect.Identifier("state") + " = 'firing'"
		result, updateErr := tx.ExecContext(ctx, groupUpdate, string(inbox.AlertAcknowledged), acknowledgedAt, actor.String(), acknowledgedAt,
			item.WorkspaceID.String(), item.RecipientUserID.String(), string(item.Surface), item.GroupKey)
		if updateErr != nil {
			return inbox.Item{}, false, fmt.Errorf("acknowledge notification alert group: %w", updateErr)
		}
		count, countErr := result.RowsAffected()
		if countErr != nil {
			return inbox.Item{}, false, countErr
		}
		if count != 1 {
			return inbox.Item{}, false, ErrMutationConflict
		}
		itemUpdate := "UPDATE " + s.dialect.Table("notification_inbox_items") + " SET " + s.dialect.Identifier("alert_state") + " = " + s.dialect.Placeholder(1) +
			", " + s.dialect.Identifier("updated_at") + " = " + s.dialect.Placeholder(2) + " WHERE " + s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(3) +
			" AND " + s.dialect.Identifier("recipient_user_id") + " = " + s.dialect.Placeholder(4) + " AND " + s.dialect.Identifier("surface") + " = " + s.dialect.Placeholder(5) +
			" AND " + s.dialect.Identifier("id") + " = " + s.dialect.Placeholder(6)
		if _, updateErr = tx.ExecContext(ctx, itemUpdate, string(inbox.AlertAcknowledged), acknowledgedAt, item.WorkspaceID.String(), item.RecipientUserID.String(), string(item.Surface), item.ID); updateErr != nil {
			return inbox.Item{}, false, fmt.Errorf("acknowledge notification inbox item: %w", updateErr)
		}
	}
	if err := tx.Commit(); err != nil {
		return inbox.Item{}, false, err
	}
	return s.GetItem(ctx, query, itemID)
}

func normalizePersonalMailboxMutation(query inbox.Query) (inbox.Query, error) {
	query, err := normalizeMailboxStoreQuery(query)
	if err != nil {
		return query, err
	}
	if query.Scope != inbox.ScopeMine || query.ViewerUserID == "" || len(query.RecipientUserIDs) != 1 || query.RecipientUserIDs[0] != query.ViewerUserID {
		return query, fmt.Errorf("notification mailbox mutation requires the viewer's personal scope")
	}
	return query, nil
}
