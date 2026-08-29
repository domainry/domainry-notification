package eventstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/domainry/domainry-notification/internal/domain/delivery/service"
	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

var failureCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,159}$`)
var failureStages = map[string]bool{"event_parse": true, "render": true, "audience_resolution": true, "materialization": true}

func (s *Store) Enqueue(ctx context.Context, event inbox.Event) (inbox.Event, bool, error) {
	if event.WorkspaceID == "" {
		return inbox.Event{}, false, fmt.Errorf("notification event workspace is required")
	}
	ctx = s.workspaceScope.Context(ctx, event.WorkspaceID)
	tx, err := s.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return inbox.Event{}, false, fmt.Errorf("begin notification event enqueue: %w", err)
	}
	defer tx.Rollback()
	if err = s.InsertEvent(ctx, tx, event); err == nil {
		err = tx.Commit()
	}
	if err == nil {
		return event, true, nil
	}
	_ = tx.Rollback()
	existing, found, readErr := s.eventBySource(ctx, event.WorkspaceID, event.Source, event.SourceEventID)
	if readErr != nil {
		return inbox.Event{}, false, fmt.Errorf("enqueue notification event: %w", err)
	}
	if found {
		if !sameIngest(existing, event) {
			return inbox.Event{}, false, ErrIdempotencyConflict
		}
		return existing, false, nil
	}
	return inbox.Event{}, false, fmt.Errorf("enqueue notification event: %w", err)
}

func sameIngest(left, right inbox.Event) bool {
	left.Status, right.Status = "", ""
	left.AttemptCount, right.AttemptCount = 0, 0
	left.NextAttemptAt, right.NextAttemptAt = "", ""
	left.LastErrorCode, right.LastErrorCode = "", ""
	left.LeaseOwner, right.LeaseOwner = "", ""
	left.LeaseExpiresAt, right.LeaseExpiresAt = "", ""
	left.FencingToken, right.FencingToken = 0, 0
	left.CreatedAt, right.CreatedAt = "", ""
	left.UpdatedAt, right.UpdatedAt = "", ""
	for index := range left.ChannelPlans {
		clearPlanLifecycle(&left.ChannelPlans[index])
	}
	for index := range right.ChannelPlans {
		clearPlanLifecycle(&right.ChannelPlans[index])
	}
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}

func clearPlanLifecycle(plan *delivery.Plan) {
	plan.Status = ""
	plan.AttemptCount = 0
	plan.NextAttemptAt = ""
	plan.LastErrorCode = ""
	plan.OutboxMessageID = ""
	plan.LeaseOwner = ""
	plan.LeaseExpiresAt = ""
	plan.FencingToken = 0
	plan.CreatedAt = ""
	plan.UpdatedAt = ""
}

func (s *Store) ListDue(ctx context.Context, now string, limit int) ([]inbox.Event, error) {
	if limit <= 0 {
		limit = 25
	}
	workspaces, err := s.queueScopes.Workspaces(ctx, s.database, notification.WorkInboxEvent, workspaceScanLimit(limit))
	if err != nil {
		return nil, err
	}
	values := []inbox.Event{}
	for _, workspaceID := range workspaces {
		workspaceValues, listErr := s.listDueForWorkspace(s.workspaceScope.Context(ctx, workspaceID), workspaceID, now, limit)
		if listErr != nil {
			return nil, listErr
		}
		values = append(values, workspaceValues...)
	}
	sort.SliceStable(values, func(i, j int) bool {
		if values[i].OccurredAt == values[j].OccurredAt {
			return values[i].ID < values[j].ID
		}
		return values[i].OccurredAt < values[j].OccurredAt
	})
	if len(values) > limit {
		values = values[:limit]
	}
	return values, nil
}

func (s *Store) listDueForWorkspace(ctx context.Context, workspaceID notification.WorkspaceID, now string, limit int) ([]inbox.Event, error) {
	query := "SELECT " + s.columns(eventColumns) + " FROM " + s.dialect.Table("notification_events") +
		" WHERE " + s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(1) + " AND ((" + s.dialect.Identifier("status") + " = 'queued' AND (" + s.dialect.Identifier("next_attempt_at") + " = '' OR " + s.dialect.Identifier("next_attempt_at") + " <= " + s.dialect.Placeholder(2) + "))" +
		" OR (" + s.dialect.Identifier("status") + " = 'processing' AND " + s.dialect.Identifier("lease_expires_at") + " <= " + s.dialect.Placeholder(3) + "))" +
		" ORDER BY " + s.dialect.Identifier("occurred_at") + " ASC LIMIT " + fmt.Sprint(limit)
	rows, err := s.database.QueryContext(ctx, query, workspaceID.String(), now, now)
	if err != nil {
		return nil, fmt.Errorf("list due notification events: %w", err)
	}
	defer rows.Close()
	values := []inbox.Event{}
	for rows.Next() {
		value, scanErr := scanEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) Claim(ctx context.Context, workspaceID notification.WorkspaceID, eventID, owner, now, expiresAt string) (inbox.Event, bool, error) {
	eventID, owner, now, expiresAt = strings.TrimSpace(eventID), strings.TrimSpace(owner), strings.TrimSpace(now), strings.TrimSpace(expiresAt)
	if workspaceID == "" || eventID == "" || owner == "" || now == "" || expiresAt == "" {
		return inbox.Event{}, false, fmt.Errorf("notification event claim identity and lease are required")
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	query := "UPDATE " + s.dialect.Table("notification_events") + " SET " +
		s.dialect.Identifier("status") + " = 'processing', " + s.dialect.Identifier("lease_owner") + " = " + s.dialect.Placeholder(1) + ", " +
		s.dialect.Identifier("lease_expires_at") + " = " + s.dialect.Placeholder(2) + ", " + s.dialect.Identifier("fencing_token") + " = " + s.dialect.Identifier("fencing_token") + " + 1, " +
		s.dialect.Identifier("updated_at") + " = " + s.dialect.Placeholder(3) + " WHERE " + s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(4) +
		" AND " + s.dialect.Identifier("id") + " = " + s.dialect.Placeholder(5) +
		" AND ((" + s.dialect.Identifier("status") + " = 'queued' AND (" + s.dialect.Identifier("next_attempt_at") + " = '' OR " + s.dialect.Identifier("next_attempt_at") + " <= " + s.dialect.Placeholder(6) + ")) OR (" +
		s.dialect.Identifier("status") + " = 'processing' AND " + s.dialect.Identifier("lease_expires_at") + " <= " + s.dialect.Placeholder(7) + "))"
	result, err := s.database.ExecContext(ctx, query, owner, expiresAt, now, workspaceID.String(), eventID, now, now)
	if err != nil {
		return inbox.Event{}, false, fmt.Errorf("claim notification event: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return inbox.Event{}, false, err
	}
	return s.eventByID(ctx, workspaceID, eventID)
}

func (s *Store) Retry(ctx context.Context, event inbox.Event, stage, errorCode, nextAttemptAt, updatedAt string) error {
	return s.transitionFailure(ctx, event, inbox.EventQueued, stage, errorCode, nextAttemptAt, updatedAt)
}

func (s *Store) Fail(ctx context.Context, event inbox.Event, stage, errorCode, updatedAt string) error {
	return s.transitionFailure(ctx, event, inbox.EventFailed, stage, errorCode, "", updatedAt)
}

func (s *Store) transitionFailure(ctx context.Context, event inbox.Event, status inbox.EventStatus, stage, errorCode, nextAttemptAt, updatedAt string) error {
	stage, errorCode = strings.TrimSpace(stage), strings.TrimSpace(errorCode)
	if !failureStages[stage] || !failureCodePattern.MatchString(errorCode) {
		return fmt.Errorf("notification event failure evidence requires a safe stage and error code")
	}
	ctx = s.workspaceScope.Context(ctx, event.WorkspaceID)
	tx, err := s.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	query := "UPDATE " + s.dialect.Table("notification_events") + " SET " + s.dialect.Identifier("status") + " = " + s.dialect.Placeholder(1) + ", " +
		s.dialect.Identifier("attempt_count") + " = " + s.dialect.Identifier("attempt_count") + " + 1, " + s.dialect.Identifier("next_attempt_at") + " = " + s.dialect.Placeholder(2) + ", " +
		s.dialect.Identifier("last_error_code") + " = " + s.dialect.Placeholder(3) + ", " + s.dialect.Identifier("lease_owner") + " = '', " + s.dialect.Identifier("lease_expires_at") + " = '', " +
		s.dialect.Identifier("updated_at") + " = " + s.dialect.Placeholder(4) + " WHERE " + s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(5) +
		" AND " + s.dialect.Identifier("id") + " = " + s.dialect.Placeholder(6) + " AND " + s.dialect.Identifier("status") + " = 'processing' AND " +
		s.dialect.Identifier("lease_owner") + " = " + s.dialect.Placeholder(7) + " AND " + s.dialect.Identifier("fencing_token") + " = " + s.dialect.Placeholder(8)
	result, err := tx.ExecContext(ctx, query, string(status), strings.TrimSpace(nextAttemptAt), errorCode, strings.TrimSpace(updatedAt), event.WorkspaceID.String(), event.ID, event.LeaseOwner, event.FencingToken)
	if err != nil {
		return fmt.Errorf("transition notification event failure: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrLeaseLost
	}
	disposition, retryable := "retry_scheduled", 1
	if status == inbox.EventFailed {
		disposition, retryable = "dead_letter", 0
	}
	failure := inbox.EventFailure{
		ID: failureID(event.WorkspaceID, event.ID, event.FencingToken), WorkspaceID: event.WorkspaceID,
		EventID: event.ID, EventType: event.EventType, Source: event.Source, SourceEventID: event.SourceEventID,
		Stage: stage, ErrorCode: errorCode, Attempt: event.AttemptCount + 1, Disposition: disposition, Retryable: retryable == 1,
		NextAttemptAt: strings.TrimSpace(nextAttemptAt), FencingToken: event.FencingToken, OccurredAt: strings.TrimSpace(updatedAt),
	}
	columns := []string{"id", "workspace_id", "event_id", "event_type", "source", "source_event_id", "stage", "error_code", "attempt", "disposition", "retryable", "next_attempt_at", "fencing_token", "occurred_at"}
	_, err = tx.ExecContext(ctx, s.dialect.Insert("notification_event_failures", columns), failure.ID, failure.WorkspaceID.String(), failure.EventID, failure.EventType, failure.Source, failure.SourceEventID, failure.Stage, failure.ErrorCode, failure.Attempt, failure.Disposition, retryable, failure.NextAttemptAt, failure.FencingToken, failure.OccurredAt)
	if err != nil {
		return fmt.Errorf("record notification event failure: %w", err)
	}
	return tx.Commit()
}

func (s *Store) eventByID(ctx context.Context, workspaceID notification.WorkspaceID, eventID string) (inbox.Event, bool, error) {
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	query := "SELECT " + s.columns(eventColumns) + " FROM " + s.dialect.Table("notification_events") + " WHERE " + s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(1) + " AND " + s.dialect.Identifier("id") + " = " + s.dialect.Placeholder(2)
	value, err := scanEvent(s.database.QueryRowContext(ctx, query, workspaceID.String(), strings.TrimSpace(eventID)))
	if errors.Is(err, sql.ErrNoRows) {
		return inbox.Event{}, false, nil
	}
	return value, err == nil, err
}

func (s *Store) eventBySource(ctx context.Context, workspaceID notification.WorkspaceID, source, sourceEventID string) (inbox.Event, bool, error) {
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	query := "SELECT " + s.columns(eventColumns) + " FROM " + s.dialect.Table("notification_events") + " WHERE " + s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(1) + " AND " + s.dialect.Identifier("source") + " = " + s.dialect.Placeholder(2) + " AND " + s.dialect.Identifier("source_event_id") + " = " + s.dialect.Placeholder(3)
	value, err := scanEvent(s.database.QueryRowContext(ctx, query, workspaceID.String(), strings.TrimSpace(source), strings.TrimSpace(sourceEventID)))
	if errors.Is(err, sql.ErrNoRows) {
		return inbox.Event{}, false, nil
	}
	return value, err == nil, err
}

// EventCommitted reports whether the exact source identity has already been
// durably accepted. It keeps replay checks behind Notification's store
// ownership instead of exposing notification_events SQL to a Module host.
func (s *Store) EventCommitted(ctx context.Context, workspaceID notification.WorkspaceID, source, sourceEventID string) (bool, error) {
	_, found, err := s.eventBySource(ctx, workspaceID, source, sourceEventID)
	return found, err
}

type scanner interface{ Scan(...any) error }

func scanEvent(row scanner) (inbox.Event, error) {
	var value inbox.Event
	var id, workspaceID, source, sourceEventID, status, raw string
	var attemptCount int
	var nextAttemptAt, lastErrorCode, leaseOwner, leaseExpiresAt string
	var fencingToken int64
	var occurredAt, createdAt, updatedAt string
	err := row.Scan(&id, &workspaceID, &source, &sourceEventID, &status, &raw, &attemptCount, &nextAttemptAt, &lastErrorCode, &leaseOwner, &leaseExpiresAt, &fencingToken, &occurredAt, &createdAt, &updatedAt)
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return value, fmt.Errorf("decode notification event: %w", err)
	}
	value.ID, value.WorkspaceID, value.Source, value.SourceEventID, value.Status = id, notification.WorkspaceID(workspaceID), source, sourceEventID, inbox.EventStatus(status)
	value.AttemptCount, value.NextAttemptAt, value.LastErrorCode = attemptCount, nextAttemptAt, lastErrorCode
	value.LeaseOwner, value.LeaseExpiresAt, value.FencingToken = leaseOwner, leaseExpiresAt, fencingToken
	value.OccurredAt, value.CreatedAt, value.UpdatedAt = occurredAt, createdAt, updatedAt
	return value, nil
}

func (s *Store) columns(columns []string) string {
	quoted := make([]string, len(columns))
	for index, column := range columns {
		quoted[index] = s.dialect.Identifier(column)
	}
	return strings.Join(quoted, ", ")
}

func workspaceScanLimit(eventLimit int) int { return min(256, max(32, eventLimit*2)) }

func failureID(workspaceID notification.WorkspaceID, eventID string, fencingToken int64) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{workspaceID.String(), eventID, fmt.Sprint(fencingToken), "failure"}, "\x00")))
	return "notification_failure_" + hex.EncodeToString(sum[:16])
}
