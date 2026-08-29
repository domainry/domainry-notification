package deliverystore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/domainry/domainry-notification/internal/domain/delivery/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-orm/builder"
)

var _ delivery.PolicyStore = (*Store)(nil)

const defaultDeliveryPolicyKey = "default"

func (s *Store) GetPolicy(ctx context.Context) (delivery.Policy, error) {
	query, args, err := builder.NewSelectBuilder(s.Renderer, "notification_delivery_policy").Columns("payload_json").Where(builder.Equal("policy_key", defaultDeliveryPolicyKey)).Build()
	if err != nil {
		return delivery.Policy{}, err
	}
	var raw string
	if err := s.Database.QueryRowContext(ctx, query, args...).Scan(&raw); errors.Is(err, sql.ErrNoRows) {
		return defaultPolicy(), nil
	} else if err != nil {
		return delivery.Policy{}, fmt.Errorf("get notification delivery policy: %w", err)
	}
	var policy delivery.Policy
	if err := json.Unmarshal([]byte(raw), &policy); err != nil {
		return policy, fmt.Errorf("decode notification delivery policy: %w", err)
	}
	return policy, nil
}

func (s *Store) SavePolicy(ctx context.Context, policy delivery.Policy) (delivery.Policy, error) {
	raw, err := json.Marshal(policy)
	if err != nil {
		return policy, fmt.Errorf("encode notification delivery policy: %w", err)
	}
	query, args, err := builder.NewUpdateBuilder(s.Renderer, "notification_delivery_policy").Set("payload_json", string(raw)).Set("updated_by", policy.UpdatedBy).Set("updated_at", policy.UpdatedAt).Where(builder.Equal("policy_key", defaultDeliveryPolicyKey)).Build()
	if err != nil {
		return policy, err
	}
	result, err := s.Database.ExecContext(ctx, query, args...)
	if err != nil {
		return policy, fmt.Errorf("update notification delivery policy: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return policy, err
	}
	if count == 0 {
		_, err = s.Insert(ctx, s.Database, "notification_delivery_policy", []string{"policy_key", "payload_json", "updated_by", "updated_at"},
			defaultDeliveryPolicyKey, string(raw), policy.UpdatedBy, policy.UpdatedAt)
		if err != nil {
			return policy, fmt.Errorf("insert notification delivery policy: %w", err)
		}
	}
	return policy, nil
}

func (s *Store) ListRecipientPreferences(ctx context.Context, workspaceID notification.WorkspaceID) ([]delivery.RecipientPreference, error) {
	if workspaceID == "" {
		return nil, fmt.Errorf("notification recipient preference workspace is required")
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	query, args, err := builder.NewSelectBuilder(s.Renderer, "notification_recipient_preferences").Columns("payload_json").Where(builder.Equal("workspace_id", workspaceID.String())).OrderBy(builder.Ascending("recipient_key")).Build()
	if err != nil {
		return nil, err
	}
	rows, err := s.Database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list notification recipient preferences: %w", err)
	}
	defer rows.Close()
	values := []delivery.RecipientPreference{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var value delivery.RecipientPreference
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return nil, fmt.Errorf("decode notification recipient preference: %w", err)
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) GetRecipientPreference(ctx context.Context, workspaceID notification.WorkspaceID, recipientID notification.UserID) (delivery.RecipientPreference, bool, error) {
	if workspaceID == "" || recipientID == "" {
		return delivery.RecipientPreference{}, false, fmt.Errorf("notification recipient preference identity is required")
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	query, args, err := builder.NewSelectBuilder(s.Renderer, "notification_recipient_preferences").Columns("payload_json").Where(builder.And(builder.Equal("workspace_id", workspaceID.String()), builder.Equal("recipient_key", recipientID.String()))).Build()
	if err != nil {
		return delivery.RecipientPreference{}, false, err
	}
	var raw string
	if err := s.Database.QueryRowContext(ctx, query, args...).Scan(&raw); errors.Is(err, sql.ErrNoRows) {
		return delivery.RecipientPreference{}, false, nil
	} else if err != nil {
		return delivery.RecipientPreference{}, false, fmt.Errorf("get notification recipient preference: %w", err)
	}
	var value delivery.RecipientPreference
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return value, false, fmt.Errorf("decode notification recipient preference: %w", err)
	}
	return value, true, nil
}

func (s *Store) SaveRecipientPreference(ctx context.Context, workspaceID notification.WorkspaceID, value delivery.RecipientPreference) (delivery.RecipientPreference, error) {
	value.RecipientKey = strings.TrimSpace(value.RecipientKey)
	if workspaceID == "" || value.RecipientKey == "" {
		return value, fmt.Errorf("notification recipient preference identity is required")
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	raw, err := json.Marshal(value)
	if err != nil {
		return value, fmt.Errorf("encode notification recipient preference: %w", err)
	}
	query, args, err := builder.NewUpdateBuilder(s.Renderer, "notification_recipient_preferences").Set("payload_json", string(raw)).Set("updated_by", value.UpdatedBy).Set("updated_at", value.UpdatedAt).Where(builder.And(builder.Equal("workspace_id", workspaceID.String()), builder.Equal("recipient_key", value.RecipientKey))).Build()
	if err != nil {
		return value, err
	}
	result, err := s.Database.ExecContext(ctx, query, args...)
	if err != nil {
		return value, fmt.Errorf("update notification recipient preference: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return value, err
	}
	if count == 0 {
		_, err = s.Insert(ctx, s.Database, "notification_recipient_preferences", []string{"workspace_id", "recipient_key", "payload_json", "updated_by", "updated_at"},
			workspaceID.String(), value.RecipientKey, string(raw), value.UpdatedBy, value.UpdatedAt)
		if err != nil {
			return value, fmt.Errorf("insert notification recipient preference: %w", err)
		}
	}
	return value, nil
}

func (s *Store) ReserveBatch(ctx context.Context, workspaceID notification.WorkspaceID, reservations []delivery.Reservation, hourlyLimit, dedupeWindowSeconds int) error {
	if workspaceID == "" || len(reservations) == 0 || hourlyLimit < 1 {
		return fmt.Errorf("notification delivery reservations and positive hourly limit are required")
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	tx, err := s.Database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, reservation := range reservations {
		if strings.TrimSpace(reservation.ID) == "" || reservation.RecipientKey == "" {
			return fmt.Errorf("notification delivery reservation identity is required")
		}
		createdAt, parseErr := time.Parse(time.RFC3339Nano, reservation.CreatedAt)
		if parseErr != nil {
			return fmt.Errorf("parse notification delivery reservation timestamp: %w", parseErr)
		}
		var count int
		frequency, frequencyArgs, buildErr := builder.NewSelectBuilder(s.Renderer, "notification_delivery_reservations").Projections(builder.Project(builder.CountAll())).Where(builder.And(builder.Equal("workspace_id", workspaceID.String()), builder.Equal("recipient_key", reservation.RecipientKey.String()), builder.Equal("channel", reservation.Channel), builder.GreaterThanOrEqual("created_at", notification.Timestamp(createdAt.Add(-time.Hour))))).Build()
		if buildErr != nil {
			return buildErr
		}
		if err := tx.QueryRowContext(ctx, frequency, frequencyArgs...).Scan(&count); err != nil {
			return err
		}
		if count >= hourlyLimit {
			return delivery.ErrFrequencyExceeded
		}
		if strings.TrimSpace(reservation.DedupeKey) != "" && dedupeWindowSeconds > 0 {
			dedupe, dedupeArgs, buildErr := builder.NewSelectBuilder(s.Renderer, "notification_delivery_reservations").Projections(builder.Project(builder.CountAll())).Where(builder.And(builder.Equal("workspace_id", workspaceID.String()), builder.Equal("recipient_key", reservation.RecipientKey.String()), builder.Equal("template_key", reservation.TemplateKey), builder.Equal("channel", reservation.Channel), builder.Equal("dedupe_key", reservation.DedupeKey), builder.GreaterThanOrEqual("created_at", notification.Timestamp(createdAt.Add(-time.Duration(dedupeWindowSeconds)*time.Second))))).Build()
			if buildErr != nil {
				return buildErr
			}
			if err := tx.QueryRowContext(ctx, dedupe, dedupeArgs...).Scan(&count); err != nil {
				return err
			}
			if count > 0 {
				return delivery.ErrDuplicate
			}
		}
		_, err = s.Insert(ctx, tx, "notification_delivery_reservations", []string{"id", "workspace_id", "recipient_key", "template_key", "channel", "dedupe_key", "created_at"},
			reservation.ID, workspaceID.String(), reservation.RecipientKey.String(), reservation.TemplateKey, reservation.Channel, reservation.DedupeKey, reservation.CreatedAt)
		if err != nil {
			return fmt.Errorf("insert notification delivery reservation: %w", err)
		}
	}
	return tx.Commit()
}

func defaultPolicy() delivery.Policy {
	return delivery.Policy{Enabled: true, QuietStart: "22:00", QuietEnd: "08:00", Timezone: "Asia/Shanghai", MaxPerRecipientPerHour: 20,
		DedupeWindowSeconds: 300, FallbackChannels: []string{"collaboration", "email"}}
}
