package deliverystore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/domainry/domainry-foundation/mutation"
	metadatasdk "github.com/domainry/domainry-metadata-sdk"
	"github.com/domainry/domainry-notification/internal/domain/delivery/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-orm/query"
)

var _ delivery.PolicyStore = (*Store)(nil)

const (
	deliveryPolicyDefinitionKind   = "delivery_policy"
	deliveryPolicyDefinitionSchema = "domainry-notification-delivery-policy-v1"
	deliveryPolicyDefinitionSource = "notification_policy"
)

const (
	notificationUserSettingsTable  = "_notification_user_settings"
	recipientPreferenceSettingKind = "delivery_preference"
	recipientPreferenceDefaultKey  = "default"
)

func (s *Store) GetPolicy(ctx context.Context) (delivery.Policy, error) {
	if s.definitions == nil {
		return delivery.Policy{}, notification.NewError(notification.ErrorUnavailable, "backend.notification.policy_store_unavailable", nil, nil)
	}
	definition, found, err := s.definitions.Get(ctx, metadatasdk.DefinitionOwnerNotification, deliveryPolicyDefinitionKind, s.deliveryPolicyDefinitionKey())
	if err != nil {
		return delivery.Policy{}, fmt.Errorf("get notification delivery policy definition: %w", err)
	}
	if !found {
		return defaultPolicy(), nil
	}
	var policy delivery.Policy
	if err := json.Unmarshal(definition.Payload, &policy); err != nil {
		return policy, fmt.Errorf("decode notification delivery policy: %w", err)
	}
	policy.Revision = definition.CurrentVersionID
	return policy, nil
}

func (s *Store) SavePolicy(ctx context.Context, policy delivery.Policy) (delivery.Policy, error) {
	if s.definitions == nil {
		return delivery.Policy{}, notification.NewError(notification.ErrorUnavailable, "backend.notification.policy_store_unavailable", nil, nil)
	}
	expectedRevision := strings.TrimSpace(policy.Revision)
	if expectedRevision == "" {
		return delivery.Policy{}, notification.NewError(notification.ErrorInvalid, "backend.notification.policy_revision_required", nil, nil)
	}
	payload := policy
	payload.Revision = ""
	raw, err := json.Marshal(payload)
	if err != nil {
		return policy, fmt.Errorf("encode notification delivery policy: %w", err)
	}
	payloadHash := sha256.Sum256(raw)
	contentHash := hex.EncodeToString(payloadHash[:])
	result, err := s.definitions.Publish(ctx, metadatasdk.DefinitionPublishCommand{
		Owner: metadatasdk.DefinitionOwnerNotification, ResourceType: deliveryPolicyDefinitionKind,
		ResourceKey: s.deliveryPolicyDefinitionKey(), ExpectedCurrentVersionID: expectedRevision,
		SchemaVersion: deliveryPolicyDefinitionSchema + ":" + contentHash, SchemaHash: contentHash,
		Name: "Notification delivery policy", Payload: raw,
		SourceKind: deliveryPolicyDefinitionSource, SourceID: s.workspaceID.String(), PublishedBy: strings.TrimSpace(policy.UpdatedBy),
	})
	if err != nil {
		var metadataError *metadatasdk.Error
		if errors.As(err, &metadataError) && metadataError.StatusCode == 409 {
			return delivery.Policy{}, notification.NewError(notification.ErrorConflict, "backend.notification.policy_revision_conflict", err, nil)
		}
		return delivery.Policy{}, fmt.Errorf("publish notification delivery policy definition: %w", err)
	}
	policy.Revision = result.CurrentVersionID
	return policy, nil
}

func (s *Store) deliveryPolicyDefinitionKey() string {
	return "workspace:" + s.workspaceID.String()
}

func (s *Store) ListRecipientPreferences(ctx context.Context, workspaceID notification.WorkspaceID) ([]delivery.RecipientPreference, error) {
	if workspaceID == "" {
		return nil, fmt.Errorf("notification recipient preference workspace is required")
	}
	if err := s.requireWorkspace(workspaceID); err != nil {
		return nil, err
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	predicate := query.Equal("setting_kind", recipientPreferenceSettingKind)
	queryValue, args, err := query.NewWorkspaceSelectBuilder(s.Renderer, notificationUserSettingsTable, workspaceID.String()).Columns("payload_json").Where(predicate).OrderBy(query.Ascending("recipient_user_id")).Build()
	if err != nil {
		return nil, err
	}
	rows, err := s.Database.QueryContext(ctx, queryValue, args...)
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
	if err := s.requireWorkspace(workspaceID); err != nil {
		return delivery.RecipientPreference{}, false, err
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	predicate := query.And(query.Equal("recipient_user_id", recipientID.String()), query.Equal("setting_kind", recipientPreferenceSettingKind), query.Equal("setting_key", recipientPreferenceDefaultKey))
	queryValue, args, err := query.NewWorkspaceSelectBuilder(s.Renderer, notificationUserSettingsTable, workspaceID.String()).Columns("payload_json").Where(predicate).Build()
	if err != nil {
		return delivery.RecipientPreference{}, false, err
	}
	var raw string
	if err := s.Database.QueryRowContext(ctx, queryValue, args...).Scan(&raw); errors.Is(err, sql.ErrNoRows) {
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
	if err := s.requireWorkspace(workspaceID); err != nil {
		return value, err
	}
	ctx = s.workspaceScope.Context(ctx, workspaceID)
	raw, err := json.Marshal(value)
	if err != nil {
		return value, fmt.Errorf("encode notification recipient preference: %w", err)
	}
	tx, err := s.Database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return value, err
	}
	defer tx.Rollback()
	predicate := query.And(query.Equal("recipient_user_id", value.RecipientKey), query.Equal("setting_kind", recipientPreferenceSettingKind), query.Equal("setting_key", recipientPreferenceDefaultKey))
	var existing string
	selectSQL, selectArgs, err := query.NewWorkspaceSelectBuilder(s.Renderer, notificationUserSettingsTable, workspaceID.String()).Columns("recipient_user_id").Where(predicate).Build()
	if err != nil {
		return value, err
	}
	err = tx.QueryRowContext(ctx, selectSQL, selectArgs...).Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = s.WorkspaceInsert(ctx, tx, workspaceID.String(), notificationUserSettingsTable, []string{"workspace_id", "recipient_user_id", "setting_kind", "setting_key", "payload_json", "updated_by", "created_at", "updated_at"},
			workspaceID.String(), value.RecipientKey, recipientPreferenceSettingKind, recipientPreferenceDefaultKey, string(raw), value.UpdatedBy, value.UpdatedAt, value.UpdatedAt)
		if err != nil {
			return value, fmt.Errorf("insert notification recipient preference: %w", err)
		}
		return value, tx.Commit()
	}
	if err != nil {
		return value, err
	}
	queryValue, args, err := query.NewWorkspaceUpdateBuilder(s.Renderer, notificationUserSettingsTable, workspaceID.String()).Set("payload_json", string(raw)).Set("updated_by", value.UpdatedBy).Set("updated_at", value.UpdatedAt).Where(predicate).Build()
	if err != nil {
		return value, err
	}
	result, err := tx.ExecContext(ctx, queryValue, args...)
	if err != nil {
		return value, fmt.Errorf("update notification recipient preference: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return value, err
	}
	if count != 1 {
		return value, mutation.MutationConflict("notification_recipient_preference", value.RecipientKey, mutation.MutationConflictOptimistic, nil)
	}
	return value, tx.Commit()
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
		idempotent, err := s.reservationAlreadyRecorded(ctx, tx, workspaceID, reservation)
		if err != nil {
			return err
		}
		if idempotent {
			continue
		}
		createdAt, parseErr := time.Parse(time.RFC3339Nano, reservation.CreatedAt)
		if parseErr != nil {
			return fmt.Errorf("parse notification delivery reservation timestamp: %w", parseErr)
		}
		var count int
		frequency, frequencyArgs, buildErr := query.NewWorkspaceSelectBuilder(s.Renderer, notificationDeliveriesTable, workspaceID.String()).Projections(query.Project(query.CountAll())).Where(query.And(query.Equal("row_kind", deliveryReservationRowKind), query.Equal("recipient_key", reservation.RecipientKey.String()), query.Equal("channel", reservation.Channel), query.GreaterThanOrEqual("created_at", notification.Timestamp(createdAt.Add(-time.Hour))))).Build()
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
			dedupe, dedupeArgs, buildErr := query.NewWorkspaceSelectBuilder(s.Renderer, notificationDeliveriesTable, workspaceID.String()).Projections(query.Project(query.CountAll())).Where(query.And(query.Equal("row_kind", deliveryReservationRowKind), query.Equal("recipient_key", reservation.RecipientKey.String()), query.Equal("template_key", reservation.TemplateKey), query.Equal("channel", reservation.Channel), query.Equal("dedupe_key", reservation.DedupeKey), query.GreaterThanOrEqual("created_at", notification.Timestamp(createdAt.Add(-time.Duration(dedupeWindowSeconds)*time.Second))))).Build()
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
		_, err = s.WorkspaceInsert(ctx, tx, workspaceID.String(), notificationDeliveriesTable, []string{"id", "workspace_id", "row_kind", "event_id", "recipient_key", "template_key", "channel", "dedupe_key", "status", "payload_json", "attempt_count", "next_attempt_at", "last_error_code", "outbox_message_id", "lease_owner", "lease_expires_at", "fencing_token", "created_at", "updated_at"},
			reservation.ID, workspaceID.String(), deliveryReservationRowKind, "", reservation.RecipientKey.String(), reservation.TemplateKey, reservation.Channel, reservation.DedupeKey, "reserved", `{}`, 0, "", "", "", "", "", 0, reservation.CreatedAt, reservation.CreatedAt)
		if err != nil {
			return fmt.Errorf("insert notification delivery reservation: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Store) reservationAlreadyRecorded(ctx context.Context, queryer *sql.Tx, workspaceID notification.WorkspaceID, reservation delivery.Reservation) (bool, error) {
	lookup, args, err := query.NewWorkspaceSelectBuilder(s.Renderer, notificationDeliveriesTable, workspaceID.String()).
		Columns("recipient_key", "template_key", "channel", "dedupe_key").
		Where(query.And(query.Equal("id", reservation.ID), query.Equal("row_kind", deliveryReservationRowKind))).Build()
	if err != nil {
		return false, err
	}
	var recipientKey, templateKey, channel, dedupeKey string
	err = queryer.QueryRowContext(ctx, lookup, args...).Scan(&recipientKey, &templateKey, &channel, &dedupeKey)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read notification delivery reservation: %w", err)
	}
	if recipientKey != reservation.RecipientKey.String() || templateKey != reservation.TemplateKey || channel != reservation.Channel || dedupeKey != reservation.DedupeKey {
		return false, fmt.Errorf("notification delivery reservation identity conflict")
	}
	return true, nil
}

func defaultPolicy() delivery.Policy {
	return delivery.Policy{Revision: metadatasdk.DefinitionNoCurrentVersion, Enabled: true, QuietStart: "22:00", QuietEnd: "08:00", Timezone: "Asia/Shanghai", MaxPerRecipientPerHour: 20,
		DedupeWindowSeconds: 300, FallbackChannels: []string{"collaboration", "email"}}
}
