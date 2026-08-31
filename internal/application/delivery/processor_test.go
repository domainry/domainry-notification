package delivery_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/domainry/domainry-notification/internal/application/delivery"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-notification/internal/domain/template/service"
)

type planStore struct {
	plans     []delivery.Plan
	completed []delivery.Plan
	retried   []delivery.Plan
	failed    []delivery.Plan
	cancelled []delivery.Plan
	terminal  bool
	outboxID  string
}

func (s *planStore) GetPlan(_ context.Context, workspace notification.WorkspaceID, id string) (delivery.Plan, bool, error) {
	for _, plan := range s.plans {
		if plan.WorkspaceID == workspace && plan.ID == id {
			return plan, true, nil
		}
	}
	return delivery.Plan{}, false, nil
}
func (s *planStore) ListDuePlans(context.Context, string, int) ([]delivery.Plan, error) {
	return append([]delivery.Plan(nil), s.plans...), nil
}
func (s *planStore) ClaimPlan(_ context.Context, workspace notification.WorkspaceID, id, owner, _, expires string) (delivery.Plan, bool, error) {
	plan, found, err := s.GetPlan(context.Background(), workspace, id)
	if found {
		plan.Status, plan.LeaseOwner, plan.LeaseExpiresAt, plan.FencingToken = "processing", owner, expires, plan.FencingToken+1
	}
	return plan, found, err
}
func (s *planStore) IsActionTerminal(context.Context, delivery.Plan) (bool, error) {
	return s.terminal, nil
}
func (s *planStore) CancelPlan(_ context.Context, plan delivery.Plan, _, _ string) error {
	s.cancelled = append(s.cancelled, plan)
	return nil
}
func (s *planStore) CompletePlan(_ context.Context, plan delivery.Plan, outboxID, _ string) error {
	s.completed, s.outboxID = append(s.completed, plan), outboxID
	return nil
}
func (s *planStore) CompletePlanBatch(_ context.Context, plans []delivery.Plan, outboxID, _ string) error {
	s.completed, s.outboxID = append(s.completed, plans...), outboxID
	return nil
}
func (s *planStore) RetryPlan(_ context.Context, plan delivery.Plan, _, _, _ string) error {
	s.retried = append(s.retried, plan)
	return nil
}
func (s *planStore) FailPlan(_ context.Context, plan delivery.Plan, _, _ string) error {
	s.failed = append(s.failed, plan)
	return nil
}

type renderer struct {
	request template.RenderRequest
	err     error
}

func (r *renderer) Render(_ context.Context, request template.RenderRequest) (template.Rendered, error) {
	r.request = request
	return template.Rendered{TemplateKey: request.TemplateKey, Recipients: []string{"user@example.com"}}, r.err
}

type dispatcher struct {
	request delivery.DispatchRequest
	err     error
}

func (d *dispatcher) Dispatch(_ context.Context, request delivery.DispatchRequest) (delivery.DispatchReceipt, error) {
	d.request = request
	return delivery.DispatchReceipt{MessageID: "outbox-1", AcceptedAt: request.CreatedAt}, d.err
}

type clock struct{ now time.Time }

func (c clock) Now() time.Time { return c.now }

type allowPolicy struct{ decision delivery.Decision }

func (p allowPolicy) EvaluateDelivery(context.Context, delivery.Evaluation) (delivery.Decision, error) {
	return p.decision, nil
}

type fallbackRenderer struct{ requests []template.RenderRequest }

func (r *fallbackRenderer) Render(_ context.Context, request template.RenderRequest) (template.Rendered, error) {
	r.requests = append(r.requests, request)
	if request.TemplateKey == "workflow.failed" {
		return template.Rendered{
			Channel: "email", TemplateKey: request.TemplateKey,
			Fallbacks: []template.Fallback{
				{TemplateKey: "fallback.chat", ConnectorKey: "collaboration", Operation: "send_message"},
				{TemplateKey: "fallback.sms", ConnectorKey: "sms", Operation: "send_message"},
			},
		}, nil
	}
	channel := "collaboration"
	if request.TemplateKey == "fallback.sms" {
		channel = "sms"
	}
	return template.Rendered{Channel: channel, TemplateKey: request.TemplateKey}, nil
}

func TestProcessorRendersDispatchesAndCompletesImmediatePlan(t *testing.T) {
	plan := immediatePlan("plan-1")
	plans, render, dispatch := &planStore{plans: []delivery.Plan{plan}}, &renderer{}, &dispatcher{}
	processor, err := delivery.NewProcessor(delivery.ProcessorDependencies{Plans: plans, Renderer: render, Dispatcher: dispatch, Policy: allowPolicy{}, Clock: clock{now: time.Date(2026, 8, 24, 1, 0, 0, 0, time.UTC)}, WorkerID: "worker-1"})
	if err != nil {
		t.Fatal(err)
	}
	done, err := processor.Process(t.Context(), plan.WorkspaceID, plan.ID)
	if err != nil || !done || len(plans.completed) != 1 || plans.outboxID != "outbox-1" {
		t.Fatalf("done=%v completed=%+v err=%v", done, plans.completed, err)
	}
	if render.request.TemplateKey != plan.TemplateKey || dispatch.request.DeduplicationKey != plan.ID || dispatch.request.ConnectorKey != plan.ConnectorKey {
		t.Fatalf("render=%+v dispatch=%+v", render.request, dispatch.request)
	}
}

func TestProcessorRetriesDispatchFailureAndCancelsTerminalAction(t *testing.T) {
	failedPlan := immediatePlan("plan-failed")
	plans := &planStore{plans: []delivery.Plan{failedPlan}}
	processor, _ := delivery.NewProcessor(delivery.ProcessorDependencies{Plans: plans, Renderer: &renderer{err: errors.New("render failed")}, Dispatcher: &dispatcher{}, Policy: allowPolicy{}, Clock: clock{now: time.Now()}, WorkerID: "worker-1"})
	if done, err := processor.Process(t.Context(), failedPlan.WorkspaceID, failedPlan.ID); err != nil || done || len(plans.retried) != 1 {
		t.Fatalf("done=%v retried=%+v err=%v", done, plans.retried, err)
	}
	terminal := immediatePlan("plan-terminal")
	terminal.CancelWhenActionTerminal = true
	plans = &planStore{plans: []delivery.Plan{terminal}, terminal: true}
	processor, _ = delivery.NewProcessor(delivery.ProcessorDependencies{Plans: plans, Renderer: &renderer{}, Dispatcher: &dispatcher{}, Policy: allowPolicy{}, Clock: clock{now: time.Now()}, WorkerID: "worker-1"})
	if done, err := processor.Process(t.Context(), terminal.WorkspaceID, terminal.ID); err != nil || done || len(plans.cancelled) != 1 {
		t.Fatalf("done=%v cancelled=%+v err=%v", done, plans.cancelled, err)
	}
}

func TestProcessorCombinesCompatibleDigestPlans(t *testing.T) {
	first, second := immediatePlan("plan-1"), immediatePlan("plan-2")
	for _, plan := range []*delivery.Plan{&first, &second} {
		plan.DeliveryMode, plan.DigestKey, plan.NextAttemptAt, plan.DigestItemTitle = "digest", "daily", "2026-08-24T02:00:00.000000000Z", plan.ID
	}
	plans, render, dispatch := &planStore{plans: []delivery.Plan{first, second}}, &renderer{}, &dispatcher{}
	processor, _ := delivery.NewProcessor(delivery.ProcessorDependencies{Plans: plans, Renderer: render, Dispatcher: dispatch, Policy: allowPolicy{}, Clock: clock{now: time.Now()}, WorkerID: "worker-1"})
	processed, err := processor.ProcessDue(t.Context(), 10)
	if err != nil || processed != 2 || len(plans.completed) != 2 || render.request.Variables["digest_count"] != 2 {
		t.Fatalf("processed=%d completed=%+v render=%+v err=%v", processed, plans.completed, render.request, err)
	}
}

func TestProcessorDoesNotCombineDigestPlansAcrossDispatchIdentity(t *testing.T) {
	first, second := immediatePlan("plan-1"), immediatePlan("plan-2")
	for _, plan := range []*delivery.Plan{&first, &second} {
		plan.DeliveryMode, plan.DigestKey, plan.NextAttemptAt = "digest", "daily", "2026-08-24T02:00:00.000000000Z"
	}
	second.ConnectionKey = "secondary"
	plans := &planStore{plans: []delivery.Plan{first, second}}
	dispatch := &countingDispatcher{}
	processor, _ := delivery.NewProcessor(delivery.ProcessorDependencies{Plans: plans, Renderer: &renderer{}, Dispatcher: dispatch, Policy: allowPolicy{}, Clock: clock{now: time.Now()}, WorkerID: "worker-1"})
	processed, err := processor.ProcessDue(t.Context(), 10)
	if err != nil || processed != 2 || dispatch.count != 2 {
		t.Fatalf("processed=%d dispatches=%d err=%v", processed, dispatch.count, err)
	}
}

func TestProcessorEvaluatesPolicyAndSnapshotsFallbacksBeforeDispatch(t *testing.T) {
	plan := immediatePlan("plan-fallback")
	plans, render, dispatch := &planStore{plans: []delivery.Plan{plan}}, &fallbackRenderer{}, &dispatcher{}
	decision := delivery.Decision{DeliverAfter: "2026-08-24T08:00:00.000000000Z", FallbackOrder: []string{"sms", "collaboration"}}
	processor, err := delivery.NewProcessor(delivery.ProcessorDependencies{
		Plans: plans, Renderer: render, Dispatcher: dispatch, Policy: allowPolicy{decision: decision}, Clock: clock{now: time.Now()}, WorkerID: "worker-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if done, err := processor.Process(t.Context(), plan.WorkspaceID, plan.ID); err != nil || !done {
		t.Fatalf("done=%v err=%v", done, err)
	}
	if dispatch.request.Decision.DeliverAfter != decision.DeliverAfter || len(dispatch.request.Fallbacks) != 2 {
		t.Fatalf("dispatch=%+v", dispatch.request)
	}
	if dispatch.request.Fallbacks[0].Content.Channel != "sms" || dispatch.request.Fallbacks[1].Content.Channel != "collaboration" {
		t.Fatalf("fallback order=%+v", dispatch.request.Fallbacks)
	}
	if len(render.requests) != 3 {
		t.Fatalf("render requests=%+v", render.requests)
	}
}

type countingDispatcher struct{ count int }

func (d *countingDispatcher) Dispatch(_ context.Context, request delivery.DispatchRequest) (delivery.DispatchReceipt, error) {
	d.count++
	return delivery.DispatchReceipt{MessageID: request.PlanID}, nil
}

func immediatePlan(id string) delivery.Plan {
	return delivery.Plan{ID: id, WorkspaceID: "workspace-1", EventID: "event-" + id, Channel: "email", TemplateKey: "workflow.failed",
		ConnectorKey: "email", ConnectionKey: "primary", Operation: "send_email", RecipientUserIDs: []notification.UserID{"user-1"},
		Variables: map[string]any{"name": "Ada"}, Status: "queued", CreatedAt: "2026-08-24T00:00:00.000000000Z", UpdatedAt: "2026-08-24T00:00:00.000000000Z"}
}
