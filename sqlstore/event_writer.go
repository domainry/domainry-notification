package sqlstore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/domainry/domainry-notification"
	"github.com/domainry/domainry-notification/inbox"
)

var eventColumns = []string{
	"id", "workspace_id", "source", "source_event_id", "status", "payload_json", "attempt_count",
	"next_attempt_at", "last_error_code", "lease_owner", "lease_expires_at", "fencing_token",
	"occurred_at", "created_at", "updated_at",
}

// InsertEvent writes a compiled event through the caller's executor. Passing a
// transaction preserves atomicity with Workflow, Record, Report, Automation,
// or Integration state. Commit, rollback, and after-commit wakeup remain owned
// by that caller.
func (s *Store) InsertEvent(ctx context.Context, executor Executor, event inbox.Event) error {
	if executor == nil {
		return fmt.Errorf("notification event executor is required")
	}
	if event.WorkspaceID == "" || event.ID == "" {
		return fmt.Errorf("notification event identity is required")
	}
	if err := s.queueScopes.Register(ctx, executor, notification.WorkInboxEvent, event.WorkspaceID, event.UpdatedAt); err != nil {
		return fmt.Errorf("register notification inbox queue scope: %w", err)
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode notification event: %w", err)
	}
	ctx = s.workspaceScope.Context(ctx, event.WorkspaceID)
	_, err = executor.ExecContext(ctx, s.dialect.Insert("notification_events", eventColumns), eventValues(event, string(raw))...)
	if err != nil {
		return fmt.Errorf("insert notification event: %w", err)
	}
	return nil
}

func eventValues(event inbox.Event, raw string) []any {
	return []any{
		event.ID, event.WorkspaceID.String(), event.Source, event.SourceEventID, string(event.Status), raw, event.AttemptCount,
		event.NextAttemptAt, event.LastErrorCode, event.LeaseOwner, event.LeaseExpiresAt, event.FencingToken,
		event.OccurredAt, event.CreatedAt, event.UpdatedAt,
	}
}
