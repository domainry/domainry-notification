package delivery

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/domainry/domainry-foundation/worker"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-notification/internal/domain/template/service"
)

const (
	planLeaseDuration = 30 * time.Second
	maximumAttempts   = 8
)

type Renderer interface {
	Render(context.Context, template.RenderRequest) (template.Rendered, error)
}

type ProcessorDependencies struct {
	Plans      PlanStore
	Renderer   Renderer
	Dispatcher Dispatcher
	Policy     PolicyEvaluator
	Clock      notification.Clock
	WorkerID   string
}

// Processor owns durable channel-plan orchestration. It stops at Dispatcher;
// Integration Outbox and provider execution remain host/Connector concerns.
type Processor struct {
	plans      PlanStore
	renderer   Renderer
	dispatcher Dispatcher
	policy     PolicyEvaluator
	clock      notification.Clock
	workerID   string
}

func NewProcessor(dependencies ProcessorDependencies) (*Processor, error) {
	dependencies.WorkerID = strings.TrimSpace(dependencies.WorkerID)
	if dependencies.Plans == nil || dependencies.Renderer == nil || dependencies.Dispatcher == nil || dependencies.Policy == nil || dependencies.Clock == nil || dependencies.WorkerID == "" {
		return nil, fmt.Errorf("notification delivery processor dependencies are required")
	}
	return &Processor{plans: dependencies.Plans, renderer: dependencies.Renderer, dispatcher: dependencies.Dispatcher, policy: dependencies.Policy, clock: dependencies.Clock, workerID: dependencies.WorkerID}, nil
}

func (p *Processor) ProcessDue(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	now := p.clock.Now().UTC()
	plans, err := p.plans.ListDuePlans(ctx, notification.Timestamp(now), limit)
	if err != nil {
		return 0, err
	}
	processed := 0
	consumed := map[string]bool{}
	for _, candidate := range plans {
		if consumed[candidate.ID] {
			continue
		}
		if candidate.DeliveryMode == "digest" && strings.TrimSpace(candidate.DigestKey) != "" {
			batch := digestCandidates(plans, candidate, consumed)
			for _, value := range batch {
				consumed[value.ID] = true
			}
			count, processErr := p.processDigest(ctx, batch, now)
			if processErr != nil {
				return processed, processErr
			}
			processed += count
			continue
		}
		consumed[candidate.ID] = true
		done, processErr := p.processImmediate(ctx, candidate, now)
		if processErr != nil {
			return processed, processErr
		}
		if done {
			processed++
		}
	}
	return processed, nil
}

func (p *Processor) Process(ctx context.Context, workspaceID notification.WorkspaceID, planID string) (bool, error) {
	plan, found, err := p.plans.GetPlan(ctx, workspaceID, strings.TrimSpace(planID))
	if err != nil || !found {
		return false, err
	}
	if plan.DeliveryMode == "digest" {
		return false, nil
	}
	return p.processImmediate(ctx, plan, p.clock.Now().UTC())
}

func (p *Processor) processImmediate(ctx context.Context, candidate Plan, now time.Time) (bool, error) {
	claimed, deliverable, err := p.claimDeliverable(ctx, candidate, now)
	if err != nil || !deliverable {
		return false, err
	}
	receipt, err := p.dispatch(ctx, claimed, now)
	if err != nil {
		if failureErr := p.recordFailure(ctx, claimed, now); failureErr != nil {
			return false, failureErr
		}
		return false, nil
	}
	if err := p.plans.CompletePlan(ctx, claimed, receipt.MessageID, notification.Timestamp(now)); err != nil {
		return false, err
	}
	return true, nil
}

func (p *Processor) claimDeliverable(ctx context.Context, candidate Plan, now time.Time) (Plan, bool, error) {
	claimed, found, err := p.plans.ClaimPlan(ctx, candidate.WorkspaceID, candidate.ID, p.workerID, notification.Timestamp(now), notification.Timestamp(now.Add(planLeaseDuration)))
	if err != nil || !found {
		return claimed, found, err
	}
	if !claimed.CancelWhenActionTerminal {
		return claimed, true, nil
	}
	terminal, err := p.plans.IsActionTerminal(ctx, claimed)
	if err != nil {
		return claimed, false, err
	}
	if !terminal {
		return claimed, true, nil
	}
	err = p.plans.CancelPlan(ctx, claimed, "backend.notification.channel_plan_action_terminal", notification.Timestamp(now))
	return claimed, false, err
}

func (p *Processor) processDigest(ctx context.Context, candidates []Plan, now time.Time) (int, error) {
	claimed := make([]Plan, 0, len(candidates))
	for _, candidate := range candidates {
		value, deliverable, err := p.claimDeliverable(ctx, candidate, now)
		if err != nil {
			return 0, err
		}
		if deliverable {
			claimed = append(claimed, value)
		}
	}
	if len(claimed) == 0 {
		return 0, nil
	}
	aggregate := digestPlan(claimed)
	receipt, err := p.dispatch(ctx, aggregate, now)
	if err != nil {
		for _, plan := range claimed {
			if failureErr := p.recordFailure(ctx, plan, now); failureErr != nil {
				return 0, failureErr
			}
		}
		return 0, nil
	}
	if err := p.plans.CompletePlanBatch(ctx, claimed, receipt.MessageID, notification.Timestamp(now)); err != nil {
		return 0, err
	}
	return len(claimed), nil
}

func (p *Processor) dispatch(ctx context.Context, plan Plan, now time.Time) (DispatchReceipt, error) {
	decision, err := p.policy.EvaluateDelivery(ctx, Evaluation{
		WorkspaceID: plan.WorkspaceID, TemplateKey: plan.TemplateKey, Channel: plan.Channel,
		Recipients: append([]notification.UserID(nil), plan.RecipientUserIDs...), DedupeKey: plan.DedupeKey, ReservationKey: plan.ID,
	})
	if err != nil {
		return DispatchReceipt{}, err
	}
	content, err := p.renderer.Render(ctx, template.RenderRequest{
		WorkspaceID: plan.WorkspaceID, TemplateKey: plan.TemplateKey, Locale: plan.Locale,
		Recipients: append([]notification.UserID(nil), plan.RecipientUserIDs...), Variables: cloneVariables(plan.Variables),
		Metadata: map[string]any{"notification_event_id": plan.EventID, "notification_channel_plan_id": plan.ID, "dedupe_key": plan.DedupeKey},
	})
	if err != nil {
		return DispatchReceipt{}, err
	}
	fallbacks, err := p.renderFallbacks(ctx, plan, content, decision)
	if err != nil {
		return DispatchReceipt{}, err
	}
	return p.dispatcher.Dispatch(ctx, DispatchRequest{
		WorkspaceID: plan.WorkspaceID, PlanID: plan.ID, EventID: plan.EventID, Channel: plan.Channel,
		ConnectorKey: plan.ConnectorKey, ConnectionKey: plan.ConnectionKey, Operation: plan.Operation,
		DeduplicationKey: plan.ID, Content: content, Decision: decision, Fallbacks: fallbacks, CreatedAt: now,
	})
}

func (p *Processor) renderFallbacks(ctx context.Context, plan Plan, content template.Rendered, decision Decision) ([]DispatchFallback, error) {
	result := make([]DispatchFallback, 0, len(content.Fallbacks))
	for _, fallback := range content.Fallbacks {
		rendered, err := p.renderer.Render(ctx, template.RenderRequest{
			WorkspaceID: plan.WorkspaceID, TemplateKey: fallback.TemplateKey, Locale: plan.Locale,
			Recipients: append([]notification.UserID(nil), plan.RecipientUserIDs...), Variables: cloneVariables(plan.Variables),
			Metadata: map[string]any{"notification_event_id": plan.EventID, "notification_channel_plan_id": plan.ID, "dedupe_key": plan.DedupeKey},
		})
		if err != nil {
			return nil, err
		}
		result = append(result, DispatchFallback{ConnectorKey: fallback.ConnectorKey, ConnectionKey: fallback.ConnectionKey, Operation: fallback.Operation, Content: rendered})
	}
	priority := func(channel string) int {
		for index, candidate := range decision.FallbackOrder {
			if candidate == channel {
				return index
			}
		}
		return len(decision.FallbackOrder) + 1
	}
	sort.SliceStable(result, func(i, j int) bool { return priority(result[i].Content.Channel) < priority(result[j].Content.Channel) })
	return result, nil
}

func (p *Processor) recordFailure(ctx context.Context, plan Plan, now time.Time) error {
	updatedAt := notification.Timestamp(now)
	attempt := plan.AttemptCount + 1
	policy := worker.RetryPolicy{MaxAttempts: maximumAttempts, BaseDelay: time.Minute, MaxDelay: 32 * time.Minute, Backoff: worker.BackoffExponential}
	if !policy.Allows(attempt, now) {
		return p.plans.FailPlan(ctx, plan, "backend.notification.channel_plan_failed", updatedAt)
	}
	return p.plans.RetryPlan(ctx, plan, "backend.notification.channel_plan_failed", notification.Timestamp(now.Add(policy.Delay(attempt, nil))), updatedAt)
}

func digestCandidates(plans []Plan, first Plan, consumed map[string]bool) []Plan {
	maximum := first.DigestMaximumItems
	if maximum <= 0 || maximum > 100 {
		maximum = 50
	}
	result := make([]Plan, 0, maximum)
	for _, plan := range plans {
		if len(result) >= maximum {
			break
		}
		if !consumed[plan.ID] && plan.WorkspaceID == first.WorkspaceID && plan.DeliveryMode == "digest" && plan.DigestKey == first.DigestKey &&
			plan.NextAttemptAt == first.NextAttemptAt && plan.Channel == first.Channel && plan.TemplateKey == first.TemplateKey && plan.Locale == first.Locale &&
			plan.ConnectorKey == first.ConnectorKey && plan.ConnectionKey == first.ConnectionKey && plan.Operation == first.Operation && sameRecipients(plan.RecipientUserIDs, first.RecipientUserIDs) {
			result = append(result, plan)
		}
	}
	return result
}

func sameRecipients(left, right []notification.UserID) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func digestPlan(plans []Plan) Plan {
	result := plans[0]
	eventIDs, lines := make([]string, 0, len(plans)), make([]string, 0, len(plans))
	for _, plan := range plans {
		eventIDs = append(eventIDs, plan.EventID)
		line := strings.TrimSpace(plan.DigestItemTitle)
		if body := strings.TrimSpace(plan.DigestItemBody); body != "" {
			line += ": " + body
		}
		lines = append(lines, line)
	}
	result.ID = "digest:" + result.DigestKey + ":" + result.NextAttemptAt
	result.Variables = cloneVariables(result.Variables)
	result.Variables["digest_count"] = len(plans)
	result.Variables["digest_items_text"] = strings.Join(lines, "\n")
	result.Variables["digest_event_ids"] = strings.Join(eventIDs, ",")
	return result
}

func cloneVariables(source map[string]any) map[string]any {
	result := make(map[string]any, len(source)+3)
	for key, value := range source {
		result[key] = value
	}
	return result
}
