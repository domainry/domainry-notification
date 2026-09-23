package eventstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/domainry/domainry-foundation/mutation"
	"github.com/domainry/domainry-notification/internal/domain/delivery/service"
	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-orm/query"
)

var _ inbox.EventStore = (*Store)(nil)

var inboxItemColumns = []string{
	"id", "workspace_id", "recipient_user_id", "event_id", "event_type", "source", "category", "severity",
	"title", "body", "search_text", "payload_json", "subject_type", "subject_id", "action_state", "alert_state", "group_key",
	"occurrence_count", "first_occurred_at", "last_occurred_at", "read_at", "archived_at", "expires_at", "created_at", "updated_at",
}

var alertGroupColumns = []string{
	"workspace_id", "recipient_user_id", "group_key", "state", "occurrence_count", "first_occurred_at",
	"last_occurred_at", "acknowledged_at", "acknowledged_by", "resolved_at", "last_event_id", "updated_at",
}

var deliveryColumns = []string{
	"id", "workspace_id", "row_kind", "event_id", "recipient_key", "template_key", "channel", "dedupe_key", "status", "payload_json", "attempt_count", "next_attempt_at",
	"last_error_code", "outbox_message_id", "lease_owner", "lease_expires_at", "fencing_token", "created_at", "updated_at",
}

const notificationDeliveriesTable = "_notification_deliveries"

func (s *Store) Materialize(ctx context.Context, event inbox.Event, items []inbox.Item) error {
	if event.WorkspaceID == "" || event.ID == "" || event.LeaseOwner == "" {
		return fmt.Errorf("notification materialization requires a claimed event")
	}
	ctx = s.workspaceScope.Context(ctx, event.WorkspaceID)
	tx, err := s.Database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
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
	queryValue, args, err := query.NewWorkspaceUpdateBuilder(s.Renderer, "_notification_events", event.WorkspaceID.String()).Set("status", "materialized").Set("lease_owner", "").Set("lease_expires_at", "").Set("last_error_code", "").Set("updated_at", event.UpdatedAt).Where(query.And(query.Equal("id", event.ID), query.Equal("status", "processing"), query.Equal("lease_owner", event.LeaseOwner), query.Equal("fencing_token", event.FencingToken))).Build()
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, queryValue, args...)
	if err != nil {
		return fmt.Errorf("finish notification event: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return mutation.MutationConflict("notification_event", event.ID, mutation.MutationConflictLeaseLost, nil)
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
	update, updateArgs, err := query.NewWorkspaceUpdateBuilder(s.Renderer, "_notification_alert_groups", event.WorkspaceID.String()).Set("state", string(event.AlertState)).SetExpression("occurrence_count", query.Add(query.Column("occurrence_count"), query.Value(increment))).Set("last_occurred_at", event.OccurredAt).Set("acknowledged_at", "").Set("acknowledged_by", "").Set("resolved_at", resolvedAt).Set("last_event_id", event.ID).Set("updated_at", event.UpdatedAt).Where(query.And(query.Equal("recipient_user_id", item.RecipientUserID.String()), query.Equal("group_key", event.GroupKey), query.LessThanOrEqual("last_occurred_at", event.OccurredAt))).Build()
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, update, updateArgs...)
	if err != nil {
		return fmt.Errorf("update notification alert group: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil || count == 1 {
		return err
	}
	var exists int
	lookup, lookupArgs, err := query.NewWorkspaceSelectBuilder(s.Renderer, "_notification_alert_groups", event.WorkspaceID.String()).Projections(query.Project(query.CountAll())).Where(query.And(query.Equal("recipient_user_id", item.RecipientUserID.String()), query.Equal("group_key", event.GroupKey))).Build()
	if err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, lookup, lookupArgs...).Scan(&exists); err != nil {
		return err
	}
	if exists == 1 {
		return nil
	}
	_, err = s.WorkspaceInsert(ctx, tx, event.WorkspaceID.String(), "_notification_alert_groups", alertGroupColumns, event.WorkspaceID.String(), item.RecipientUserID.String(),
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
	queryBuilder := query.NewWorkspaceUpdateBuilder(s.Renderer, "_notification_inbox_items", item.WorkspaceID.String()).
		Set("event_id", item.EventID).Set("event_type", item.EventType).Set("source", item.Source).
		Set("category", item.Category).Set("severity", item.Severity).Set("title", item.Title).
		Set("body", item.Body).Set("search_text", inboxSearchText(item)).Set("payload_json", string(raw)).
		Set("subject_type", item.SubjectType).Set("subject_id", item.SubjectID).
		Set("action_state", string(item.ActionState)).Set("alert_state", string(item.AlertState)).
		Set("last_occurred_at", item.LastOccurredAt).Set("expires_at", item.ExpiresAt).Set("updated_at", item.UpdatedAt)
	queryValue, args, err := queryBuilder.SetExpression("occurrence_count", query.Add(query.Column("occurrence_count"), query.Value(increment))).Where(query.And(query.Equal("id", item.ID), query.LessThanOrEqual("last_occurred_at", item.LastOccurredAt))).Build()
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, queryValue, args...)
	if err != nil {
		return fmt.Errorf("update notification inbox item: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil || count == 1 {
		return err
	}
	var exists int
	lookup, lookupArgs, err := query.NewWorkspaceSelectBuilder(s.Renderer, "_notification_inbox_items", item.WorkspaceID.String()).Projections(query.Project(query.CountAll())).Where(query.Equal("id", item.ID)).Build()
	if err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, lookup, lookupArgs...).Scan(&exists); err != nil {
		return err
	}
	if exists == 1 {
		return nil
	}
	occurrences := item.OccurrenceCount
	if item.AlertState == inbox.AlertFiring && occurrences == 0 {
		occurrences = 1
	}
	values := []any{item.ID, item.WorkspaceID.String(), item.RecipientUserID.String(), item.EventID, item.EventType, item.Source, item.Category, item.Severity,
		item.Title, item.Body, inboxSearchText(item), string(raw), item.SubjectType, item.SubjectID, string(item.ActionState), string(item.AlertState), item.GroupKey,
		occurrences, item.FirstOccurredAt, item.LastOccurredAt, item.ReadAt, item.ArchivedAt, item.ExpiresAt, item.CreatedAt, item.UpdatedAt}
	if _, err := s.WorkspaceInsert(ctx, tx, item.WorkspaceID.String(), "_notification_inbox_items", inboxItemColumns, values...); err != nil {
		return fmt.Errorf("insert notification inbox item: %w", err)
	}
	return nil
}

func (s *Store) insertChannelPlan(ctx context.Context, tx *sql.Tx, plan delivery.Plan) error {
	raw, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("encode notification channel plan: %w", err)
	}
	_, err = s.WorkspaceInsert(ctx, tx, plan.WorkspaceID.String(), notificationDeliveriesTable, deliveryColumns, plan.ID, plan.WorkspaceID.String(), "delivery", plan.EventID, "", plan.TemplateKey, plan.Channel, plan.DedupeKey,
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
