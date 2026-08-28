package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/domainry/domainry-notification/internal/domain/delivery/service"
	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

var _ inbox.EventStore = (*Store)(nil)

var inboxItemColumns = []string{
	"id", "workspace_id", "recipient_user_id", "surface", "event_id", "event_type", "source", "category", "severity",
	"title", "body", "search_text", "payload_json", "subject_type", "subject_id", "action_state", "alert_state", "group_key",
	"occurrence_count", "first_occurred_at", "last_occurred_at", "read_at", "archived_at", "expires_at", "created_at", "updated_at",
}

var alertGroupColumns = []string{
	"workspace_id", "recipient_user_id", "surface", "group_key", "state", "occurrence_count", "first_occurred_at",
	"last_occurred_at", "acknowledged_at", "acknowledged_by", "resolved_at", "last_event_id", "updated_at",
}

var channelPlanColumns = []string{
	"id", "workspace_id", "event_id", "channel", "status", "payload_json", "attempt_count", "next_attempt_at",
	"last_error_code", "outbox_message_id", "lease_owner", "lease_expires_at", "fencing_token", "created_at", "updated_at",
}

// Materialize commits the in-app projection, external delivery plans, and the
// fenced terminal event transition as one unit. Worker wakeups deliberately
// happen outside this adapter, after this method returns successfully.
func (s *Store) Materialize(ctx context.Context, event inbox.Event, items []inbox.Item) error {
	if event.WorkspaceID == "" || event.ID == "" || event.LeaseOwner == "" {
		return fmt.Errorf("notification materialization requires a claimed event")
	}
	ctx = s.workspaceScope.Context(ctx, event.WorkspaceID)
	tx, err := s.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin notification materialization: %w", err)
	}
	defer tx.Rollback()
	for _, item := range items {
		if err := s.transitionAlertGroup(ctx, tx, event, item); err != nil {
			return err
		}
		if err := s.upsertInboxItem(ctx, tx, item); err != nil {
			return err
		}
	}
	if len(event.ChannelPlans) > 0 {
		if err := s.queueScopes.Register(ctx, tx, notification.WorkChannelPlan, event.WorkspaceID, event.UpdatedAt); err != nil {
			return fmt.Errorf("register notification channel queue scope: %w", err)
		}
	}
	for _, plan := range event.ChannelPlans {
		if err := s.insertChannelPlan(ctx, tx, plan); err != nil {
			return err
		}
	}
	query := "UPDATE " + s.dialect.Table("notification_events") + " SET " + s.dialect.Identifier("status") + " = 'materialized', " +
		s.dialect.Identifier("lease_owner") + " = '', " + s.dialect.Identifier("lease_expires_at") + " = '', " + s.dialect.Identifier("last_error_code") + " = '', " +
		s.dialect.Identifier("updated_at") + " = " + s.dialect.Placeholder(1) + " WHERE " + s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(2) +
		" AND " + s.dialect.Identifier("id") + " = " + s.dialect.Placeholder(3) + " AND " + s.dialect.Identifier("status") + " = 'processing' AND " +
		s.dialect.Identifier("lease_owner") + " = " + s.dialect.Placeholder(4) + " AND " + s.dialect.Identifier("fencing_token") + " = " + s.dialect.Placeholder(5)
	result, err := tx.ExecContext(ctx, query, event.UpdatedAt, event.WorkspaceID.String(), event.ID, event.LeaseOwner, event.FencingToken)
	if err != nil {
		return fmt.Errorf("finish notification event: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrLeaseLost
	}
	return tx.Commit()
}

func (s *Store) transitionAlertGroup(ctx context.Context, tx *sql.Tx, event inbox.Event, item inbox.Item) error {
	if event.AlertState == "" {
		return nil
	}
	resolvedAt, increment := "", 0
	if event.AlertState == inbox.AlertResolved {
		resolvedAt = event.OccurredAt
	}
	if event.AlertState == inbox.AlertFiring {
		increment = 1
	}
	update := "UPDATE " + s.dialect.Table("notification_alert_groups") + " SET " + s.dialect.Identifier("state") + " = " + s.dialect.Placeholder(1) + ", " +
		s.dialect.Identifier("occurrence_count") + " = " + s.dialect.Identifier("occurrence_count") + " + " + fmt.Sprint(increment) + ", " +
		s.dialect.Identifier("last_occurred_at") + " = " + s.dialect.Placeholder(2) + ", " + s.dialect.Identifier("acknowledged_at") + " = '', " +
		s.dialect.Identifier("acknowledged_by") + " = '', " + s.dialect.Identifier("resolved_at") + " = " + s.dialect.Placeholder(3) + ", " +
		s.dialect.Identifier("last_event_id") + " = " + s.dialect.Placeholder(4) + ", " + s.dialect.Identifier("updated_at") + " = " + s.dialect.Placeholder(5) +
		" WHERE " + s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(6) + " AND " + s.dialect.Identifier("recipient_user_id") + " = " + s.dialect.Placeholder(7) +
		" AND " + s.dialect.Identifier("surface") + " = " + s.dialect.Placeholder(8) + " AND " + s.dialect.Identifier("group_key") + " = " + s.dialect.Placeholder(9) +
		" AND " + s.dialect.Identifier("last_occurred_at") + " <= " + s.dialect.Placeholder(10)
	result, err := tx.ExecContext(ctx, update, string(event.AlertState), event.OccurredAt, resolvedAt, event.ID, event.UpdatedAt,
		event.WorkspaceID.String(), item.RecipientUserID.String(), string(event.Surface), event.GroupKey, event.OccurredAt)
	if err != nil {
		return fmt.Errorf("update notification alert group: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil || count == 1 {
		return err
	}
	var exists int
	lookup := "SELECT COUNT(*) FROM " + s.dialect.Table("notification_alert_groups") + " WHERE " + s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(1) +
		" AND " + s.dialect.Identifier("recipient_user_id") + " = " + s.dialect.Placeholder(2) + " AND " + s.dialect.Identifier("surface") + " = " + s.dialect.Placeholder(3) +
		" AND " + s.dialect.Identifier("group_key") + " = " + s.dialect.Placeholder(4)
	if err := tx.QueryRowContext(ctx, lookup, event.WorkspaceID.String(), item.RecipientUserID.String(), string(event.Surface), event.GroupKey).Scan(&exists); err != nil {
		return err
	}
	if exists == 1 { // a newer transition already won
		return nil
	}
	_, err = tx.ExecContext(ctx, s.dialect.Insert("notification_alert_groups", alertGroupColumns), event.WorkspaceID.String(), item.RecipientUserID.String(), string(event.Surface),
		event.GroupKey, string(event.AlertState), increment, event.OccurredAt, event.OccurredAt, "", "", resolvedAt, event.ID, event.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert notification alert group: %w", err)
	}
	return nil
}

func (s *Store) upsertInboxItem(ctx context.Context, tx *sql.Tx, item inbox.Item) error {
	raw, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("encode notification inbox item: %w", err)
	}
	increment := 1
	if item.AlertState == inbox.AlertResolved {
		increment = 0
	}
	mutable := []string{"event_id", "event_type", "source", "category", "severity", "title", "body", "search_text", "payload_json", "subject_type", "subject_id", "action_state", "alert_state", "last_occurred_at", "expires_at", "updated_at"}
	args := []any{item.EventID, item.EventType, item.Source, item.Category, item.Severity, item.Title, item.Body, inboxSearchText(item), string(raw), item.SubjectType, item.SubjectID, string(item.ActionState), string(item.AlertState), item.LastOccurredAt, item.ExpiresAt, item.UpdatedAt}
	assignments := make([]string, 0, len(mutable)+1)
	for index, column := range mutable {
		assignments = append(assignments, s.dialect.Identifier(column)+" = "+s.dialect.Placeholder(index+1))
	}
	assignments = append(assignments, s.dialect.Identifier("occurrence_count")+" = "+s.dialect.Identifier("occurrence_count")+" + "+fmt.Sprint(increment))
	query := "UPDATE " + s.dialect.Table("notification_inbox_items") + " SET " + strings.Join(assignments, ", ") + " WHERE " +
		s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(17) + " AND " + s.dialect.Identifier("id") + " = " + s.dialect.Placeholder(18) +
		" AND " + s.dialect.Identifier("last_occurred_at") + " <= " + s.dialect.Placeholder(19)
	args = append(args, item.WorkspaceID.String(), item.ID, item.LastOccurredAt)
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update notification inbox item: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil || count == 1 {
		return err
	}
	var exists int
	lookup := "SELECT COUNT(*) FROM " + s.dialect.Table("notification_inbox_items") + " WHERE " + s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(1) + " AND " + s.dialect.Identifier("id") + " = " + s.dialect.Placeholder(2)
	if err := tx.QueryRowContext(ctx, lookup, item.WorkspaceID.String(), item.ID).Scan(&exists); err != nil {
		return err
	}
	if exists == 1 { // a newer occurrence already won
		return nil
	}
	occurrences := item.OccurrenceCount
	if item.AlertState == inbox.AlertFiring && occurrences == 0 {
		occurrences = 1
	}
	values := []any{item.ID, item.WorkspaceID.String(), item.RecipientUserID.String(), string(item.Surface), item.EventID, item.EventType, item.Source, item.Category, item.Severity,
		item.Title, item.Body, inboxSearchText(item), string(raw), item.SubjectType, item.SubjectID, string(item.ActionState), string(item.AlertState), item.GroupKey,
		occurrences, item.FirstOccurredAt, item.LastOccurredAt, item.ReadAt, item.ArchivedAt, item.ExpiresAt, item.CreatedAt, item.UpdatedAt}
	if _, err := tx.ExecContext(ctx, s.dialect.Insert("notification_inbox_items", inboxItemColumns), values...); err != nil {
		return fmt.Errorf("insert notification inbox item: %w", err)
	}
	return nil
}

func (s *Store) insertChannelPlan(ctx context.Context, tx *sql.Tx, plan delivery.Plan) error {
	raw, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("encode notification channel plan: %w", err)
	}
	_, err = tx.ExecContext(ctx, s.dialect.Insert("notification_channel_plans", channelPlanColumns), plan.ID, plan.WorkspaceID.String(), plan.EventID, plan.Channel,
		plan.Status, string(raw), plan.AttemptCount, plan.NextAttemptAt, plan.LastErrorCode, plan.OutboxMessageID, plan.LeaseOwner, plan.LeaseExpiresAt,
		plan.FencingToken, plan.CreatedAt, plan.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert notification channel plan: %w", err)
	}
	return nil
}

func inboxSearchText(item inbox.Item) string {
	parts := []string{item.Title, item.Body, item.EventType, item.Source, item.Category, item.SubjectType, item.SubjectID}
	for _, fact := range item.Facts {
		parts = append(parts, fact.Key, fact.Value)
	}
	return strings.ToLower(strings.Join(parts, " \n"))
}
