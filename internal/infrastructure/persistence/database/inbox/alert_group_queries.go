package inboxstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-orm/query"
)

// GetAlertGroup reads one exact durable alert identity for a trusted system
// evaluator. User mailbox authorization does not use this method.
func (s *Store) GetAlertGroup(ctx context.Context, workspaceID notification.WorkspaceID, recipientUserID notification.UserID, groupKey string) (inbox.AlertGroup, bool, error) {
	if err := s.requireWorkspace(workspaceID); err != nil {
		return inbox.AlertGroup{}, false, err
	}
	groupKey = strings.TrimSpace(groupKey)
	if recipientUserID == "" || groupKey == "" {
		return inbox.AlertGroup{}, false, fmt.Errorf("notification alert group identity is required")
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	statement, args, err := query.NewWorkspaceSelectBuilder(s.Renderer, "_notification_alert_groups", workspaceID.String()).Columns(
		"workspace_id", "recipient_user_id", "group_key", "state", "occurrence_count", "first_occurred_at",
		"last_occurred_at", "acknowledged_at", "acknowledged_by", "resolved_at", "last_event_id", "updated_at",
	).Where(query.And(query.Equal("recipient_user_id", recipientUserID.String()), query.Equal("group_key", groupKey))).Build()
	if err != nil {
		return inbox.AlertGroup{}, false, err
	}
	var value inbox.AlertGroup
	var state, acknowledgedBy string
	var firstOccurredAt, lastOccurredAt, acknowledgedAt, resolvedAt, updatedAt int64
	err = s.Database.QueryRowContext(ctx, statement, args...).Scan(
		&value.WorkspaceID, &value.RecipientUserID, &value.GroupKey, &state, &value.OccurrenceCount,
		&firstOccurredAt, &lastOccurredAt, &acknowledgedAt, &acknowledgedBy, &resolvedAt, &value.LastEventID, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return inbox.AlertGroup{}, false, nil
	}
	if err != nil {
		return inbox.AlertGroup{}, false, fmt.Errorf("read notification alert group: %w", err)
	}
	value.State = inbox.AlertState(state)
	value.FirstOccurredAt = notification.MillisTimestamp(firstOccurredAt)
	value.LastOccurredAt = notification.MillisTimestamp(lastOccurredAt)
	value.AcknowledgedAt = notification.MillisTimestamp(acknowledgedAt)
	value.AcknowledgedBy = notification.UserID(acknowledgedBy)
	value.ResolvedAt = notification.MillisTimestamp(resolvedAt)
	value.UpdatedAt = notification.MillisTimestamp(updatedAt)
	return value, true, nil
}
