package inbox

import (
	"github.com/domainry/domainry-notification"
	"github.com/domainry/domainry-notification/delivery"
	"github.com/domainry/domainry-notification/template"
)

// Intent is the only producer-facing message. Producers provide typed facts;
// copy and routes are compiled inside this module. Provider payloads remain a
// Connector responsibility downstream of delivery.Dispatcher.
type Intent struct {
	ID                   string                   `json:"id"`
	WorkspaceID          notification.WorkspaceID `json:"workspace_id"`
	SourceEventID        string                   `json:"source_event_id"`
	EventType            string                   `json:"event_type"`
	Severity             string                   `json:"severity,omitempty"`
	Surface              notification.Surface     `json:"surface"`
	RecipientUserIDs     []notification.UserID    `json:"recipient_user_ids"`
	AudienceResolverKeys []string                 `json:"audience_resolver_keys,omitempty"`
	SubjectType          string                   `json:"subject_type,omitempty"`
	SubjectID            string                   `json:"subject_id,omitempty"`
	SubjectVersion       string                   `json:"subject_version,omitempty"`
	GroupKey             string                   `json:"group_key,omitempty"`
	DedupeKey            string                   `json:"dedupe_key,omitempty"`
	ActionState          ActionState              `json:"action_state,omitempty"`
	AlertState           AlertState               `json:"alert_state,omitempty"`
	ExpiresAt            string                   `json:"expires_at,omitempty"`
	OccurredAt           string                   `json:"occurred_at"`
	Locale               string                   `json:"locale,omitempty"`
	Variables            map[string]any           `json:"variables,omitempty"`
	CorrelationID        string                   `json:"correlation_id,omitempty"`
	TraceID              string                   `json:"trace_id,omitempty"`
}

// Snapshot is immutable, sanitized in-app content. Template evidence prevents
// later edits from rewriting historical messages.
type Snapshot struct {
	Title               string          `json:"title"`
	Body                string          `json:"body"`
	Facts               []template.Fact `json:"facts,omitempty"`
	Actions             []ActionRef     `json:"actions,omitempty"`
	TemplateKey         string          `json:"template_key,omitempty"`
	TemplateVersion     int             `json:"template_version,omitempty"`
	TemplateLocale      string          `json:"template_locale,omitempty"`
	TemplateContentHash string          `json:"template_content_hash,omitempty"`
}

// Event is the durable, retry-safe result of compiling an Intent.
type Event struct {
	ID                   string                   `json:"id"`
	WorkspaceID          notification.WorkspaceID `json:"workspace_id"`
	Source               string                   `json:"source"`
	SourceEventID        string                   `json:"source_event_id"`
	EventType            string                   `json:"event_type"`
	Category             string                   `json:"category"`
	Severity             string                   `json:"severity"`
	Surface              notification.Surface     `json:"surface"`
	RecipientUserIDs     []notification.UserID    `json:"recipient_user_ids"`
	AudienceResolverKeys []string                 `json:"audience_resolver_keys,omitempty"`
	SubjectType          string                   `json:"subject_type,omitempty"`
	SubjectID            string                   `json:"subject_id,omitempty"`
	SubjectVersion       string                   `json:"subject_version,omitempty"`
	GroupKey             string                   `json:"group_key,omitempty"`
	DedupeKey            string                   `json:"dedupe_key,omitempty"`
	ActionState          ActionState              `json:"action_state"`
	AlertState           AlertState               `json:"alert_state,omitempty"`
	ExpiresAt            string                   `json:"expires_at,omitempty"`
	OccurredAt           string                   `json:"occurred_at"`
	CorrelationID        string                   `json:"correlation_id,omitempty"`
	TraceID              string                   `json:"trace_id,omitempty"`
	Snapshot             Snapshot                 `json:"snapshot"`
	LocalizedSnapshots   map[string]Snapshot      `json:"localized_snapshots,omitempty"`
	ChannelPlans         []delivery.Plan          `json:"channel_plans,omitempty"`
	Status               EventStatus              `json:"status"`
	AttemptCount         int                      `json:"attempt_count"`
	NextAttemptAt        string                   `json:"next_attempt_at,omitempty"`
	LastErrorCode        string                   `json:"last_error_code,omitempty"`
	LeaseOwner           string                   `json:"lease_owner,omitempty"`
	LeaseExpiresAt       string                   `json:"lease_expires_at,omitempty"`
	FencingToken         int64                    `json:"fencing_token"`
	CreatedAt            string                   `json:"created_at"`
	UpdatedAt            string                   `json:"updated_at"`
}

// EventFailure is append-only, non-sensitive processing evidence.
type EventFailure struct {
	ID            string                   `json:"id"`
	WorkspaceID   notification.WorkspaceID `json:"workspace_id"`
	EventID       string                   `json:"event_id"`
	EventType     string                   `json:"event_type"`
	Source        string                   `json:"source"`
	SourceEventID string                   `json:"source_event_id"`
	Stage         string                   `json:"stage"`
	ErrorCode     string                   `json:"error_code"`
	Attempt       int                      `json:"attempt"`
	Disposition   string                   `json:"disposition"`
	Retryable     bool                     `json:"retryable"`
	NextAttemptAt string                   `json:"next_attempt_at,omitempty"`
	FencingToken  int64                    `json:"fencing_token"`
	OccurredAt    string                   `json:"occurred_at"`
}
