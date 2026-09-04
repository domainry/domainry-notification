package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

var _ PolicyEvaluator = (*PolicyManager)(nil)

type PolicyManagerDependencies struct {
	Store PolicyStore
	Clock notification.Clock
}

// PolicyManager owns policy validation, recipient preferences, rate/dedupe
// reservation, and quiet-hours evaluation. Host authorization remains outside.
type PolicyManager struct {
	store PolicyStore
	clock notification.Clock
}

func NewPolicyManager(dependencies PolicyManagerDependencies) (*PolicyManager, error) {
	if dependencies.Store == nil || dependencies.Clock == nil {
		return nil, fmt.Errorf("notification delivery policy dependencies are required")
	}
	return &PolicyManager{store: dependencies.Store, clock: dependencies.Clock}, nil
}

func (m *PolicyManager) GetPolicy(ctx context.Context) (Policy, error) {
	return m.store.GetPolicy(ctx)
}

func (m *PolicyManager) SavePolicy(ctx context.Context, value Policy, actor string) (Policy, error) {
	if err := ValidatePolicy(value); err != nil {
		return Policy{}, err
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return Policy{}, notification.NewError(notification.ErrorInvalid, "backend.notification.policy_actor_required", nil, nil)
	}
	value.UpdatedBy, value.UpdatedAt = actor, notification.Timestamp(m.clock.Now())
	return m.store.SavePolicy(ctx, value)
}

func (m *PolicyManager) ListRecipientPreferences(ctx context.Context, workspaceID notification.WorkspaceID) ([]RecipientPreference, error) {
	return m.store.ListRecipientPreferences(ctx, workspaceID)
}

func (m *PolicyManager) GetRecipientPreference(ctx context.Context, workspaceID notification.WorkspaceID, recipientID notification.UserID) (RecipientPreference, bool, error) {
	return m.store.GetRecipientPreference(ctx, workspaceID, recipientID)
}

func (m *PolicyManager) SaveRecipientPreference(ctx context.Context, workspaceID notification.WorkspaceID, value RecipientPreference, actor string) (RecipientPreference, error) {
	value.RecipientKey, actor = strings.TrimSpace(value.RecipientKey), strings.TrimSpace(actor)
	if workspaceID == "" || value.RecipientKey == "" || actor == "" {
		return RecipientPreference{}, notification.NewError(notification.ErrorInvalid, "backend.notification.preference_identity_required", nil, nil)
	}
	if value.EnabledChannels == nil {
		value.EnabledChannels = map[string]bool{}
	}
	value.MutedTemplateKeys = uniqueStrings(value.MutedTemplateKeys)
	value.UpdatedBy, value.UpdatedAt = actor, notification.Timestamp(m.clock.Now())
	return m.store.SaveRecipientPreference(ctx, workspaceID, value)
}

func (m *PolicyManager) EvaluateDelivery(ctx context.Context, evaluation Evaluation) (Decision, error) {
	if evaluation.WorkspaceID == "" || strings.TrimSpace(evaluation.TemplateKey) == "" || strings.TrimSpace(evaluation.Channel) == "" {
		return Decision{}, notification.NewError(notification.ErrorInvalid, "backend.notification.delivery_evaluation_invalid", nil, nil)
	}
	recipients := uniqueUsers(evaluation.Recipients)
	if len(recipients) == 0 {
		return Decision{}, notification.NewError(notification.ErrorInvalid, "backend.notification.recipients_required", nil, nil)
	}
	policy, err := m.store.GetPolicy(ctx)
	if err != nil || !policy.Enabled {
		return Decision{}, err
	}
	now := m.clock.Now().UTC()
	reservations := make([]Reservation, 0, len(recipients))
	for _, recipient := range recipients {
		preference, found, err := m.store.GetRecipientPreference(ctx, evaluation.WorkspaceID, recipient)
		if err != nil {
			return Decision{}, err
		}
		if found {
			if enabled, configured := preference.EnabledChannels[evaluation.Channel]; configured && !enabled {
				return Decision{}, notification.NewError(notification.ErrorConflict, "backend.notification.recipient_channel_disabled", nil,
					map[string]any{"recipient": recipient.String(), "channel": evaluation.Channel})
			}
			for _, key := range preference.MutedTemplateKeys {
				if key == evaluation.TemplateKey {
					return Decision{}, notification.NewError(notification.ErrorConflict, "backend.notification.recipient_template_muted", nil,
						map[string]any{"recipient": recipient.String(), "template_key": evaluation.TemplateKey})
				}
			}
		}
		createdAt := notification.Timestamp(now)
		reservationKey := strings.TrimSpace(evaluation.ReservationKey)
		if reservationKey == "" {
			reservationKey = createdAt
		}
		identity := strings.Join([]string{evaluation.WorkspaceID.String(), recipient.String(), evaluation.TemplateKey, evaluation.Channel, evaluation.DedupeKey, reservationKey}, "\x00")
		sum := sha256.Sum256([]byte(identity))
		reservations = append(reservations, Reservation{ID: hex.EncodeToString(sum[:]), RecipientKey: recipient, TemplateKey: evaluation.TemplateKey,
			Channel: evaluation.Channel, DedupeKey: evaluation.DedupeKey, CreatedAt: createdAt})
	}
	if err := m.store.ReserveBatch(ctx, evaluation.WorkspaceID, reservations, policy.MaxPerRecipientPerHour, policy.DedupeWindowSeconds); err != nil {
		if errors.Is(err, ErrFrequencyExceeded) {
			return Decision{}, notification.NewError(notification.ErrorConflict, "backend.notification.delivery_frequency_exceeded", err, nil)
		}
		if errors.Is(err, ErrDuplicate) {
			return Decision{}, notification.NewError(notification.ErrorConflict, "backend.notification.delivery_duplicate", err, nil)
		}
		return Decision{}, err
	}
	decision := Decision{FallbackOrder: append([]string(nil), policy.FallbackChannels...)}
	if policy.QuietHoursEnabled {
		decision.DeliverAfter = NextQuietHoursEnd(now, policy)
	}
	return decision, nil
}

func ValidatePolicy(value Policy) error {
	if value.MaxPerRecipientPerHour < 1 || value.MaxPerRecipientPerHour > 10000 {
		return notification.NewError(notification.ErrorInvalid, "backend.notification.policy_frequency_invalid", nil, nil)
	}
	if value.DedupeWindowSeconds < 0 || value.DedupeWindowSeconds > 86400 {
		return notification.NewError(notification.ErrorInvalid, "backend.notification.policy_dedupe_invalid", nil, nil)
	}
	if _, err := time.LoadLocation(value.Timezone); err != nil {
		return notification.NewError(notification.ErrorInvalid, "backend.notification.policy_timezone_invalid", err, nil)
	}
	for _, clock := range []string{value.QuietStart, value.QuietEnd} {
		if _, err := time.Parse("15:04", clock); err != nil {
			return notification.NewError(notification.ErrorInvalid, "backend.notification.policy_quiet_hours_invalid", err, nil)
		}
	}
	return nil
}

func NextQuietHoursEnd(now time.Time, policy Policy) string {
	location, err := time.LoadLocation(policy.Timezone)
	if err != nil {
		return ""
	}
	local := now.In(location)
	start, startErr := time.Parse("15:04", policy.QuietStart)
	end, endErr := time.Parse("15:04", policy.QuietEnd)
	if startErr != nil || endErr != nil {
		return ""
	}
	minute, startMinute, endMinute := local.Hour()*60+local.Minute(), start.Hour()*60+start.Minute(), end.Hour()*60+end.Minute()
	inQuiet := startMinute < endMinute && minute >= startMinute && minute < endMinute || startMinute >= endMinute && (minute >= startMinute || minute < endMinute)
	if !inQuiet {
		return ""
	}
	endAt := time.Date(local.Year(), local.Month(), local.Day(), end.Hour(), end.Minute(), 0, 0, location)
	if !endAt.After(local) {
		endAt = endAt.AddDate(0, 0, 1)
	}
	return notification.Timestamp(endAt)
}

func uniqueUsers(values []notification.UserID) []notification.UserID {
	seen := map[notification.UserID]bool{}
	result := make([]notification.UserID, 0, len(values))
	for _, value := range values {
		value = notification.UserID(strings.TrimSpace(value.String()))
		if value != "" && !seen[value] {
			seen[value], result = true, append(result, value)
		}
	}
	return result
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value], result = true, append(result, value)
		}
	}
	return result
}
