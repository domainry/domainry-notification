package inbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/domainry/domainry-foundation/entityid"
	"github.com/domainry/domainry-foundation/logging"
	"github.com/domainry/domainry-foundation/worker"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-notification/internal/domain/template/service"
	"go.uber.org/zap"
)

const (
	eventLeaseDuration = 30 * time.Second
	maximumAttempts    = 8
)

type AudienceResolver interface {
	ResolveAudience(context.Context, string, Event) ([]notification.UserID, error)
}

type RecipientLocaleResolver interface {
	RecipientLocale(context.Context, notification.WorkspaceID, notification.UserID) (string, error)
}

// Processor claims durable events, resolves dynamic audiences, and atomically
// materializes inbox items and channel plans through EventStore.
type Processor struct {
	events          EventStore
	clock           notification.Clock
	workerID        string
	audiences       AudienceResolver
	recipientLocale RecipientLocaleResolver
	notifier        WorkNotifier
}

type ProcessorDependencies struct {
	Events          EventStore
	Clock           notification.Clock
	WorkerID        string
	Audiences       AudienceResolver
	RecipientLocale RecipientLocaleResolver
	WorkNotifier    WorkNotifier
}

func NewProcessor(dependencies ProcessorDependencies) (*Processor, error) {
	dependencies.WorkerID = strings.TrimSpace(dependencies.WorkerID)
	if dependencies.Events == nil || dependencies.Clock == nil || dependencies.WorkerID == "" {
		return nil, fmt.Errorf("notification inbox processor dependencies are required")
	}
	return &Processor{
		events: dependencies.Events, clock: dependencies.Clock, workerID: dependencies.WorkerID,
		audiences: dependencies.Audiences, recipientLocale: dependencies.RecipientLocale, notifier: dependencies.WorkNotifier,
	}, nil
}

func (p *Processor) ProcessDue(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	now := p.clock.Now().UTC()
	events, err := p.events.ListDue(ctx, notification.Timestamp(now), limit)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, event := range events {
		completed, processErr := p.processAt(ctx, event.WorkspaceID, event.EventID, now)
		if processErr != nil {
			return processed, processErr
		}
		if completed {
			processed++
		}
	}
	return processed, nil
}

func (p *Processor) Process(ctx context.Context, workspaceID notification.WorkspaceID, eventID string) (bool, error) {
	return p.processAt(ctx, workspaceID, strings.TrimSpace(eventID), p.clock.Now().UTC())
}

func (p *Processor) processAt(ctx context.Context, workspaceID notification.WorkspaceID, eventID string, now time.Time) (bool, error) {
	claimed, found, err := p.events.Claim(ctx, workspaceID, eventID, p.workerID, notification.Timestamp(now), notification.Timestamp(now.Add(eventLeaseDuration)))
	if err != nil || !found {
		return false, err
	}
	if materializeErr := p.materialize(ctx, claimed); materializeErr != nil {
		if retryErr := p.recordFailure(ctx, claimed, materializeErr, now); retryErr != nil {
			return false, retryErr
		}
		return false, nil
	}
	return true, nil
}

func (p *Processor) materialize(ctx context.Context, event Event) error {
	recipients := append([]notification.UserID(nil), event.RecipientUserIDs...)
	for _, resolverKey := range event.AudienceResolverKeys {
		if p.audiences == nil {
			return unavailable("backend.notification.inbox_audience_resolver_unavailable", nil)
		}
		resolved, err := p.audiences.ResolveAudience(ctx, resolverKey, event)
		if err != nil {
			return unavailable("backend.notification.inbox_audience_resolution_failed", err)
		}
		recipients = append(recipients, resolved...)
	}
	recipients = uniqueUsers(recipients)
	if len(recipients) == 0 || len(recipients) > recipientLimit {
		return unavailable("backend.notification.inbox_audience_resolution_empty", nil)
	}
	event.RecipientUserIDs = recipients
	for index := range event.ChannelPlans {
		event.ChannelPlans[index].RecipientUserIDs = append([]notification.UserID(nil), recipients...)
	}
	items := make([]Item, 0, len(recipients))
	for _, recipient := range recipients {
		locale := ""
		if p.recipientLocale != nil {
			var err error
			locale, err = p.recipientLocale.RecipientLocale(ctx, event.WorkspaceID, recipient)
			if err != nil {
				return err
			}
		}
		item, err := itemFromEvent(event, recipient, locale)
		if err != nil {
			return err
		}
		items = append(items, item)
	}
	if err := p.events.Materialize(ctx, event, items); err != nil {
		return err
	}
	if p.notifier != nil {
		for _, plan := range event.ChannelPlans {
			p.notifier.Notify(ctx, notification.Work{Kind: notification.WorkChannelPlan, WorkspaceID: plan.WorkspaceID, TaskID: plan.DeliveryID})
		}
	}
	return nil
}

func (p *Processor) recordFailure(ctx context.Context, event Event, cause error, now time.Time) error {
	updatedAt := notification.Timestamp(now)
	stage, code := "materialization", "backend.notification.inbox_materialization_failed"
	if causeCode := notification.ErrorCode(cause); strings.HasPrefix(causeCode, "backend.notification.inbox_audience_") {
		stage, code = "audience_resolution", causeCode
	}
	attempt := event.AttemptCount + 1
	policy := worker.RetryPolicy{MaxAttempts: maximumAttempts, BaseDelay: time.Minute, MaxDelay: 32 * time.Minute, Backoff: worker.BackoffExponential}
	if !policy.Allows(attempt, now) {
		if err := p.events.Fail(ctx, event, stage, code, updatedAt); err != nil {
			return errors.Join(cause, err)
		}
		logInboxTransition(ctx, cause, event, stage, attempt, "dead_letter", false)
		return nil
	}
	next := notification.Timestamp(now.Add(policy.Delay(attempt, nil)))
	if err := p.events.Retry(ctx, event, stage, code, next, updatedAt); err != nil {
		return errors.Join(cause, err)
	}
	logInboxTransition(ctx, cause, event, stage, attempt, "retry", true)
	return nil
}

func logInboxTransition(ctx context.Context, cause error, event Event, stage string, attempt int, outcome string, retryable bool) {
	fields := logging.ErrorFields(cause, logging.Failure{Stage: stage, Retryable: retryable})
	fields = append(fields,
		zap.String("task_kind", "notification_inbox"),
		zap.String("task_id", event.EventID),
		zap.Int("attempt", attempt),
		zap.Int("max_attempts", maximumAttempts),
		zap.String("outcome", outcome),
	)
	logger := logging.FromContext(ctx)
	if retryable {
		logger.Warn("notification_inbox_retry_scheduled", fields...)
		return
	}
	logger.Error("notification_inbox_dead_lettered", fields...)
}

func itemFromEvent(event Event, recipient notification.UserID, locale string) (Item, error) {
	itemID, err := entityid.New("inbox")
	if err != nil {
		return Item{}, err
	}
	identity := event.EventID
	if event.GroupKey != "" {
		identity = "group:" + event.GroupKey
	}
	snapshot := snapshotForLocale(event, locale)
	return Item{
		ItemID: itemID, ItemKey: inboxItemKey(event.WorkspaceID.String(), recipient.String(), identity), WorkspaceID: event.WorkspaceID, RecipientUserID: recipient,
		EventID: event.EventID, EventType: event.EventType, Source: event.Source, Category: event.Category, Severity: event.Severity,
		Title: snapshot.Title, Body: snapshot.Body, Facts: append([]template.Fact(nil), snapshot.Facts...), Actions: append([]ActionRef(nil), snapshot.Actions...),
		TemplateKey: snapshot.TemplateKey, TemplateVersion: snapshot.TemplateVersion, TemplateLocale: snapshot.TemplateLocale, TemplateContentHash: snapshot.TemplateContentHash,
		SubjectType: event.SubjectType, SubjectID: event.SubjectID, SubjectVersion: event.SubjectVersion,
		ActionState: event.ActionState, AlertState: event.AlertState, GroupKey: event.GroupKey, OccurrenceCount: 1,
		FirstOccurredAt: event.OccurredAt, LastOccurredAt: event.OccurredAt, ExpiresAt: event.ExpiresAt,
		CreatedAt: event.CreatedAt, UpdatedAt: event.UpdatedAt,
	}, nil
}

func inboxItemKey(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write([]byte(strings.TrimSpace(part)))
		_, _ = hash.Write([]byte{0})
	}
	return "notifkey_" + hex.EncodeToString(hash.Sum(nil)[:16])
}

func snapshotForLocale(event Event, requested string) Snapshot {
	requested = normalizeLocale(requested)
	for locale, snapshot := range event.LocalizedSnapshots {
		if strings.EqualFold(normalizeLocale(locale), requested) {
			return snapshot
		}
	}
	return event.Snapshot
}
