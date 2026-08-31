package template

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

const publicationLeaseDuration = 2 * time.Minute

type PublicationWorkNotifier interface {
	Notify(context.Context, notification.Work)
}

type ResumedPublicationAuthorizer func(context.Context, string) (bool, error)

type PublicationProcessorDependencies struct {
	Store            Store
	Manager          *Manager
	Clock            notification.Clock
	WorkerID         string
	WorkNotifier     PublicationWorkNotifier
	NewRequestID     func() (string, error)
	AuthorizeResumed ResumedPublicationAuthorizer
}

// PublicationProcessor owns approval and durable asynchronous publication.
// It does not own administrator authorization or worker scheduling.
type PublicationProcessor struct {
	store            Store
	manager          *Manager
	clock            notification.Clock
	workerID         string
	notifier         PublicationWorkNotifier
	newID            func() (string, error)
	authorizeResumed ResumedPublicationAuthorizer
}

func NewPublicationProcessor(dependencies PublicationProcessorDependencies) (*PublicationProcessor, error) {
	dependencies.WorkerID = strings.TrimSpace(dependencies.WorkerID)
	if dependencies.Store == nil || dependencies.Manager == nil || dependencies.Clock == nil || dependencies.WorkerID == "" || dependencies.AuthorizeResumed == nil {
		return nil, fmt.Errorf("notification publication processor dependencies are required")
	}
	if dependencies.NewRequestID == nil {
		dependencies.NewRequestID = publicationRequestID
	}
	return &PublicationProcessor{store: dependencies.Store, manager: dependencies.Manager, clock: dependencies.Clock, workerID: dependencies.WorkerID,
		notifier: dependencies.WorkNotifier, newID: dependencies.NewRequestID, authorizeResumed: dependencies.AuthorizeResumed}, nil
}

func PublicationCandidateHash(value Template) string {
	value.Status, value.Version, value.ContentHash = "draft", 0, ""
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (p *PublicationProcessor) List(ctx context.Context, templateKey string) ([]PublicationRequest, error) {
	return p.store.ListPublicationRequests(ctx, strings.TrimSpace(templateKey))
}

func (p *PublicationProcessor) Request(ctx context.Context, key, scheduledFor, expectedUpdatedAt, actor string) (PublicationRequest, error) {
	key, actor = strings.TrimSpace(key), strings.TrimSpace(actor)
	if key == "" || actor == "" {
		return PublicationRequest{}, invalid("backend.notification.publication_identity_required", "template_key", key)
	}
	record, found, err := p.store.Get(ctx, key)
	if err != nil {
		return PublicationRequest{}, err
	}
	if !found || record.Draft == nil {
		return PublicationRequest{}, notification.NewError(notification.ErrorNotFound, "backend.notification.template_draft_not_found", nil, map[string]any{"template_key": key})
	}
	if expectedUpdatedAt != "" && expectedUpdatedAt != record.UpdatedAt {
		return PublicationRequest{}, notification.NewError(notification.ErrorConflict, "backend.notification.template_conflict", nil, map[string]any{"template_key": key})
	}
	if locked, err := p.store.HasOpenPublicationRequest(ctx, key); err != nil {
		return PublicationRequest{}, err
	} else if locked {
		return PublicationRequest{}, notification.NewError(notification.ErrorConflict, "backend.notification.template_publication_locked", nil, map[string]any{"template_key": key})
	}
	snapshot := cloneTemplate(*record.Draft)
	snapshot.Key, snapshot.Status = key, "draft"
	if err := p.manager.ValidateEditable(snapshot); err != nil {
		return PublicationRequest{}, err
	}
	now := p.clock.Now().UTC()
	when := ""
	if scheduledFor = strings.TrimSpace(scheduledFor); scheduledFor != "" {
		parsed, parseErr := time.Parse(time.RFC3339Nano, scheduledFor)
		if parseErr != nil || !parsed.After(now) {
			return PublicationRequest{}, invalid("backend.notification.publication_schedule_invalid", "template_key", key)
		}
		when = notification.Timestamp(parsed)
	}
	requestID, err := p.newID()
	if err != nil {
		return PublicationRequest{}, fmt.Errorf("generate notification publication request id: %w", err)
	}
	if strings.TrimSpace(requestID) == "" {
		return PublicationRequest{}, fmt.Errorf("generate notification publication request id: empty id")
	}
	request := PublicationRequest{
		ID: strings.TrimSpace(requestID), TemplateKey: key, Snapshot: snapshot, CandidateHash: PublicationCandidateHash(snapshot), DraftUpdatedAt: record.UpdatedAt,
		Status: PublicationPending, ScheduledFor: when, RequestedBy: actor, RequestedAt: notification.Timestamp(now), UpdatedAt: notification.Timestamp(now),
	}
	if err := p.store.CreatePublicationRequest(ctx, request); err != nil {
		if errors.Is(err, ErrPublicationConflict) {
			return PublicationRequest{}, notification.NewError(notification.ErrorConflict, "backend.notification.template_publication_locked", err, map[string]any{"template_key": key})
		}
		return PublicationRequest{}, err
	}
	return request, nil
}

func (p *PublicationProcessor) Approve(ctx context.Context, requestID, actor string) (PublicationRequest, error) {
	requestID, actor = strings.TrimSpace(requestID), strings.TrimSpace(actor)
	request, err := p.requireRequest(ctx, requestID)
	if err != nil {
		return PublicationRequest{}, err
	}
	if request.RequestedBy == actor {
		return PublicationRequest{}, notification.NewError(notification.ErrorConflict, "backend.notification.publication_self_approval_forbidden", nil, map[string]any{"publication_id": requestID})
	}
	if request.Status != PublicationPending {
		return PublicationRequest{}, publicationStatusConflict(requestID, nil)
	}
	now := p.clock.Now().UTC()
	scheduled := request.ScheduledFor
	if scheduled == "" {
		scheduled = notification.Timestamp(now)
	}
	value, err := p.store.TransitionPublicationRequest(ctx, requestID, PublicationPending, PublicationTransition{
		Status: PublicationScheduled, ScheduledFor: scheduled, ReviewedBy: actor, ReviewedAt: notification.Timestamp(now), UpdatedAt: notification.Timestamp(now),
	})
	if err != nil {
		if errors.Is(err, ErrPublicationConflict) {
			return PublicationRequest{}, publicationStatusConflict(requestID, err)
		}
		return PublicationRequest{}, err
	}
	if p.notifier != nil {
		p.notifier.Notify(ctx, notification.Work{Kind: notification.WorkPublication, TaskID: value.ID})
	}
	return value, nil
}

func (p *PublicationProcessor) Reject(ctx context.Context, requestID, actor, reason string) (PublicationRequest, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return PublicationRequest{}, invalid("backend.notification.publication_rejection_reason_required", "publication_id", requestID)
	}
	request, err := p.requireRequest(ctx, strings.TrimSpace(requestID))
	if err != nil {
		return PublicationRequest{}, err
	}
	if request.RequestedBy == strings.TrimSpace(actor) {
		return PublicationRequest{}, notification.NewError(notification.ErrorConflict, "backend.notification.publication_self_approval_forbidden", nil, map[string]any{"publication_id": request.ID})
	}
	now := notification.Timestamp(p.clock.Now())
	value, err := p.store.TransitionPublicationRequest(ctx, request.ID, PublicationPending, PublicationTransition{
		Status: PublicationRejected, ReviewedBy: strings.TrimSpace(actor), ReviewedAt: now, Failure: reason, UpdatedAt: now,
	})
	if errors.Is(err, ErrPublicationConflict) {
		return PublicationRequest{}, publicationStatusConflict(request.ID, err)
	}
	return value, err
}

func (p *PublicationProcessor) Cancel(ctx context.Context, requestID, actor string) (PublicationRequest, error) {
	request, err := p.requireRequest(ctx, strings.TrimSpace(requestID))
	if err != nil {
		return PublicationRequest{}, err
	}
	if request.Status != PublicationPending && request.Status != PublicationScheduled {
		return PublicationRequest{}, publicationStatusConflict(request.ID, nil)
	}
	now := notification.Timestamp(p.clock.Now())
	value, err := p.store.TransitionPublicationRequest(ctx, request.ID, request.Status, PublicationTransition{
		Status: PublicationCancelled, ReviewedBy: strings.TrimSpace(actor), ReviewedAt: now, UpdatedAt: now,
	})
	if errors.Is(err, ErrPublicationConflict) {
		return PublicationRequest{}, publicationStatusConflict(request.ID, err)
	}
	return value, err
}

func (p *PublicationProcessor) ProcessDue(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	now := p.clock.Now().UTC()
	requests, err := p.store.ListDuePublicationRequests(ctx, notification.Timestamp(now), notification.Timestamp(now), limit)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, request := range requests {
		claimed, found, err := p.store.ClaimPublicationRequest(ctx, request.ID, p.workerID, notification.Timestamp(now), notification.Timestamp(now.Add(publicationLeaseDuration)))
		if err != nil {
			return processed, err
		}
		if !found {
			continue
		}
		_, _ = p.publishClaimed(ctx, claimed)
		processed++
	}
	return processed, nil
}

func (p *PublicationProcessor) Process(ctx context.Context, requestID string) (bool, error) {
	now := p.clock.Now().UTC()
	claimed, found, err := p.store.ClaimPublicationRequest(ctx, strings.TrimSpace(requestID), p.workerID, notification.Timestamp(now), notification.Timestamp(now.Add(publicationLeaseDuration)))
	if err != nil || !found {
		return false, err
	}
	_, _ = p.publishClaimed(ctx, claimed)
	return true, nil
}

func (p *PublicationProcessor) QueueStats(ctx context.Context, limit int) (int, time.Duration, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	now := p.clock.Now().UTC()
	requests, err := p.store.ListDuePublicationRequests(ctx, notification.Timestamp(now), notification.Timestamp(now), limit)
	if err != nil {
		return 0, 0, err
	}
	lag := time.Duration(0)
	for _, request := range requests {
		if scheduled, parseErr := time.Parse(time.RFC3339Nano, request.ScheduledFor); parseErr == nil && now.After(scheduled) && now.Sub(scheduled) > lag {
			lag = now.Sub(scheduled)
		}
	}
	return len(requests), lag, nil
}

func (p *PublicationProcessor) publishClaimed(ctx context.Context, request PublicationRequest) (PublicationRequest, error) {
	allowed, err := p.authorizeResumed(ctx, request.ReviewedBy)
	if err != nil {
		return PublicationRequest{}, err
	}
	if !allowed {
		value, finishErr := p.finish(ctx, request, PublicationFailed, 0, "backend.notification.publication_authorization_revoked")
		if finishErr != nil {
			return PublicationRequest{}, finishErr
		}
		return value, notification.NewError(notification.ErrorForbidden, "backend.notification.publication_authorization_revoked", nil, map[string]any{"publication_id": request.ID})
	}
	record, found, err := p.store.Get(ctx, request.TemplateKey)
	if err != nil {
		return PublicationRequest{}, err
	}
	if found && record.Published != nil && PublicationCandidateHash(*record.Published) == request.CandidateHash && record.Draft == nil {
		return p.finish(ctx, request, PublicationPublished, record.PublishedVersion, "")
	}
	if !found || record.Draft == nil || record.UpdatedAt != request.DraftUpdatedAt || PublicationCandidateHash(*record.Draft) != request.CandidateHash {
		value, finishErr := p.finish(ctx, request, PublicationSuperseded, 0, "backend.notification.publication_candidate_changed")
		if finishErr != nil {
			return PublicationRequest{}, finishErr
		}
		return value, notification.NewError(notification.ErrorConflict, "backend.notification.publication_candidate_changed", nil, map[string]any{"publication_id": request.ID})
	}
	published, err := p.manager.PublishApproved(ctx, request.TemplateKey, request.DraftUpdatedAt, request.ReviewedBy)
	if err != nil {
		if current, ok, getErr := p.store.Get(ctx, request.TemplateKey); getErr == nil && ok && current.Published != nil && PublicationCandidateHash(*current.Published) == request.CandidateHash {
			return p.finish(ctx, request, PublicationPublished, current.PublishedVersion, "")
		}
		failureCode := notification.ErrorCode(err)
		if failureCode == "" {
			failureCode = "backend.notification.publication_failed"
		}
		value, finishErr := p.finish(ctx, request, PublicationFailed, 0, failureCode)
		if finishErr != nil {
			return PublicationRequest{}, finishErr
		}
		return value, err
	}
	return p.finish(ctx, request, PublicationPublished, published.PublishedVersion, "")
}

func (p *PublicationProcessor) finish(ctx context.Context, request PublicationRequest, status PublicationStatus, version int, failure string) (PublicationRequest, error) {
	value, err := p.store.TransitionPublicationRequest(ctx, request.ID, PublicationPublishing, PublicationTransition{
		Status: status, ScheduledFor: request.ScheduledFor, ReviewedBy: request.ReviewedBy, ReviewedAt: request.ReviewedAt, PublishedVersion: version,
		Failure: failure, ExpectedLeaseOwner: request.LeaseOwner, ExpectedFencingToken: request.FencingToken, UpdatedAt: notification.Timestamp(p.clock.Now()),
	})
	if errors.Is(err, ErrPublicationConflict) {
		current, found, getErr := p.store.GetPublicationRequest(ctx, request.ID)
		if getErr == nil && found && current.Status == status {
			return current, nil
		}
	}
	return value, err
}

func (p *PublicationProcessor) requireRequest(ctx context.Context, requestID string) (PublicationRequest, error) {
	request, found, err := p.store.GetPublicationRequest(ctx, requestID)
	if err != nil {
		return PublicationRequest{}, err
	}
	if !found {
		return PublicationRequest{}, notification.NewError(notification.ErrorNotFound, "backend.notification.publication_not_found", nil, map[string]any{"publication_id": requestID})
	}
	return request, nil
}

func publicationRequestID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return "ntpub_" + hex.EncodeToString(value), nil
}

func publicationStatusConflict(requestID string, cause error) error {
	return notification.NewError(notification.ErrorConflict, "backend.notification.publication_status_conflict", cause, map[string]any{"publication_id": requestID})
}
