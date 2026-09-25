package deliverystore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/domainry/domainry-foundation/mutation"
	"github.com/domainry/domainry-notification/internal/domain/delivery/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/timejson"
	"github.com/domainry/domainry-orm/query"
	"github.com/domainry/domainry-orm/sqlhost"
)

var _ delivery.PlanStore = (*Store)(nil)

func (s *Store) GetPlan(ctx context.Context, workspaceID notification.WorkspaceID, planID string) (delivery.Plan, bool, error) {
	if workspaceID == "" || strings.TrimSpace(planID) == "" {
		return delivery.Plan{}, false, fmt.Errorf("notification channel plan identity is required")
	}
	return s.planByID(s.workspaceScope.Context(ctx, workspaceID), workspaceID, strings.TrimSpace(planID))
}

func (s *Store) ListDuePlans(ctx context.Context, now string, limit int) ([]delivery.Plan, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	workspaces, err := s.queueScopes.Workspaces(ctx, s.Database, notification.WorkChannelPlan, workspaceScanLimit(limit))
	if err != nil {
		return nil, err
	}
	plans := []delivery.Plan{}
	for _, workspaceID := range workspaces {
		values, listErr := s.listDuePlansForWorkspace(s.workspaceScope.Context(ctx, workspaceID), workspaceID, strings.TrimSpace(now), limit)
		if listErr != nil {
			return nil, listErr
		}
		plans = append(plans, values...)
	}
	sort.SliceStable(plans, func(i, j int) bool {
		if plans[i].CreatedAt == plans[j].CreatedAt {
			return plans[i].ID < plans[j].ID
		}
		return plans[i].CreatedAt < plans[j].CreatedAt
	})
	if len(plans) > limit {
		plans = plans[:limit]
	}
	return plans, nil
}

func (s *Store) listDuePlansForWorkspace(ctx context.Context, workspaceID notification.WorkspaceID, now string, limit int) ([]delivery.Plan, error) {
	nowMillis := notification.TimestampMillis(now)
	due := query.Or(
		query.And(query.Equal("status", "queued"), query.Or(query.Equal("next_attempt_at", int64(0)), query.LessThanOrEqual("next_attempt_at", nowMillis))),
		query.And(query.Equal("status", "processing"), query.LessThanOrEqual("lease_expires_at", nowMillis)),
	)
	queryValue, args, err := query.NewWorkspaceSelectBuilder(s.Renderer, notificationDeliveriesTable, workspaceID.String()).Columns(channelPlanColumns...).Where(query.And(query.Equal("row_kind", deliveryRowKind), due)).OrderBy(query.Ascending("created_at")).Limit(limit).Build()
	if err != nil {
		return nil, err
	}
	rows, err := s.Database.QueryContext(ctx, queryValue, args...)
	if err != nil {
		return nil, fmt.Errorf("list due notification channel plans: %w", err)
	}
	defer rows.Close()
	plans := []delivery.Plan{}
	for rows.Next() {
		plan, scanErr := scanPlan(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		plans = append(plans, plan)
	}
	return plans, rows.Err()
}

func (s *Store) ClaimPlan(ctx context.Context, workspaceID notification.WorkspaceID, planID, owner, now, expiresAt string) (delivery.Plan, bool, error) {
	planID, owner, now, expiresAt = strings.TrimSpace(planID), strings.TrimSpace(owner), strings.TrimSpace(now), strings.TrimSpace(expiresAt)
	if workspaceID == "" || planID == "" || owner == "" || now == "" || expiresAt == "" {
		return delivery.Plan{}, false, fmt.Errorf("notification channel plan claim identity and lease are required")
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	nowMillis, expiresAtMillis := notification.TimestampMillis(now), notification.TimestampMillis(expiresAt)
	due := query.Or(query.And(query.Equal("status", "queued"), query.Or(query.Equal("next_attempt_at", int64(0)), query.LessThanOrEqual("next_attempt_at", nowMillis))), query.And(query.Equal("status", "processing"), query.LessThanOrEqual("lease_expires_at", nowMillis)))
	queryValue, args, err := query.NewWorkspaceUpdateBuilder(s.Renderer, notificationDeliveriesTable, workspaceID.String()).Set("status", "processing").Set("lease_owner", owner).Set("lease_expires_at", expiresAtMillis).SetExpression("fencing_token", query.Add(query.Column("fencing_token"), query.Value(1))).Set("updated_at", nowMillis).Where(query.And(query.Equal("id", planID), query.Equal("row_kind", deliveryRowKind), due)).Build()
	if err != nil {
		return delivery.Plan{}, false, err
	}
	result, err := s.Database.ExecContext(ctx, queryValue, args...)
	if err != nil {
		return delivery.Plan{}, false, fmt.Errorf("claim notification channel plan: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return delivery.Plan{}, false, err
	}
	return s.planByID(ctx, workspaceID, planID)
}

func (s *Store) IsActionTerminal(ctx context.Context, plan delivery.Plan) (bool, error) {
	ctx = s.workspaceScope.Context(ctx, plan.WorkspaceID)
	queryValue, args, err := query.NewWorkspaceSelectBuilder(s.Renderer, "_notification_inbox_items", plan.WorkspaceID.String()).Projections(query.Project(query.CountAll()), query.Project(query.Coalesce(query.Sum(query.CaseWhen(query.Or(query.Equal("action_state", "open"), query.Equal("alert_state", "firing")), 1).Else(0)), query.Value(0)))).Where(query.Equal("event_id", plan.EventID)).Build()
	if err != nil {
		return false, err
	}
	var total, active int
	if err := s.Database.QueryRowContext(ctx, queryValue, args...).Scan(&total, &active); err != nil {
		return false, fmt.Errorf("inspect notification channel plan action state: %w", err)
	}
	return total > 0 && active == 0, nil
}

func (s *Store) CancelPlan(ctx context.Context, plan delivery.Plan, reasonCode, updatedAt string) error {
	return s.transitionPlan(ctx, plan, "cancelled", 0, reasonCode, "", "", updatedAt)
}

func (s *Store) CompletePlan(ctx context.Context, plan delivery.Plan, outboxMessageID, updatedAt string) error {
	return s.transitionPlan(ctx, plan, "planned", 0, "", outboxMessageID, "", updatedAt)
}

func (s *Store) RetryPlan(ctx context.Context, plan delivery.Plan, errorCode, nextAttemptAt, updatedAt string) error {
	return s.transitionPlanFailure(ctx, plan, "queued", errorCode, nextAttemptAt, updatedAt, "retry_scheduled", 1)
}

func (s *Store) FailPlan(ctx context.Context, plan delivery.Plan, errorCode, updatedAt string) error {
	return s.transitionPlanFailure(ctx, plan, "failed", errorCode, "", updatedAt, "dead_letter", 0)
}

func (s *Store) CompletePlanBatch(ctx context.Context, plans []delivery.Plan, outboxMessageID, updatedAt string) error {
	if len(plans) == 0 {
		return nil
	}
	workspaceID := plans[0].WorkspaceID
	for _, plan := range plans {
		if workspaceID == "" || plan.WorkspaceID != workspaceID {
			return fmt.Errorf("notification channel plan batch must belong to one workspace")
		}
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	tx, err := s.Database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, plan := range plans {
		if err := s.transitionPlanWith(ctx, tx, plan, "planned", 0, "", outboxMessageID, "", updatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) transitionPlan(ctx context.Context, plan delivery.Plan, status string, attemptIncrement int, errorCode, outboxMessageID, nextAttemptAt, updatedAt string) error {
	return s.transitionPlanWith(s.workspaceScope.Context(ctx, plan.WorkspaceID), s.Database, plan, status, attemptIncrement, errorCode, outboxMessageID, nextAttemptAt, updatedAt)
}

func (s *Store) transitionPlanFailure(ctx context.Context, plan delivery.Plan, status, errorCode, nextAttemptAt, updatedAt, disposition string, retryable int) error {
	ctx = s.workspaceScope.Context(ctx, plan.WorkspaceID)
	tx, err := s.Database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.transitionPlanWith(ctx, tx, plan, status, 1, errorCode, "", nextAttemptAt, updatedAt); err != nil {
		return err
	}
	columns := []string{"id", "workspace_id", "attempt_kind", "event_id", "delivery_id", "event_type", "source", "source_event_id", "channel", "stage", "error_code", "attempt", "disposition", "retryable", "next_attempt_at", "fencing_token", "occurred_at"}
	_, err = s.WorkspaceInsert(ctx, tx, plan.WorkspaceID.String(), "_notification_delivery_attempts", columns,
		deliveryAttemptID(plan), plan.WorkspaceID.String(), "channel", plan.EventID, plan.ID, "", "", "", plan.Channel, "channel_dispatch",
		strings.TrimSpace(errorCode), plan.AttemptCount+1, disposition, retryable, notification.TimestampMillis(nextAttemptAt), plan.FencingToken, notification.TimestampMillis(updatedAt))
	if err != nil {
		return fmt.Errorf("record notification channel delivery attempt: %w", err)
	}
	return tx.Commit()
}

func (s *Store) transitionPlanWith(ctx context.Context, executor sqlhost.Executor, plan delivery.Plan, status string, attemptIncrement int, errorCode, outboxMessageID, nextAttemptAt, updatedAt string) error {
	errorCode, outboxMessageID, nextAttemptAt, updatedAt = strings.TrimSpace(errorCode), strings.TrimSpace(outboxMessageID), strings.TrimSpace(nextAttemptAt), strings.TrimSpace(updatedAt)
	if errorCode != "" && !failureCodePattern.MatchString(errorCode) {
		return fmt.Errorf("notification channel plan error code is invalid")
	}
	queryValue, args, err := query.NewWorkspaceUpdateBuilder(s.Renderer, notificationDeliveriesTable, plan.WorkspaceID.String()).Set("status", status).SetExpression("attempt_count", query.Add(query.Column("attempt_count"), query.Value(attemptIncrement))).Set("next_attempt_at", notification.TimestampMillis(nextAttemptAt)).Set("last_error_code", errorCode).Set("outbox_message_id", outboxMessageID).Set("lease_owner", "").Set("lease_expires_at", int64(0)).Set("updated_at", notification.TimestampMillis(updatedAt)).Where(query.And(query.Equal("id", plan.ID), query.Equal("row_kind", deliveryRowKind), query.Equal("status", "processing"), query.Equal("lease_owner", plan.LeaseOwner), query.Equal("fencing_token", plan.FencingToken))).Build()
	if err != nil {
		return err
	}
	result, err := executor.ExecContext(ctx, queryValue, args...)
	if err != nil {
		return fmt.Errorf("transition notification channel plan: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return mutation.MutationConflict("notification_channel_plan", plan.ID, mutation.MutationConflictLeaseLost, nil)
	}
	return nil
}

func (s *Store) planByID(ctx context.Context, workspaceID notification.WorkspaceID, planID string) (delivery.Plan, bool, error) {
	queryValue, args, err := query.NewWorkspaceSelectBuilder(s.Renderer, notificationDeliveriesTable, workspaceID.String()).Columns(channelPlanColumns...).Where(query.And(query.Equal("id", planID), query.Equal("row_kind", deliveryRowKind))).Build()
	if err != nil {
		return delivery.Plan{}, false, err
	}
	plan, err := scanPlan(s.Database.QueryRowContext(ctx, queryValue, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return delivery.Plan{}, false, nil
	}
	return plan, err == nil, err
}

func scanPlan(row scanner) (delivery.Plan, error) {
	var plan delivery.Plan
	var id, workspaceID, eventID, channel, status, raw, lastErrorCode, outboxMessageID, leaseOwner string
	var nextAttemptAt, leaseExpiresAt, createdAt, updatedAt int64
	var attemptCount int
	var fencingToken int64
	if err := row.Scan(&id, &workspaceID, &eventID, &channel, &status, &raw, &attemptCount, &nextAttemptAt, &lastErrorCode, &outboxMessageID, &leaseOwner, &leaseExpiresAt, &fencingToken, &createdAt, &updatedAt); err != nil {
		return plan, err
	}
	if err := timejson.Unmarshal([]byte(raw), &plan); err != nil {
		return plan, fmt.Errorf("decode notification channel plan: %w", err)
	}
	plan.ID, plan.WorkspaceID, plan.EventID, plan.Channel, plan.Status = id, notification.WorkspaceID(workspaceID), eventID, channel, status
	plan.AttemptCount, plan.NextAttemptAt, plan.LastErrorCode, plan.OutboxMessageID = attemptCount, notification.MillisTimestamp(nextAttemptAt), lastErrorCode, outboxMessageID
	plan.LeaseOwner, plan.LeaseExpiresAt, plan.FencingToken = leaseOwner, notification.MillisTimestamp(leaseExpiresAt), fencingToken
	plan.CreatedAt, plan.UpdatedAt = notification.MillisTimestamp(createdAt), notification.MillisTimestamp(updatedAt)
	return plan, nil
}

func deliveryAttemptID(plan delivery.Plan) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{plan.WorkspaceID.String(), plan.EventID, plan.ID, fmt.Sprint(plan.FencingToken), "channel_attempt"}, "\x00")))
	return "notification_attempt_" + hex.EncodeToString(digest[:16])
}
