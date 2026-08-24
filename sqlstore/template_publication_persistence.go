package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/domainry/domainry-notification/template"
)

var _ template.Store = (*Store)(nil)
var _ template.RevisionStore = (*Store)(nil)

var publicationRequestColumns = []string{
	"id", "template_key", "snapshot_json", "candidate_hash", "draft_updated_at", "status", "scheduled_for", "requested_by", "requested_at",
	"reviewed_by", "reviewed_at", "published_version", "failure", "lease_owner", "lease_expires_at", "fencing_token", "updated_at",
}

func (s *Store) ListPublicationRequests(ctx context.Context, templateKey string) ([]template.PublicationRequest, error) {
	query := "SELECT " + s.columns(publicationRequestColumns) + " FROM " + s.dialect.Table("notification_template_publication_requests")
	args := []any{}
	if templateKey = strings.TrimSpace(templateKey); templateKey != "" {
		query += " WHERE " + s.dialect.Identifier("template_key") + " = " + s.dialect.Placeholder(1)
		args = append(args, templateKey)
	}
	query += " ORDER BY " + s.dialect.Identifier("requested_at") + " DESC"
	rows, err := s.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list notification publication requests: %w", err)
	}
	defer rows.Close()
	values := []template.PublicationRequest{}
	for rows.Next() {
		value, scanErr := scanPublicationRequest(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) GetPublicationRequest(ctx context.Context, requestID string) (template.PublicationRequest, bool, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return template.PublicationRequest{}, false, fmt.Errorf("notification publication request id is required")
	}
	return s.publicationRequestByID(ctx, s.database, requestID)
}

func (s *Store) CreatePublicationRequest(ctx context.Context, request template.PublicationRequest) error {
	request.ID, request.TemplateKey = strings.TrimSpace(request.ID), strings.TrimSpace(request.TemplateKey)
	if request.ID == "" || request.TemplateKey == "" {
		return fmt.Errorf("notification publication request identity is required")
	}
	snapshot, err := json.Marshal(request.Snapshot)
	if err != nil {
		return fmt.Errorf("encode notification publication snapshot: %w", err)
	}
	tx, err := s.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, s.dialect.Insert("notification_template_publication_locks", []string{"template_key", "request_id", "created_at"}),
		request.TemplateKey, request.ID, request.RequestedAt)
	if err != nil {
		_ = tx.Rollback()
		open, inspectErr := s.HasOpenPublicationRequest(ctx, request.TemplateKey)
		if inspectErr == nil && open {
			return template.ErrPublicationConflict
		}
		return fmt.Errorf("acquire notification publication lock: %w", err)
	}
	_, err = tx.ExecContext(ctx, s.dialect.Insert("notification_template_publication_requests", publicationRequestColumns), request.ID, request.TemplateKey,
		string(snapshot), request.CandidateHash, request.DraftUpdatedAt, string(request.Status), request.ScheduledFor, request.RequestedBy, request.RequestedAt,
		request.ReviewedBy, request.ReviewedAt, request.PublishedVersion, request.Failure, request.LeaseOwner, request.LeaseExpiresAt, request.FencingToken, request.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert notification publication request: %w", err)
	}
	return tx.Commit()
}

func (s *Store) TransitionPublicationRequest(ctx context.Context, requestID string, expectedStatus template.PublicationStatus, transition template.PublicationTransition) (template.PublicationRequest, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return template.PublicationRequest{}, fmt.Errorf("notification publication request id is required")
	}
	if expectedStatus == template.PublicationPublishing && (strings.TrimSpace(transition.ExpectedLeaseOwner) == "" || transition.ExpectedFencingToken < 1) {
		return template.PublicationRequest{}, fmt.Errorf("notification publication terminal transition requires lease owner and fencing token")
	}
	tx, err := s.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return template.PublicationRequest{}, err
	}
	defer tx.Rollback()
	query := "UPDATE " + s.dialect.Table("notification_template_publication_requests") + " SET " + s.dialect.Identifier("status") + " = " + s.dialect.Placeholder(1) +
		", " + s.dialect.Identifier("scheduled_for") + " = " + s.dialect.Placeholder(2) + ", " + s.dialect.Identifier("reviewed_by") + " = " + s.dialect.Placeholder(3) +
		", " + s.dialect.Identifier("reviewed_at") + " = " + s.dialect.Placeholder(4) + ", " + s.dialect.Identifier("published_version") + " = " + s.dialect.Placeholder(5) +
		", " + s.dialect.Identifier("failure") + " = " + s.dialect.Placeholder(6) + ", " + s.dialect.Identifier("lease_owner") + " = '', " +
		s.dialect.Identifier("lease_expires_at") + " = '', " + s.dialect.Identifier("updated_at") + " = " + s.dialect.Placeholder(7) +
		" WHERE " + s.dialect.Identifier("id") + " = " + s.dialect.Placeholder(8) + " AND " + s.dialect.Identifier("status") + " = " + s.dialect.Placeholder(9)
	args := []any{string(transition.Status), transition.ScheduledFor, transition.ReviewedBy, transition.ReviewedAt, transition.PublishedVersion, transition.Failure,
		transition.UpdatedAt, requestID, string(expectedStatus)}
	if transition.ExpectedLeaseOwner = strings.TrimSpace(transition.ExpectedLeaseOwner); transition.ExpectedLeaseOwner != "" && transition.ExpectedFencingToken > 0 {
		query += " AND " + s.dialect.Identifier("lease_owner") + " = " + s.dialect.Placeholder(10) + " AND " + s.dialect.Identifier("fencing_token") + " = " + s.dialect.Placeholder(11)
		args = append(args, transition.ExpectedLeaseOwner, transition.ExpectedFencingToken)
	}
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return template.PublicationRequest{}, fmt.Errorf("transition notification publication request: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return template.PublicationRequest{}, err
	}
	if count != 1 {
		return template.PublicationRequest{}, template.ErrPublicationConflict
	}
	if !publicationStatusOpen(transition.Status) {
		query = "DELETE FROM " + s.dialect.Table("notification_template_publication_locks") + " WHERE " + s.dialect.Identifier("request_id") + " = " + s.dialect.Placeholder(1)
		if _, err := tx.ExecContext(ctx, query, requestID); err != nil {
			return template.PublicationRequest{}, fmt.Errorf("release notification publication lock: %w", err)
		}
	}
	value, found, err := s.publicationRequestByID(ctx, tx, requestID)
	if err != nil {
		return template.PublicationRequest{}, err
	}
	if !found {
		return template.PublicationRequest{}, template.ErrPublicationNotFound
	}
	if err := tx.Commit(); err != nil {
		return template.PublicationRequest{}, err
	}
	return value, nil
}

func (s *Store) ListDuePublicationRequests(ctx context.Context, now, staleBefore string, limit int) ([]template.PublicationRequest, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	query := "SELECT " + s.columns(publicationRequestColumns) + " FROM " + s.dialect.Table("notification_template_publication_requests") + " WHERE (" +
		s.dialect.Identifier("status") + " = 'scheduled' AND " + s.dialect.Identifier("scheduled_for") + " <= " + s.dialect.Placeholder(1) + ") OR (" +
		s.dialect.Identifier("status") + " = 'publishing' AND " + s.dialect.Identifier("lease_expires_at") + " <= " + s.dialect.Placeholder(2) + ") ORDER BY " +
		s.dialect.Identifier("scheduled_for") + ", " + s.dialect.Identifier("requested_at") + " LIMIT " + fmt.Sprint(limit)
	rows, err := s.database.QueryContext(ctx, query, strings.TrimSpace(now), strings.TrimSpace(staleBefore))
	if err != nil {
		return nil, fmt.Errorf("list due notification publication requests: %w", err)
	}
	defer rows.Close()
	values := []template.PublicationRequest{}
	for rows.Next() {
		value, scanErr := scanPublicationRequest(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) ClaimPublicationRequest(ctx context.Context, requestID, owner, now, expiresAt string) (template.PublicationRequest, bool, error) {
	requestID, owner, now, expiresAt = strings.TrimSpace(requestID), strings.TrimSpace(owner), strings.TrimSpace(now), strings.TrimSpace(expiresAt)
	if requestID == "" || owner == "" || now == "" || expiresAt == "" {
		return template.PublicationRequest{}, false, fmt.Errorf("notification publication claim identity and lease are required")
	}
	query := "UPDATE " + s.dialect.Table("notification_template_publication_requests") + " SET " + s.dialect.Identifier("status") + " = 'publishing', " +
		s.dialect.Identifier("lease_owner") + " = " + s.dialect.Placeholder(1) + ", " + s.dialect.Identifier("lease_expires_at") + " = " + s.dialect.Placeholder(2) +
		", " + s.dialect.Identifier("fencing_token") + " = " + s.dialect.Identifier("fencing_token") + " + 1, " + s.dialect.Identifier("updated_at") + " = " + s.dialect.Placeholder(3) +
		" WHERE " + s.dialect.Identifier("id") + " = " + s.dialect.Placeholder(4) + " AND ((" + s.dialect.Identifier("status") + " = 'scheduled' AND " +
		s.dialect.Identifier("scheduled_for") + " <= " + s.dialect.Placeholder(5) + ") OR (" + s.dialect.Identifier("status") + " = 'publishing' AND " +
		s.dialect.Identifier("lease_expires_at") + " <= " + s.dialect.Placeholder(6) + "))"
	result, err := s.database.ExecContext(ctx, query, owner, expiresAt, now, requestID, now, now)
	if err != nil {
		return template.PublicationRequest{}, false, fmt.Errorf("claim notification publication request: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return template.PublicationRequest{}, false, err
	}
	return s.GetPublicationRequest(ctx, requestID)
}

func (s *Store) HasOpenPublicationRequest(ctx context.Context, templateKey string) (bool, error) {
	var count int
	query := "SELECT COUNT(*) FROM " + s.dialect.Table("notification_template_publication_locks") + " WHERE " +
		s.dialect.Identifier("template_key") + " = " + s.dialect.Placeholder(1)
	err := s.database.QueryRowContext(ctx, query, strings.TrimSpace(templateKey)).Scan(&count)
	return count > 0, err
}

func (s *Store) publicationRequestByID(ctx context.Context, queryer Queryer, requestID string) (template.PublicationRequest, bool, error) {
	query := "SELECT " + s.columns(publicationRequestColumns) + " FROM " + s.dialect.Table("notification_template_publication_requests") +
		" WHERE " + s.dialect.Identifier("id") + " = " + s.dialect.Placeholder(1)
	value, err := scanPublicationRequest(queryer.QueryRowContext(ctx, query, requestID))
	if errors.Is(err, sql.ErrNoRows) {
		return template.PublicationRequest{}, false, nil
	}
	return value, err == nil, err
}

func scanPublicationRequest(row scanner) (template.PublicationRequest, error) {
	var value template.PublicationRequest
	var snapshot string
	if err := row.Scan(&value.ID, &value.TemplateKey, &snapshot, &value.CandidateHash, &value.DraftUpdatedAt, &value.Status, &value.ScheduledFor,
		&value.RequestedBy, &value.RequestedAt, &value.ReviewedBy, &value.ReviewedAt, &value.PublishedVersion, &value.Failure, &value.LeaseOwner,
		&value.LeaseExpiresAt, &value.FencingToken, &value.UpdatedAt); err != nil {
		return value, err
	}
	if err := json.Unmarshal([]byte(snapshot), &value.Snapshot); err != nil {
		return value, fmt.Errorf("decode notification publication snapshot: %w", err)
	}
	return value, nil
}

func publicationStatusOpen(status template.PublicationStatus) bool {
	return status == template.PublicationPending || status == template.PublicationScheduled || status == template.PublicationPublishing
}
