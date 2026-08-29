package deliverystore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/domainry/domainry-notification/internal/domain/delivery/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-orm/builder"
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
	due := builder.Or(
		builder.And(builder.Equal("status", "queued"), builder.Or(builder.Equal("next_attempt_at", ""), builder.LessThanOrEqual("next_attempt_at", now))),
		builder.And(builder.Equal("status", "processing"), builder.LessThanOrEqual("lease_expires_at", now)),
	)
	query, args, err := builder.NewSelectBuilder(s.Renderer, "notification_channel_plans").Columns(channelPlanColumns...).Where(builder.And(builder.Equal("workspace_id", workspaceID.String()), due)).OrderBy(builder.Ascending("created_at")).Limit(limit).Build()
	if err != nil {
		return nil, err
	}
	rows, err := s.Database.QueryContext(ctx, query, args...)
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
	due := builder.Or(builder.And(builder.Equal("status", "queued"), builder.Or(builder.Equal("next_attempt_at", ""), builder.LessThanOrEqual("next_attempt_at", now))), builder.And(builder.Equal("status", "processing"), builder.LessThanOrEqual("lease_expires_at", now)))
	query, args, err := builder.NewUpdateBuilder(s.Renderer, "notification_channel_plans").Set("status", "processing").Set("lease_owner", owner).Set("lease_expires_at", expiresAt).SetExpression("fencing_token", builder.Add(builder.Column("fencing_token"), builder.Value(1))).Set("updated_at", now).Where(builder.And(builder.Equal("workspace_id", workspaceID.String()), builder.Equal("id", planID), due)).Build()
	if err != nil {
		return delivery.Plan{}, false, err
	}
	result, err := s.Database.ExecContext(ctx, query, args...)
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
	query, args, err := builder.NewSelectBuilder(s.Renderer, "notification_inbox_items").Projections(builder.Project(builder.CountAll()), builder.Project(builder.Coalesce(builder.Sum(builder.CaseWhen(builder.Or(builder.Equal("action_state", "open"), builder.Equal("alert_state", "firing")), 1).Else(0)), builder.Value(0)))).Where(builder.And(builder.Equal("workspace_id", plan.WorkspaceID.String()), builder.Equal("event_id", plan.EventID))).Build()
	if err != nil {
		return false, err
	}
	var total, active int
	if err := s.Database.QueryRowContext(ctx, query, args...).Scan(&total, &active); err != nil {
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
	return s.transitionPlan(ctx, plan, "queued", 1, errorCode, "", nextAttemptAt, updatedAt)
}

func (s *Store) FailPlan(ctx context.Context, plan delivery.Plan, errorCode, updatedAt string) error {
	return s.transitionPlan(ctx, plan, "failed", 1, errorCode, "", "", updatedAt)
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

func (s *Store) transitionPlanWith(ctx context.Context, executor sqlhost.Executor, plan delivery.Plan, status string, attemptIncrement int, errorCode, outboxMessageID, nextAttemptAt, updatedAt string) error {
	errorCode, outboxMessageID, nextAttemptAt, updatedAt = strings.TrimSpace(errorCode), strings.TrimSpace(outboxMessageID), strings.TrimSpace(nextAttemptAt), strings.TrimSpace(updatedAt)
	if errorCode != "" && !failureCodePattern.MatchString(errorCode) {
		return fmt.Errorf("notification channel plan error code is invalid")
	}
	query, args, err := builder.NewUpdateBuilder(s.Renderer, "notification_channel_plans").Set("status", status).SetExpression("attempt_count", builder.Add(builder.Column("attempt_count"), builder.Value(attemptIncrement))).Set("next_attempt_at", nextAttemptAt).Set("last_error_code", errorCode).Set("outbox_message_id", outboxMessageID).Set("lease_owner", "").Set("lease_expires_at", "").Set("updated_at", updatedAt).Where(builder.And(builder.Equal("workspace_id", plan.WorkspaceID.String()), builder.Equal("id", plan.ID), builder.Equal("status", "processing"), builder.Equal("lease_owner", plan.LeaseOwner), builder.Equal("fencing_token", plan.FencingToken))).Build()
	if err != nil {
		return err
	}
	result, err := executor.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("transition notification channel plan: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (s *Store) planByID(ctx context.Context, workspaceID notification.WorkspaceID, planID string) (delivery.Plan, bool, error) {
	query, args, err := builder.NewSelectBuilder(s.Renderer, "notification_channel_plans").Columns(channelPlanColumns...).Where(builder.And(builder.Equal("workspace_id", workspaceID.String()), builder.Equal("id", planID))).Build()
	if err != nil {
		return delivery.Plan{}, false, err
	}
	plan, err := scanPlan(s.Database.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return delivery.Plan{}, false, nil
	}
	return plan, err == nil, err
}

func scanPlan(row scanner) (delivery.Plan, error) {
	var plan delivery.Plan
	var id, workspaceID, eventID, channel, status, raw, nextAttemptAt, lastErrorCode, outboxMessageID, leaseOwner, leaseExpiresAt, createdAt, updatedAt string
	var attemptCount int
	var fencingToken int64
	if err := row.Scan(&id, &workspaceID, &eventID, &channel, &status, &raw, &attemptCount, &nextAttemptAt, &lastErrorCode, &outboxMessageID, &leaseOwner, &leaseExpiresAt, &fencingToken, &createdAt, &updatedAt); err != nil {
		return plan, err
	}
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		return plan, fmt.Errorf("decode notification channel plan: %w", err)
	}
	plan.ID, plan.WorkspaceID, plan.EventID, plan.Channel, plan.Status = id, notification.WorkspaceID(workspaceID), eventID, channel, status
	plan.AttemptCount, plan.NextAttemptAt, plan.LastErrorCode, plan.OutboxMessageID = attemptCount, nextAttemptAt, lastErrorCode, outboxMessageID
	plan.LeaseOwner, plan.LeaseExpiresAt, plan.FencingToken = leaseOwner, leaseExpiresAt, fencingToken
	plan.CreatedAt, plan.UpdatedAt = createdAt, updatedAt
	return plan, nil
}
