package delivery_test

import (
	"context"
	"testing"
	"time"

	"github.com/domainry/domainry-notification/internal/domain/delivery/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

type policyStore struct {
	policy       delivery.Policy
	preferences  map[notification.UserID]delivery.RecipientPreference
	reservations []delivery.Reservation
}

func (s *policyStore) GetPolicy(context.Context) (delivery.Policy, error) { return s.policy, nil }
func (s *policyStore) SavePolicy(_ context.Context, value delivery.Policy) (delivery.Policy, error) {
	s.policy = value
	return value, nil
}
func (s *policyStore) ListRecipientPreferences(context.Context, notification.WorkspaceID) ([]delivery.RecipientPreference, error) {
	return nil, nil
}
func (s *policyStore) GetRecipientPreference(_ context.Context, _ notification.WorkspaceID, recipient notification.UserID) (delivery.RecipientPreference, bool, error) {
	value, found := s.preferences[recipient]
	return value, found, nil
}
func (s *policyStore) SaveRecipientPreference(_ context.Context, _ notification.WorkspaceID, value delivery.RecipientPreference) (delivery.RecipientPreference, error) {
	return value, nil
}
func (s *policyStore) ReserveBatch(_ context.Context, _ notification.WorkspaceID, values []delivery.Reservation, _, _ int) error {
	s.reservations = append(s.reservations, values...)
	return nil
}

func TestPolicyManagerEvaluatesPreferencesReservationsAndQuietHours(t *testing.T) {
	store := &policyStore{policy: delivery.Policy{Enabled: true, QuietHoursEnabled: true, QuietStart: "22:00", QuietEnd: "08:00", Timezone: "Asia/Shanghai",
		MaxPerRecipientPerHour: 20, DedupeWindowSeconds: 300, FallbackChannels: []string{"email"}}, preferences: map[notification.UserID]delivery.RecipientPreference{}}
	manager, err := delivery.NewPolicyManager(delivery.PolicyManagerDependencies{Store: store, Clock: clock{now: time.Date(2026, 8, 24, 15, 0, 0, 0, time.UTC)}})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := manager.EvaluateDelivery(t.Context(), delivery.Evaluation{WorkspaceID: "workspace-1", TemplateKey: "workflow.failed", Channel: "email",
		Recipients: []notification.UserID{"user-1", "user-1", "user-2"}, DedupeKey: "run-1"})
	if err != nil || len(store.reservations) != 2 || decision.DeliverAfter != "2026-08-25T00:00:00.000000000Z" || len(decision.FallbackOrder) != 1 {
		t.Fatalf("decision=%+v reservations=%+v err=%v", decision, store.reservations, err)
	}
	if store.reservations[0].ID == store.reservations[1].ID {
		t.Fatal("recipient reservations must have distinct identities")
	}
}

func TestPolicyManagerRejectsDisabledRecipientChannel(t *testing.T) {
	store := &policyStore{policy: delivery.Policy{Enabled: true, MaxPerRecipientPerHour: 20}, preferences: map[notification.UserID]delivery.RecipientPreference{
		"user-1": {RecipientKey: "user-1", EnabledChannels: map[string]bool{"email": false}},
	}}
	manager, _ := delivery.NewPolicyManager(delivery.PolicyManagerDependencies{Store: store, Clock: clock{now: time.Now()}})
	_, err := manager.EvaluateDelivery(t.Context(), delivery.Evaluation{WorkspaceID: "workspace-1", TemplateKey: "workflow.failed", Channel: "email", Recipients: []notification.UserID{"user-1"}})
	if notification.ErrorCode(err) != "backend.notification.recipient_channel_disabled" || len(store.reservations) != 0 {
		t.Fatalf("err=%v reservations=%+v", err, store.reservations)
	}
}

func TestValidatePolicyRejectsInvalidOperationalBounds(t *testing.T) {
	policy := delivery.Policy{MaxPerRecipientPerHour: 20, DedupeWindowSeconds: 300, Timezone: "Asia/Shanghai", QuietStart: "22:00", QuietEnd: "08:00"}
	if err := delivery.ValidatePolicy(policy); err != nil {
		t.Fatal(err)
	}
	policy.MaxPerRecipientPerHour = 0
	if code := notification.ErrorCode(delivery.ValidatePolicy(policy)); code != "backend.notification.policy_frequency_invalid" {
		t.Fatalf("code=%q", code)
	}
}
