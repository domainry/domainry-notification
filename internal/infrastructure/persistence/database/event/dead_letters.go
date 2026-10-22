package eventstore

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-orm/query"
)

// DeadLetter deliberately excludes message content and recipient identifiers.
type DeadLetter struct {
	ID            int64
	EventID       string
	Source        string
	AttemptCount  int
	LastErrorCode string
	UpdatedAt     string
}

// ListDeadLetters uses the workspace/id index for bounded, stable pagination.
func (s *Store) ListDeadLetters(ctx context.Context, workspaceID notification.WorkspaceID, afterID string, limit int) ([]DeadLetter, string, error) {
	if workspaceID == "" || limit <= 0 || limit > 100 || len(afterID) > 256 {
		return nil, "", fmt.Errorf("notification dead-letter query is invalid")
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	predicate := query.Equal("status", "failed")
	if afterID != "" {
		id, err := strconv.ParseInt(afterID, 10, 64)
		if err != nil || id <= 0 {
			return nil, "", fmt.Errorf("notification dead-letter cursor is invalid")
		}
		predicate = query.And(predicate, query.GreaterThan("id", id))
	}
	statement, args, err := query.NewWorkspaceSelectBuilder(s.Renderer, "_notification_events", workspaceID.String()).
		Columns("id", "event_id", "source", "attempt_count", "last_error_code", "updated_at").
		Where(predicate).OrderBy(query.Ascending("id")).Limit(limit + 1).Build()
	if err != nil {
		return nil, "", err
	}
	rows, err := s.Database.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list notification dead letters: %w", err)
	}
	defer rows.Close()
	items := make([]DeadLetter, 0, limit)
	for rows.Next() {
		var item DeadLetter
		var updatedAt int64
		if err := rows.Scan(&item.ID, &item.EventID, &item.Source, &item.AttemptCount, &item.LastErrorCode, &updatedAt); err != nil {
			return nil, "", err
		}
		item.UpdatedAt = notification.MillisTimestamp(updatedAt)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	if len(items) <= limit {
		return items, "", nil
	}
	nextCursor := strconv.FormatInt(items[limit-1].ID, 10)
	return items[:limit], nextCursor, nil
}

// RedriveDeadLetter is a compare-and-set transition. A duplicate request or
// concurrent operator cannot requeue a successfully materialized event.
func (s *Store) RedriveDeadLetter(ctx context.Context, workspaceID notification.WorkspaceID, eventID, now string) (bool, bool, error) {
	eventID, now = strings.TrimSpace(eventID), strings.TrimSpace(now)
	if workspaceID == "" || eventID == "" || len(eventID) > 256 || notification.TimestampMillis(now) <= 0 {
		return false, false, fmt.Errorf("notification dead-letter redrive identity is invalid")
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	statement, args, err := query.NewWorkspaceUpdateBuilder(s.Renderer, "_notification_events", workspaceID.String()).
		Set("status", "queued").Set("attempt_count", 0).Set("next_attempt_at", int64(0)).Set("last_error_code", "").
		Set("updated_at", notification.TimestampMillis(now)).
		Where(query.And(query.Equal("event_id", eventID), query.Equal("status", "failed"))).Build()
	if err != nil {
		return false, false, err
	}
	result, err := s.Database.ExecContext(ctx, statement, args...)
	if err != nil {
		return false, false, fmt.Errorf("redrive notification dead letter: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, false, err
	}
	if count == 1 {
		return true, true, nil
	}
	_, found, err := s.eventByID(ctx, workspaceID, eventID)
	return false, found, err
}
