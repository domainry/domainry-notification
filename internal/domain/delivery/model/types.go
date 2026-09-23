package model

import notification "github.com/domainry/domainry-notification/internal/domain/notification/model"

// Policy is the system default for notification delivery behavior. Recipient
// preferences are workspace-scoped overrides; a future workspace policy would
// require an explicit workspace identity and schema migration.
type Policy struct {
	Revision               string   `json:"revision,omitempty"`
	Enabled                bool     `json:"enabled"`
	QuietHoursEnabled      bool     `json:"quiet_hours_enabled"`
	QuietStart             string   `json:"quiet_start"`
	QuietEnd               string   `json:"quiet_end"`
	Timezone               string   `json:"timezone"`
	MaxPerRecipientPerHour int      `json:"max_per_recipient_per_hour"`
	DedupeWindowSeconds    int      `json:"dedupe_window_seconds"`
	FallbackChannels       []string `json:"fallback_channels,omitempty"`
	UpdatedBy              string   `json:"updated_by,omitempty"`
	UpdatedAt              string   `json:"updated_at,omitempty"`
}

type RecipientPreference struct {
	RecipientKey      string          `json:"recipient_key"`
	EnabledChannels   map[string]bool `json:"enabled_channels"`
	MutedTemplateKeys []string        `json:"muted_template_keys,omitempty"`
	UpdatedBy         string          `json:"updated_by,omitempty"`
	UpdatedAt         string          `json:"updated_at,omitempty"`
}

type Reservation struct {
	ID           string
	RecipientKey notification.UserID
	TemplateKey  string
	Channel      string
	DedupeKey    string
	CreatedAt    string
}

type Evaluation struct {
	WorkspaceID    notification.WorkspaceID
	TemplateKey    string
	Channel        string
	Recipients     []notification.UserID
	DedupeKey      string
	ReservationKey string
}

type Decision struct {
	DeliverAfter  string   `json:"deliver_after,omitempty"`
	FallbackOrder []string `json:"fallback_order,omitempty"`
}

// Plan is an independently retryable external projection of one notification
// event. In-app materialization has its own lifecycle and is never rolled back
// because external dispatch fails.
type Plan struct {
	ID                       string                   `json:"id"`
	WorkspaceID              notification.WorkspaceID `json:"workspace_id"`
	EventID                  string                   `json:"event_id"`
	Channel                  string                   `json:"channel"`
	TemplateKey              string                   `json:"template_key"`
	ConnectorKey             string                   `json:"connector_key"`
	ConnectionKey            string                   `json:"connection_key,omitempty"`
	Operation                string                   `json:"operation"`
	RecipientUserIDs         []notification.UserID    `json:"recipient_user_ids"`
	Locale                   string                   `json:"locale,omitempty"`
	Variables                map[string]any           `json:"variables,omitempty"`
	DedupeKey                string                   `json:"dedupe_key,omitempty"`
	Mandatory                bool                     `json:"mandatory,omitempty"`
	DeliveryMode             string                   `json:"delivery_mode,omitempty"`
	DigestKey                string                   `json:"digest_key,omitempty"`
	DigestMaximumItems       int                      `json:"digest_maximum_items,omitempty"`
	DigestItemTitle          string                   `json:"digest_item_title,omitempty"`
	DigestItemBody           string                   `json:"digest_item_body,omitempty"`
	EscalationStep           int                      `json:"escalation_step,omitempty"`
	CancelWhenActionTerminal bool                     `json:"cancel_when_action_terminal,omitempty"`
	Status                   string                   `json:"status"`
	AttemptCount             int                      `json:"attempt_count"`
	NextAttemptAt            string                   `json:"next_attempt_at,omitempty"`
	LastErrorCode            string                   `json:"last_error_code,omitempty"`
	OutboxMessageID          string                   `json:"outbox_message_id,omitempty"`
	LeaseOwner               string                   `json:"lease_owner,omitempty"`
	LeaseExpiresAt           string                   `json:"lease_expires_at,omitempty"`
	FencingToken             int64                    `json:"fencing_token"`
	CreatedAt                string                   `json:"created_at"`
	UpdatedAt                string                   `json:"updated_at"`
}
