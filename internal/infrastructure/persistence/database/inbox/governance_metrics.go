package inboxstore

import (
	"context"
	"fmt"
	"strings"

	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	builder "github.com/domainry/domainry-orm/query"
)

var _ inbox.MetricsStore = (*Store)(nil)

func (s *Store) GovernanceMetrics(ctx context.Context, workspaceID notification.WorkspaceID, since string) (inbox.GovernanceMetrics, error) {
	since = strings.TrimSpace(since)
	if workspaceID == "" || since == "" {
		return inbox.GovernanceMetrics{}, fmt.Errorf("notification governance metrics workspace and lower bound are required")
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	result := inbox.GovernanceMetrics{Since: since, GeneratedAt: notification.Timestamp(s.clock.Now())}
	predicate := builder.And(builder.Equal("workspace_id", workspaceID.String()), builder.GreaterThanOrEqual("last_occurred_at", since))
	var err error
	if result.Summary, err = s.inboxAggregateSummary(ctx, workspaceID.String(), predicate); err != nil {
		return result, err
	}
	result.Summary.Key = "all"
	dimensions := []struct {
		column string
		value  *[]inbox.Aggregate
	}{{"event_type", &result.ByEventType}, {"category", &result.ByCategory}, {"severity", &result.BySeverity}, {"source", &result.BySource}, {"surface", &result.BySurface}}
	for _, dimension := range dimensions {
		*dimension.value, err = s.inboxAggregateRows(ctx, workspaceID.String(), predicate, dimension.column)
		if err != nil {
			return result, err
		}
	}
	if result.Failures, err = s.eventFailureMetrics(ctx, workspaceID, since); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Store) eventFailureMetrics(ctx context.Context, workspaceID notification.WorkspaceID, since string) (inbox.FailureMetrics, error) {
	result := inbox.FailureMetrics{}
	predicate := builder.And(builder.Equal("workspace_id", workspaceID.String()), builder.GreaterThanOrEqual("occurred_at", since))
	projections := []builder.Projection{
		builder.Project(builder.CountAll()),
		builder.Project(builder.Coalesce(builder.Sum(builder.CaseWhen(builder.Equal("disposition", "retry_scheduled"), 1).Else(0)), builder.Value(0))),
		builder.Project(builder.Coalesce(builder.Sum(builder.CaseWhen(builder.Equal("disposition", "dead_letter"), 1).Else(0)), builder.Value(0))),
	}
	statement, args, err := builder.NewWorkspaceSelectBuilder(s.Renderer, "notification_event_failures", workspaceID.String()).Projections(projections...).Where(predicate).Build()
	if err != nil {
		return result, err
	}
	if err := s.Database.QueryRowContext(ctx, statement, args...).Scan(&result.Total, &result.RetryScheduled, &result.DeadLetter); err != nil {
		return result, fmt.Errorf("aggregate notification event failures: %w", err)
	}
	dimensions := []struct {
		column string
		value  *[]inbox.FailureAggregate
	}{{"stage", &result.ByStage}, {"error_code", &result.ByErrorCode}}
	for _, dimension := range dimensions {
		statement, args, err := builder.NewWorkspaceSelectBuilder(s.Renderer, "notification_event_failures", workspaceID.String()).Projections(builder.Project(builder.Column(dimension.column)), builder.Project(builder.CountAll())).Where(predicate).GroupBy(builder.Column(dimension.column)).OrderBy(builder.DescendingExpression(builder.CountAll()), builder.Ascending(dimension.column)).Build()
		if err != nil {
			return result, err
		}
		rows, err := s.Database.QueryContext(ctx, statement, args...)
		if err != nil {
			return result, fmt.Errorf("aggregate notification event failures by %s: %w", dimension.column, err)
		}
		values := []inbox.FailureAggregate{}
		for rows.Next() {
			var value inbox.FailureAggregate
			if err := rows.Scan(&value.Key, &value.Count); err != nil {
				_ = rows.Close()
				return result, err
			}
			values = append(values, value)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return result, err
		}
		_ = rows.Close()
		*dimension.value = values
	}
	return result, nil
}

func inboxAggregateProjections() []builder.Projection {
	return []builder.Projection{
		builder.Project(builder.CountAll()),
		builder.Project(builder.Coalesce(builder.Sum(builder.Column("occurrence_count")), builder.Value(0))),
		builder.Project(builder.Coalesce(builder.Sum(builder.CaseWhen(builder.Equal("read_at", ""), 1).Else(0)), builder.Value(0))),
		builder.Project(builder.Coalesce(builder.Sum(builder.CaseWhen(builder.Equal("action_state", "open"), 1).Else(0)), builder.Value(0))),
		builder.Project(builder.Coalesce(builder.Sum(builder.CaseWhen(builder.Equal("alert_state", "firing"), 1).Else(0)), builder.Value(0))),
	}
}

func (s *Store) inboxAggregateSummary(ctx context.Context, workspaceID string, predicate builder.Predicate) (inbox.Aggregate, error) {
	value := inbox.Aggregate{}
	statement, args, err := builder.NewWorkspaceSelectBuilder(s.Renderer, "notification_inbox_items", workspaceID).Projections(inboxAggregateProjections()...).Where(predicate).Build()
	if err != nil {
		return value, err
	}
	err = s.Database.QueryRowContext(ctx, statement, args...).
		Scan(&value.Items, &value.Occurrences, &value.Unread, &value.ActionRequired, &value.ActiveAlerts)
	if err != nil {
		return value, fmt.Errorf("aggregate notification inbox metrics: %w", err)
	}
	return value, nil
}

func (s *Store) inboxAggregateRows(ctx context.Context, workspaceID string, predicate builder.Predicate, column string) ([]inbox.Aggregate, error) {
	projections := append([]builder.Projection{builder.Project(builder.Column(column))}, inboxAggregateProjections()...)
	statement, args, err := builder.NewWorkspaceSelectBuilder(s.Renderer, "notification_inbox_items", workspaceID).Projections(projections...).Where(predicate).GroupBy(builder.Column(column)).OrderBy(builder.DescendingExpression(builder.CountAll()), builder.Ascending(column)).Build()
	if err != nil {
		return nil, err
	}
	rows, err := s.Database.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("aggregate notification inbox metrics by %s: %w", column, err)
	}
	defer rows.Close()
	values := []inbox.Aggregate{}
	for rows.Next() {
		var value inbox.Aggregate
		if err := rows.Scan(&value.Key, &value.Items, &value.Occurrences, &value.Unread, &value.ActionRequired, &value.ActiveAlerts); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
