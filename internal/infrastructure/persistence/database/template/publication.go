package templatestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	metadatamodulehost "github.com/domainry/domainry-metadata-sdk/modulehost"
	notificationmodulehost "github.com/domainry/domainry-notification-sdk/modulehost"
	template "github.com/domainry/domainry-notification/internal/domain/template/service"
)

const (
	templatePublicationOperationOwner = "notification"
	templatePublicationOperationKind  = "template_publication"
	templatePublicationActionKey      = "notification.templates.publish"
	templatePublicationResourceType   = "notification_template"
)

var _ template.Store = (*Store)(nil)
var _ template.RevisionStore = (*Store)(nil)

func (s *Store) ListPublicationRequests(ctx context.Context, templateKey string) ([]template.PublicationRequest, error) {
	if s.operations == nil {
		return nil, fmt.Errorf("notification shared managed Operation store is unavailable")
	}
	operations, err := s.operations.List(ctx, notificationmodulehost.ManagedOperationQuery{
		Scope: notificationmodulehost.OperationScope{WorkspaceID: s.workspaceID.String()}, Owner: templatePublicationOperationOwner,
		Kind: templatePublicationOperationKind, ResourceID: strings.TrimSpace(templateKey), Limit: 1000,
	})
	if err != nil {
		return nil, fmt.Errorf("list notification publication Operations: %w", err)
	}
	values := make([]template.PublicationRequest, 0, len(operations))
	for _, operation := range operations {
		value, decodeErr := publicationRequestFromOperation(operation)
		if decodeErr != nil {
			return nil, decodeErr
		}
		values = append(values, value)
	}
	return values, nil
}

func (s *Store) GetPublicationRequest(ctx context.Context, requestID string) (template.PublicationRequest, bool, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return template.PublicationRequest{}, false, fmt.Errorf("notification publication request id is required")
	}
	if s.operations == nil {
		return template.PublicationRequest{}, false, fmt.Errorf("notification shared managed Operation store is unavailable")
	}
	operation, found, err := s.operations.Get(ctx, s.publicationOperationIdentity(requestID))
	if err != nil || !found {
		return template.PublicationRequest{}, found, err
	}
	value, err := publicationRequestFromOperation(operation)
	return value, err == nil, err
}

func (s *Store) CreatePublicationRequest(ctx context.Context, request template.PublicationRequest) error {
	request.ID, request.TemplateKey = strings.TrimSpace(request.ID), strings.TrimSpace(request.TemplateKey)
	if request.ID == "" || request.TemplateKey == "" {
		return fmt.Errorf("notification publication request identity is required")
	}
	if s.Database == nil || s.operations == nil {
		return fmt.Errorf("notification publication persistence is unavailable")
	}
	operation, err := s.publicationOperation(request)
	if err != nil {
		return err
	}
	tx, err := s.Database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	txContext := notificationmodulehost.WithOperationExecutor(metadatamodulehost.WithExecutor(ctx, tx), tx)
	definition, found, err := s.templateRecordDefinition(txContext, request.TemplateKey)
	if err != nil {
		return err
	}
	if !found {
		return template.ErrRecordNotFound
	}
	record, err := decodeTemplateRecord(definition.Payload)
	if err != nil {
		return err
	}
	if strings.TrimSpace(record.PublicationID) != "" {
		return template.ErrPublicationConflict
	}
	record.PublicationID = request.ID
	if err := s.publishTemplateRecord(txContext, record, definition.CurrentVersionID, request.RequestedBy); err != nil {
		if errors.Is(err, template.ErrRecordConflict) {
			return template.ErrPublicationConflict
		}
		return err
	}
	if err := s.operations.Create(txContext, operation); err != nil {
		if errors.Is(err, notificationmodulehost.ErrManagedOperationIdentityConflict) {
			return template.ErrPublicationConflict
		}
		return err
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
	if s.Database == nil || s.operations == nil {
		return template.PublicationRequest{}, fmt.Errorf("notification publication persistence is unavailable")
	}
	tx, err := s.Database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return template.PublicationRequest{}, err
	}
	defer func() { _ = tx.Rollback() }()
	txContext := notificationmodulehost.WithOperationExecutor(metadatamodulehost.WithExecutor(ctx, tx), tx)
	current, found, err := s.operations.Get(txContext, s.publicationOperationIdentity(requestID))
	if err != nil {
		return template.PublicationRequest{}, err
	}
	if !found {
		return template.PublicationRequest{}, template.ErrPublicationNotFound
	}
	request, err := publicationRequestFromOperation(current)
	if err != nil {
		return template.PublicationRequest{}, err
	}
	if request.Status != expectedStatus {
		return template.PublicationRequest{}, template.ErrPublicationConflict
	}
	request.Status, request.ScheduledFor = transition.Status, transition.ScheduledFor
	request.ReviewedBy, request.ReviewedAt = transition.ReviewedBy, transition.ReviewedAt
	request.PublishedVersion, request.Failure, request.UpdatedAt = transition.PublishedVersion, transition.Failure, transition.UpdatedAt
	request.LeaseOwner, request.LeaseExpiresAt = "", ""
	metadata, err := json.Marshal(request)
	if err != nil {
		return template.PublicationRequest{}, fmt.Errorf("encode notification publication Operation: %w", err)
	}
	result, _ := json.Marshal(map[string]any{"published_version": request.PublishedVersion, "status": request.Status})
	updatedAt, err := publicationTimestamp(request.UpdatedAt)
	if err != nil {
		return template.PublicationRequest{}, err
	}
	nextAction := ""
	if request.Status == template.PublicationScheduled || request.Status == template.PublicationPublishing {
		nextAction = request.ScheduledFor
	}
	updated, changed, err := s.operations.Transition(txContext, notificationmodulehost.ManagedOperationTransition{
		Identity: s.publicationOperationIdentity(requestID), ExpectedStatus: string(expectedStatus),
		ExpectedLeaseOwner: transition.ExpectedLeaseOwner, ExpectedFencingToken: transition.ExpectedFencingToken,
		Status: string(request.Status), Metadata: metadata, Result: result, ErrorCode: request.Failure,
		NextAction: nextAction, ClearLease: true, UpdatedAt: updatedAt,
	})
	if err != nil {
		return template.PublicationRequest{}, err
	}
	if !changed {
		return template.PublicationRequest{}, template.ErrPublicationConflict
	}
	if !publicationStatusOpen(request.Status) {
		if err := s.clearPublicationReservation(txContext, request); err != nil {
			return template.PublicationRequest{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return template.PublicationRequest{}, err
	}
	return publicationRequestFromOperation(updated)
}

func (s *Store) ListDuePublicationRequests(ctx context.Context, now, staleBefore string, limit int) ([]template.PublicationRequest, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	if s.operations == nil {
		return nil, fmt.Errorf("notification shared managed Operation store is unavailable")
	}
	base := notificationmodulehost.ManagedOperationQuery{Scope: notificationmodulehost.OperationScope{WorkspaceID: s.workspaceID.String()}, Owner: templatePublicationOperationOwner, Kind: templatePublicationOperationKind, Limit: limit, OldestFirst: true}
	scheduledQuery := base
	scheduledQuery.Statuses, scheduledQuery.NextActionBefore = []string{string(template.PublicationScheduled)}, strings.TrimSpace(now)
	scheduled, err := s.operations.List(ctx, scheduledQuery)
	if err != nil {
		return nil, err
	}
	reclaimQuery := base
	reclaimQuery.Statuses, reclaimQuery.LeaseExpiresBefore = []string{string(template.PublicationPublishing)}, strings.TrimSpace(staleBefore)
	reclaim, err := s.operations.List(ctx, reclaimQuery)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]template.PublicationRequest, len(scheduled)+len(reclaim))
	for _, operation := range append(scheduled, reclaim...) {
		value, decodeErr := publicationRequestFromOperation(operation)
		if decodeErr != nil {
			return nil, decodeErr
		}
		byID[value.ID] = value
	}
	values := make([]template.PublicationRequest, 0, len(byID))
	for _, value := range byID {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool {
		left, right := values[i].ScheduledFor, values[j].ScheduledFor
		if left == right {
			return values[i].RequestedAt < values[j].RequestedAt
		}
		return left < right
	})
	if len(values) > limit {
		values = values[:limit]
	}
	return values, nil
}

func (s *Store) ClaimPublicationRequest(ctx context.Context, requestID, owner, now, expiresAt string) (template.PublicationRequest, bool, error) {
	requestID, owner, now, expiresAt = strings.TrimSpace(requestID), strings.TrimSpace(owner), strings.TrimSpace(now), strings.TrimSpace(expiresAt)
	if requestID == "" || owner == "" || now == "" || expiresAt == "" {
		return template.PublicationRequest{}, false, fmt.Errorf("notification publication claim identity and lease are required")
	}
	if s.operations == nil {
		return template.PublicationRequest{}, false, fmt.Errorf("notification shared managed Operation store is unavailable")
	}
	updatedAt, err := publicationTimestamp(now)
	if err != nil {
		return template.PublicationRequest{}, false, err
	}
	operation, claimed, err := s.operations.Claim(ctx, notificationmodulehost.ManagedOperationClaim{
		Identity: s.publicationOperationIdentity(requestID), DueStatus: string(template.PublicationScheduled), ReclaimStatus: string(template.PublicationPublishing),
		Now: now, Status: string(template.PublicationPublishing), LeaseOwner: owner, LeaseExpiresAt: expiresAt, UpdatedAt: updatedAt,
	})
	if err != nil || !claimed {
		return template.PublicationRequest{}, false, err
	}
	value, err := publicationRequestFromOperation(operation)
	return value, err == nil, err
}

func (s *Store) HasOpenPublicationRequest(ctx context.Context, templateKey string) (bool, error) {
	definition, found, err := s.templateRecordDefinition(ctx, strings.TrimSpace(templateKey))
	if err != nil || !found {
		return false, err
	}
	record, err := decodeTemplateRecord(definition.Payload)
	return strings.TrimSpace(record.PublicationID) != "", err
}

func (s *Store) publicationOperationIdentity(requestID string) notificationmodulehost.ManagedOperationIdentity {
	return notificationmodulehost.ManagedOperationIdentity{ID: strings.TrimSpace(requestID), Scope: notificationmodulehost.OperationScope{WorkspaceID: s.workspaceID.String()}, Owner: templatePublicationOperationOwner, Kind: templatePublicationOperationKind}
}

func (s *Store) publicationOperation(request template.PublicationRequest) (notificationmodulehost.ManagedOperation, error) {
	metadata, err := json.Marshal(request)
	if err != nil {
		return notificationmodulehost.ManagedOperation{}, fmt.Errorf("encode notification publication Operation: %w", err)
	}
	createdAt, err := publicationTimestamp(request.RequestedAt)
	if err != nil {
		return notificationmodulehost.ManagedOperation{}, err
	}
	updatedAt, err := publicationTimestamp(request.UpdatedAt)
	if err != nil {
		return notificationmodulehost.ManagedOperation{}, err
	}
	nextAction := ""
	if request.Status == template.PublicationScheduled || request.Status == template.PublicationPublishing {
		nextAction = request.ScheduledFor
	}
	return notificationmodulehost.ManagedOperation{
		Command: notificationmodulehost.OperationCommand{
			ID: request.ID, Scope: notificationmodulehost.OperationScope{WorkspaceID: s.workspaceID.String(), ResourceType: templatePublicationResourceType, ResourceID: request.TemplateKey},
			Owner: templatePublicationOperationOwner, Kind: templatePublicationOperationKind, ActionKey: templatePublicationActionKey,
			IdempotencyKey: request.ID, RequestFingerprint: request.CandidateHash, RequestedBy: request.RequestedBy,
			Reason: "Notification template publication", Reference: request.TemplateKey, StatusURL: "/notification/publications/" + request.ID, CreatedAt: createdAt,
		},
		Status: string(request.Status), Metadata: metadata, Result: json.RawMessage(`{}`), ErrorCode: request.Failure,
		NextAction: nextAction, LeaseOwner: request.LeaseOwner, LeaseExpiresAt: request.LeaseExpiresAt, FencingToken: request.FencingToken, UpdatedAt: updatedAt,
	}, nil
}

func publicationRequestFromOperation(operation notificationmodulehost.ManagedOperation) (template.PublicationRequest, error) {
	var value template.PublicationRequest
	if err := json.Unmarshal(operation.Metadata, &value); err != nil {
		return value, fmt.Errorf("decode notification publication Operation: %w", err)
	}
	value.ID, value.TemplateKey = operation.Command.ID, operation.Command.Scope.ResourceID
	value.Status = template.PublicationStatus(operation.Status)
	value.LeaseOwner, value.LeaseExpiresAt, value.FencingToken = operation.LeaseOwner, operation.LeaseExpiresAt, operation.FencingToken
	value.UpdatedAt = operation.UpdatedAt.UTC().Format(time.RFC3339Nano)
	if operation.ErrorCode != "" {
		value.Failure = operation.ErrorCode
	}
	return value, nil
}

func (s *Store) clearPublicationReservation(ctx context.Context, request template.PublicationRequest) error {
	definition, found, err := s.templateRecordDefinition(ctx, request.TemplateKey)
	if err != nil {
		return err
	}
	if !found {
		return template.ErrPublicationConflict
	}
	record, err := decodeTemplateRecord(definition.Payload)
	if err != nil {
		return err
	}
	if record.PublicationID != request.ID {
		return template.ErrPublicationConflict
	}
	record.PublicationID = ""
	actor := strings.TrimSpace(request.ReviewedBy)
	if actor == "" {
		actor = strings.TrimSpace(request.RequestedBy)
	}
	if err := s.publishTemplateRecord(ctx, record, definition.CurrentVersionID, actor); err != nil {
		if errors.Is(err, template.ErrRecordConflict) {
			return template.ErrPublicationConflict
		}
		return err
	}
	return nil
}

func publicationTimestamp(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, fmt.Errorf("notification publication timestamp %q is invalid: %w", value, err)
	}
	return parsed.UTC(), nil
}

func publicationStatusOpen(status template.PublicationStatus) bool {
	return status == template.PublicationPending || status == template.PublicationScheduled || status == template.PublicationPublishing
}
