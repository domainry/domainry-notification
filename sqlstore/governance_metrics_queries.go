package sqlstore

import (
	"context"
	"fmt"
	"strings"

	"github.com/domainry/domainry-notification"
	"github.com/domainry/domainry-notification/inbox"
)

var _ inbox.MetricsStore = (*Store)(nil)

func (s *Store) GovernanceMetrics(ctx context.Context, workspaceID notification.WorkspaceID, since string) (inbox.GovernanceMetrics, error) {
	since = strings.TrimSpace(since)
	if workspaceID == "" || since == "" {
		return inbox.GovernanceMetrics{}, fmt.Errorf("notification governance metrics workspace and lower bound are required")
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	result := inbox.GovernanceMetrics{Since: since, GeneratedAt: notification.Timestamp(s.clock.Now())}
	where := s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(1) + " AND " + s.dialect.Identifier("last_occurred_at") + " >= " + s.dialect.Placeholder(2)
	args := []any{workspaceID.String(), since}
	var err error
	if result.Summary, err = s.inboxAggregateSummary(ctx, where, args); err != nil {
		return result, err
	}
	result.Summary.Key = "all"
	dimensions := []struct {
		column string
		value  *[]inbox.Aggregate
	}{{"event_type", &result.ByEventType}, {"category", &result.ByCategory}, {"severity", &result.BySeverity}, {"source", &result.BySource}, {"surface", &result.BySurface}}
	for _, dimension := range dimensions {
		*dimension.value, err = s.inboxAggregateRows(ctx, where, args, dimension.column)
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
	table := s.dialect.Table("notification_event_failures")
	where := s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(1) + " AND " + s.dialect.Identifier("occurred_at") + " >= " + s.dialect.Placeholder(2)
	projection := "COUNT(*), COALESCE(SUM(CASE WHEN " + s.dialect.Identifier("disposition") + " = 'retry_scheduled' THEN 1 ELSE 0 END), 0), " +
		"COALESCE(SUM(CASE WHEN " + s.dialect.Identifier("disposition") + " = 'dead_letter' THEN 1 ELSE 0 END), 0)"
	if err := s.database.QueryRowContext(ctx, "SELECT "+projection+" FROM "+table+" WHERE "+where, workspaceID.String(), since).Scan(&result.Total, &result.RetryScheduled, &result.DeadLetter); err != nil {
		return result, fmt.Errorf("aggregate notification event failures: %w", err)
	}
	dimensions := []struct {
		column string
		value  *[]inbox.FailureAggregate
	}{{"stage", &result.ByStage}, {"error_code", &result.ByErrorCode}}
	for _, dimension := range dimensions {
		identifier := s.dialect.Identifier(dimension.column)
		rows, err := s.database.QueryContext(ctx, "SELECT "+identifier+", COUNT(*) FROM "+table+" WHERE "+where+" GROUP BY "+identifier+" ORDER BY COUNT(*) DESC, "+identifier+" ASC", workspaceID.String(), since)
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

func (s *Store) inboxAggregateProjection() string {
	return "COUNT(*), COALESCE(SUM(" + s.dialect.Identifier("occurrence_count") + "), 0), " +
		"COALESCE(SUM(CASE WHEN " + s.dialect.Identifier("read_at") + " = '' THEN 1 ELSE 0 END), 0), " +
		"COALESCE(SUM(CASE WHEN " + s.dialect.Identifier("action_state") + " = 'open' THEN 1 ELSE 0 END), 0), " +
		"COALESCE(SUM(CASE WHEN " + s.dialect.Identifier("alert_state") + " = 'firing' THEN 1 ELSE 0 END), 0)"
}

func (s *Store) inboxAggregateSummary(ctx context.Context, where string, args []any) (inbox.Aggregate, error) {
	value := inbox.Aggregate{}
	err := s.database.QueryRowContext(ctx, "SELECT "+s.inboxAggregateProjection()+" FROM "+s.dialect.Table("notification_inbox_items")+" WHERE "+where, args...).
		Scan(&value.Items, &value.Occurrences, &value.Unread, &value.ActionRequired, &value.ActiveAlerts)
	if err != nil {
		return value, fmt.Errorf("aggregate notification inbox metrics: %w", err)
	}
	return value, nil
}

func (s *Store) inboxAggregateRows(ctx context.Context, where string, args []any, column string) ([]inbox.Aggregate, error) {
	identifier := s.dialect.Identifier(column)
	statement := "SELECT " + identifier + ", " + s.inboxAggregateProjection() + " FROM " + s.dialect.Table("notification_inbox_items") + " WHERE " + where +
		" GROUP BY " + identifier + " ORDER BY COUNT(*) DESC, " + identifier + " ASC"
	rows, err := s.database.QueryContext(ctx, statement, args...)
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
