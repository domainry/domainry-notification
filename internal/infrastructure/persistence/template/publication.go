package templatestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/domainry/domainry-notification/internal/domain/template/service"
	"github.com/domainry/domainry-orm/builder"
	"github.com/domainry/domainry-orm/sqlhost"
)

var _ template.Store = (*Store)(nil)
var _ template.RevisionStore = (*Store)(nil)

var publicationRequestColumns = []string{
	"id", "template_key", "snapshot_json", "candidate_hash", "draft_updated_at", "status", "scheduled_for", "requested_by", "requested_at",
	"reviewed_by", "reviewed_at", "published_version", "failure", "lease_owner", "lease_expires_at", "fencing_token", "updated_at",
}

func (s *Store) ListPublicationRequests(ctx context.Context, templateKey string) ([]template.PublicationRequest, error) {
	selectBuilder := builder.NewSelectBuilder(s.Renderer, "notification_template_publication_requests").Columns(publicationRequestColumns...).OrderBy(builder.Descending("requested_at"))
	if templateKey = strings.TrimSpace(templateKey); templateKey != "" {
		selectBuilder.Where(builder.Equal("template_key", templateKey))
	}
	query, args, err := selectBuilder.Build()
	if err != nil {
		return nil, err
	}
	rows, err := s.Database.QueryContext(ctx, query, args...)
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
	return s.publicationRequestByID(ctx, s.Database, requestID)
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
	tx, err := s.Database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = s.Insert(ctx, tx, "notification_template_publication_locks", []string{"template_key", "request_id", "created_at"},
		request.TemplateKey, request.ID, request.RequestedAt)
	if err != nil {
		_ = tx.Rollback()
		open, inspectErr := s.HasOpenPublicationRequest(ctx, request.TemplateKey)
		if inspectErr == nil && open {
			return template.ErrPublicationConflict
		}
		return fmt.Errorf("acquire notification publication lock: %w", err)
	}
	_, err = s.Insert(ctx, tx, "notification_template_publication_requests", publicationRequestColumns, request.ID, request.TemplateKey,
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
	tx, err := s.Database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return template.PublicationRequest{}, err
	}
	defer tx.Rollback()
	predicate := builder.Predicate(builder.And(builder.Equal("id", requestID), builder.Equal("status", string(expectedStatus))))
	if transition.ExpectedLeaseOwner = strings.TrimSpace(transition.ExpectedLeaseOwner); transition.ExpectedLeaseOwner != "" && transition.ExpectedFencingToken > 0 {
		predicate = builder.And(predicate, builder.Equal("lease_owner", transition.ExpectedLeaseOwner), builder.Equal("fencing_token", transition.ExpectedFencingToken))
	}
	query, args, err := builder.NewUpdateBuilder(s.Renderer, "notification_template_publication_requests").Set("status", string(transition.Status)).Set("scheduled_for", transition.ScheduledFor).Set("reviewed_by", transition.ReviewedBy).Set("reviewed_at", transition.ReviewedAt).Set("published_version", transition.PublishedVersion).Set("failure", transition.Failure).Set("lease_owner", "").Set("lease_expires_at", "").Set("updated_at", transition.UpdatedAt).Where(predicate).Build()
	if err != nil {
		return template.PublicationRequest{}, err
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
		query, args, err = builder.NewDeleteBuilder(s.Renderer, "notification_template_publication_locks").Where(builder.Equal("request_id", requestID)).Build()
		if err != nil {
			return template.PublicationRequest{}, err
		}
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
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
	due := builder.Or(builder.And(builder.Equal("status", "scheduled"), builder.LessThanOrEqual("scheduled_for", strings.TrimSpace(now))), builder.And(builder.Equal("status", "publishing"), builder.LessThanOrEqual("lease_expires_at", strings.TrimSpace(staleBefore))))
	query, args, err := builder.NewSelectBuilder(s.Renderer, "notification_template_publication_requests").Columns(publicationRequestColumns...).Where(due).OrderBy(builder.Ascending("scheduled_for"), builder.Ascending("requested_at")).Limit(limit).Build()
	if err != nil {
		return nil, err
	}
	rows, err := s.Database.QueryContext(ctx, query, args...)
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
	due := builder.Or(builder.And(builder.Equal("status", "scheduled"), builder.LessThanOrEqual("scheduled_for", now)), builder.And(builder.Equal("status", "publishing"), builder.LessThanOrEqual("lease_expires_at", now)))
	query, args, err := builder.NewUpdateBuilder(s.Renderer, "notification_template_publication_requests").Set("status", "publishing").Set("lease_owner", owner).Set("lease_expires_at", expiresAt).SetExpression("fencing_token", builder.Add(builder.Column("fencing_token"), builder.Value(1))).Set("updated_at", now).Where(builder.And(builder.Equal("id", requestID), due)).Build()
	if err != nil {
		return template.PublicationRequest{}, false, err
	}
	result, err := s.Database.ExecContext(ctx, query, args...)
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
	query, args, err := builder.NewSelectBuilder(s.Renderer, "notification_template_publication_locks").Projections(builder.Project(builder.CountAll())).Where(builder.Equal("template_key", strings.TrimSpace(templateKey))).Build()
	if err != nil {
		return false, err
	}
	err = s.Database.QueryRowContext(ctx, query, args...).Scan(&count)
	return count > 0, err
}

func (s *Store) publicationRequestByID(ctx context.Context, queryer sqlhost.Queryer, requestID string) (template.PublicationRequest, bool, error) {
	query, args, err := builder.NewSelectBuilder(s.Renderer, "notification_template_publication_requests").Columns(publicationRequestColumns...).Where(builder.Equal("id", requestID)).Build()
	if err != nil {
		return template.PublicationRequest{}, false, err
	}
	value, err := scanPublicationRequest(queryer.QueryRowContext(ctx, query, args...))
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
