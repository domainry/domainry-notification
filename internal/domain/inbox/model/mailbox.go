package model

import (
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	template "github.com/domainry/domainry-notification/internal/domain/template/model"
)

type Item struct {
	ID                  string                   `json:"id"`
	WorkspaceID         notification.WorkspaceID `json:"workspace_id"`
	RecipientUserID     notification.UserID      `json:"recipient_user_id"`
	EventID             string                   `json:"event_id"`
	EventType           string                   `json:"event_type"`
	Source              string                   `json:"source"`
	Category            string                   `json:"category"`
	Severity            string                   `json:"severity"`
	Title               string                   `json:"title"`
	Body                string                   `json:"body"`
	Facts               []template.Fact          `json:"facts,omitempty"`
	Actions             []ActionRef              `json:"actions,omitempty"`
	TemplateKey         string                   `json:"template_key,omitempty"`
	TemplateVersion     int                      `json:"template_version,omitempty"`
	TemplateLocale      string                   `json:"template_locale,omitempty"`
	TemplateContentHash string                   `json:"template_content_hash,omitempty"`
	SubjectType         string                   `json:"subject_type,omitempty"`
	SubjectID           string                   `json:"subject_id,omitempty"`
	SubjectVersion      string                   `json:"subject_version,omitempty"`
	ActionState         ActionState              `json:"action_state"`
	AlertState          AlertState               `json:"alert_state,omitempty"`
	GroupKey            string                   `json:"group_key,omitempty"`
	OccurrenceCount     int                      `json:"occurrence_count"`
	FirstOccurredAt     string                   `json:"first_occurred_at"`
	LastOccurredAt      string                   `json:"last_occurred_at"`
	ReadAt              string                   `json:"read_at,omitempty"`
	ArchivedAt          string                   `json:"archived_at,omitempty"`
	ExpiresAt           string                   `json:"expires_at,omitempty"`
	CreatedAt           string                   `json:"created_at"`
	UpdatedAt           string                   `json:"updated_at"`
}

type Query struct {
	WorkspaceID      notification.WorkspaceID
	ViewerUserID     notification.UserID
	RecipientUserID  notification.UserID
	RecipientUserIDs []notification.UserID
	ReportingUserIDs []notification.UserID
	DelegatedUserIDs []notification.UserID
	Scope            Scope
	Mailbox          Mailbox
	Query            string
	Categories       []string
	Sources          []string
	Severities       []string
	ActionStates     []ActionState
	From             string
	To               string
	BeforeUpdatedAt  string
	BeforeID         string
	Limit            int
}

type Page struct {
	Items      []Item `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

type Facet struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type Facets struct {
	Unread         int     `json:"unread"`
	ActionRequired int     `json:"action_required"`
	Categories     []Facet `json:"categories"`
	Sources        []Facet `json:"sources"`
	Severities     []Facet `json:"severities"`
}

// Delegation grants read-only mailbox access. It never grants authority to
// mutate the owner's mailbox or source business resource.
type Delegation struct {
	ID             string                   `json:"id"`
	WorkspaceID    notification.WorkspaceID `json:"workspace_id"`
	OwnerUserID    notification.UserID      `json:"owner_user_id"`
	DelegateUserID notification.UserID      `json:"delegate_user_id"`
	StartsAt       string                   `json:"starts_at,omitempty"`
	EndsAt         string                   `json:"ends_at,omitempty"`
	Enabled        bool                     `json:"enabled"`
	CreatedAt      string                   `json:"created_at"`
	UpdatedAt      string                   `json:"updated_at"`
}

type SavedView struct {
	Key          string              `json:"key"`
	Name         string              `json:"name"`
	Mailbox      Mailbox             `json:"mailbox"`
	Scope        Scope               `json:"scope"`
	TeamMemberID notification.UserID `json:"team_member_id,omitempty"`
	Query        string              `json:"query,omitempty"`
	Categories   []string            `json:"categories,omitempty"`
	Sources      []string            `json:"sources,omitempty"`
	Severities   []string            `json:"severities,omitempty"`
	ActionStates []ActionState       `json:"action_states,omitempty"`
	From         string              `json:"from,omitempty"`
	To           string              `json:"to,omitempty"`
	CreatedAt    string              `json:"created_at,omitempty"`
	UpdatedAt    string              `json:"updated_at,omitempty"`
}

// AlertGroup is the durable lifecycle of a continuing alert for one recipient.
// Inbox rows mirror this state for mailbox queries.
type AlertGroup struct {
	WorkspaceID     notification.WorkspaceID `json:"workspace_id"`
	RecipientUserID notification.UserID      `json:"recipient_user_id"`
	GroupKey        string                   `json:"group_key"`
	State           AlertState               `json:"state"`
	OccurrenceCount int                      `json:"occurrence_count"`
	FirstOccurredAt string                   `json:"first_occurred_at"`
	LastOccurredAt  string                   `json:"last_occurred_at"`
	AcknowledgedAt  string                   `json:"acknowledged_at,omitempty"`
	AcknowledgedBy  notification.UserID      `json:"acknowledged_by,omitempty"`
	ResolvedAt      string                   `json:"resolved_at,omitempty"`
	LastEventID     string                   `json:"last_event_id"`
	UpdatedAt       string                   `json:"updated_at"`
}
