package inbox

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/domainry/domainry-notification"
)

const (
	titleLimit     = 240
	bodyLimit      = 4000
	factLimit      = 20
	actionLimit    = 5
	recipientLimit = 500
)

var stableKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`)
var severities = map[string]bool{"info": true, "warning": true, "critical": true}
var actionStates = map[ActionState]bool{ActionNone: true, ActionOpen: true, ActionCompleted: true, ActionExpired: true, ActionCancelled: true}
var alertStates = map[AlertState]bool{"": true, AlertFiring: true, AlertResolved: true}

type Validator struct {
	configuration *Configuration
}

func NewValidator(configuration *Configuration) (*Validator, error) {
	if configuration == nil {
		return nil, fmt.Errorf("notification inbox configuration is required")
	}
	return &Validator{configuration: configuration}, nil
}

func (v *Validator) ValidateEvent(value Event) (Event, error) {
	value.ID = strings.TrimSpace(value.ID)
	value.WorkspaceID = notification.WorkspaceID(strings.TrimSpace(value.WorkspaceID.String()))
	value.Source, value.SourceEventID = strings.TrimSpace(value.Source), strings.TrimSpace(value.SourceEventID)
	value.EventType, value.Category = strings.TrimSpace(value.EventType), strings.TrimSpace(value.Category)
	value.Severity = strings.TrimSpace(value.Severity)
	value.Surface = notification.Surface(strings.TrimSpace(string(value.Surface)))
	value.SubjectType, value.SubjectID, value.SubjectVersion = strings.TrimSpace(value.SubjectType), strings.TrimSpace(value.SubjectID), strings.TrimSpace(value.SubjectVersion)
	value.GroupKey, value.DedupeKey = strings.TrimSpace(value.GroupKey), strings.TrimSpace(value.DedupeKey)
	value.ActionState = ActionState(strings.TrimSpace(string(value.ActionState)))
	value.AlertState = AlertState(strings.TrimSpace(string(value.AlertState)))
	value.ExpiresAt, value.OccurredAt = strings.TrimSpace(value.ExpiresAt), strings.TrimSpace(value.OccurredAt)
	value.Snapshot.Title, value.Snapshot.Body = strings.TrimSpace(value.Snapshot.Title), strings.TrimSpace(value.Snapshot.Body)
	value.AudienceResolverKeys = uniqueStrings(value.AudienceResolverKeys)
	if len(value.AudienceResolverKeys) > 8 {
		return value, invalid("backend.notification.inbox_audience_resolvers_invalid")
	}
	for _, resolverKey := range value.AudienceResolverKeys {
		if !stableKeyPattern.MatchString(resolverKey) {
			return value, invalid("backend.notification.inbox_audience_resolvers_invalid")
		}
	}
	if value.ID == "" || value.WorkspaceID == "" || value.SourceEventID == "" {
		return value, invalid("backend.notification.inbox_event_identity_required")
	}
	for field, text := range map[string]string{"source": value.Source, "event_type": value.EventType, "category": value.Category} {
		if !stableKeyPattern.MatchString(text) {
			return value, invalid("backend.notification.inbox_event_key_invalid", "field", field, "value", text)
		}
	}
	if !severities[value.Severity] {
		return value, invalid("backend.notification.inbox_event_severity_invalid", "severity", value.Severity)
	}
	if !v.configuration.SupportsSurface(value.Surface) {
		return value, invalid("backend.notification.inbox_event_surface_invalid", "surface", string(value.Surface))
	}
	if value.ActionState == "" {
		value.ActionState = ActionNone
	}
	if !actionStates[value.ActionState] {
		return value, invalid("backend.notification.inbox_action_state_invalid", "action_state", string(value.ActionState))
	}
	if !alertStates[value.AlertState] || (value.AlertState != "" && value.GroupKey == "") {
		return value, invalid("backend.notification.inbox_alert_state_invalid", "alert_state", string(value.AlertState))
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, value.OccurredAt)
	if err != nil {
		return value, invalid("backend.notification.inbox_event_time_invalid", "field", "occurred_at")
	}
	value.OccurredAt = notification.Timestamp(occurredAt)
	if value.ExpiresAt != "" {
		expiresAt, parseErr := time.Parse(time.RFC3339Nano, value.ExpiresAt)
		if parseErr != nil {
			return value, invalid("backend.notification.inbox_event_time_invalid", "field", "expires_at")
		}
		value.ExpiresAt = notification.Timestamp(expiresAt)
	}
	if value.Snapshot.Title == "" || utf8.RuneCountInString(value.Snapshot.Title) > titleLimit {
		return value, invalid("backend.notification.inbox_title_invalid")
	}
	if looksLikeLink(value.Snapshot.Title) {
		return value, invalid("backend.notification.inbox_title_link_forbidden")
	}
	if value.Snapshot.Body == "" || utf8.RuneCountInString(value.Snapshot.Body) > bodyLimit {
		return value, invalid("backend.notification.inbox_body_invalid")
	}
	if len(value.Snapshot.Facts) > factLimit || len(value.Snapshot.Actions) > actionLimit {
		return value, invalid("backend.notification.inbox_components_limit")
	}
	for _, fact := range value.Snapshot.Facts {
		if strings.TrimSpace(fact.Key) == "" || strings.TrimSpace(fact.Value) == "" {
			return value, invalid("backend.notification.inbox_fact_invalid")
		}
	}
	for index, action := range value.Snapshot.Actions {
		validated, actionErr := validateAction(action)
		if actionErr != nil {
			return value, actionErr
		}
		value.Snapshot.Actions[index] = validated
	}
	for locale, snapshot := range value.LocalizedSnapshots {
		if strings.TrimSpace(locale) == "" {
			return value, invalid("backend.notification.inbox_snapshot_locale_invalid")
		}
		candidate := value
		candidate.Snapshot, candidate.LocalizedSnapshots = snapshot, nil
		validated, snapshotErr := v.ValidateEvent(candidate)
		if snapshotErr != nil {
			return value, snapshotErr
		}
		value.LocalizedSnapshots[locale] = validated.Snapshot
	}
	value.RecipientUserIDs = uniqueUsers(value.RecipientUserIDs)
	if (len(value.RecipientUserIDs) == 0 && len(value.AudienceResolverKeys) == 0) || len(value.RecipientUserIDs) > recipientLimit {
		return value, invalid("backend.notification.inbox_recipients_invalid")
	}
	value.Status = EventQueued
	value.AttemptCount, value.NextAttemptAt, value.LastErrorCode = 0, "", ""
	value.LeaseOwner, value.LeaseExpiresAt, value.FencingToken = "", "", 0
	return value, nil
}

func (v *Validator) ValidateQuery(value Query) (Query, error) {
	value.WorkspaceID = notification.WorkspaceID(strings.TrimSpace(value.WorkspaceID.String()))
	value.ViewerUserID = notification.UserID(strings.TrimSpace(value.ViewerUserID.String()))
	value.RecipientUserID = notification.UserID(strings.TrimSpace(value.RecipientUserID.String()))
	value.Surface = notification.Surface(strings.TrimSpace(string(value.Surface)))
	value.Scope = Scope(strings.TrimSpace(string(value.Scope)))
	value.Mailbox = Mailbox(strings.TrimSpace(string(value.Mailbox)))
	value.Query = strings.TrimSpace(value.Query)
	if value.WorkspaceID == "" || value.ViewerUserID == "" || !v.configuration.SupportsSurface(value.Surface) {
		return value, invalid("backend.notification.inbox_scope_invalid")
	}
	if value.Scope == "" {
		value.Scope = ScopeMine
	}
	if value.Scope != ScopeMine && value.Scope != ScopeTeam && value.Scope != ScopeDelegated {
		return value, invalid("backend.notification.inbox_audience_scope_invalid")
	}
	value.ReportingUserIDs, value.DelegatedUserIDs = uniqueUsers(value.ReportingUserIDs), uniqueUsers(value.DelegatedUserIDs)
	switch value.Scope {
	case ScopeTeam:
		if len(value.ReportingUserIDs) == 0 || (value.RecipientUserID != "" && !containsUser(value.ReportingUserIDs, value.RecipientUserID)) {
			return value, invalid("backend.notification.inbox_team_scope_denied")
		}
		value.RecipientUserIDs = append([]notification.UserID(nil), value.ReportingUserIDs...)
	case ScopeDelegated:
		if len(value.DelegatedUserIDs) == 0 || (value.RecipientUserID != "" && !containsUser(value.DelegatedUserIDs, value.RecipientUserID)) {
			return value, invalid("backend.notification.inbox_delegated_scope_denied")
		}
		value.RecipientUserIDs = append([]notification.UserID(nil), value.DelegatedUserIDs...)
	default:
		if value.RecipientUserID != "" && value.RecipientUserID != value.ViewerUserID {
			return value, invalid("backend.notification.inbox_team_filter_invalid")
		}
		value.RecipientUserID = value.ViewerUserID
		value.RecipientUserIDs = []notification.UserID{value.ViewerUserID}
	}
	if (value.Scope == ScopeTeam || value.Scope == ScopeDelegated) && value.RecipientUserID != "" {
		value.RecipientUserIDs = []notification.UserID{value.RecipientUserID}
	}
	if value.Mailbox == "" {
		value.Mailbox = MailboxInbox
	}
	switch value.Mailbox {
	case MailboxInbox, MailboxUnread, MailboxActionRequired, MailboxArchived:
	default:
		return value, invalid("backend.notification.inbox_mailbox_invalid", "mailbox", string(value.Mailbox))
	}
	if value.Limit <= 0 {
		value.Limit = 50
	}
	if value.Limit > 100 {
		value.Limit = 100
	}
	if len(value.Query) > 200 {
		return value, invalid("backend.notification.inbox_query_invalid")
	}
	var err error
	if value.From, err = normalizeQueryTime("from", value.From); err != nil {
		return value, err
	}
	if value.To, err = normalizeQueryTime("to", value.To); err != nil {
		return value, err
	}
	return value, nil
}

func (v *Validator) ValidateSavedView(value SavedView) (SavedView, error) {
	value.Key, value.Name = strings.TrimSpace(value.Key), strings.TrimSpace(value.Name)
	value.TeamMemberID = notification.UserID(strings.TrimSpace(value.TeamMemberID.String()))
	if !stableKeyPattern.MatchString(value.Key) || value.Name == "" || utf8.RuneCountInString(value.Name) > 80 {
		return value, invalid("backend.notification.inbox_saved_view_invalid")
	}
	reportingID := value.TeamMemberID
	if reportingID == "" {
		reportingID = "saved-view-report"
	}
	reportingIDs, delegatedIDs := []notification.UserID{reportingID}, []notification.UserID(nil)
	if value.Scope == ScopeDelegated {
		reportingIDs, delegatedIDs = nil, []notification.UserID{reportingID}
	}
	query, err := v.ValidateQuery(Query{
		WorkspaceID: "saved-view", ViewerUserID: "saved-view", Surface: firstSurface(v.configuration), Scope: value.Scope,
		RecipientUserID: value.TeamMemberID, ReportingUserIDs: reportingIDs, DelegatedUserIDs: delegatedIDs, Mailbox: value.Mailbox,
		Query: value.Query, Categories: value.Categories, Sources: value.Sources, Severities: value.Severities,
		ActionStates: value.ActionStates, From: value.From, To: value.To, Limit: 1,
	})
	if err != nil {
		return value, fmt.Errorf("saved view: %w", err)
	}
	value.Mailbox, value.Query, value.Scope = query.Mailbox, query.Query, query.Scope
	return value, nil
}

func validateAction(action ActionRef) (ActionRef, error) {
	key, kind, label := strings.TrimSpace(action.Key), strings.TrimSpace(action.Kind), strings.TrimSpace(action.Label)
	resourceType, resourceID := strings.TrimSpace(action.ResourceType), strings.TrimSpace(action.ResourceID)
	if !stableKeyPattern.MatchString(key) || label == "" || looksLikeLink(label) || (kind != "route" && kind != "business_action") {
		return action, invalid("backend.notification.inbox_action_invalid", "action_key", key)
	}
	if !stableKeyPattern.MatchString(resourceType) || resourceID == "" || looksLikeLink(resourceID) {
		return action, invalid("backend.notification.inbox_action_resource_invalid", "action_key", key)
	}
	style := strings.TrimSpace(action.Style)
	if style != "" && style != "primary" && style != "secondary" && style != "danger" {
		return action, invalid("backend.notification.inbox_action_style_invalid", "action_key", key)
	}
	action.Key, action.Kind, action.Label = key, kind, label
	action.ResourceType, action.ResourceID, action.Style = resourceType, resourceID, style
	return action, nil
}

func looksLikeLink(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(value, "://") || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "www.")
}

func uniqueStrings(values []string) []string {
	seen, result := map[string]bool{}, make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value], result = true, append(result, value)
		}
	}
	return result
}

func uniqueUsers(values []notification.UserID) []notification.UserID {
	seen, result := map[notification.UserID]bool{}, make([]notification.UserID, 0, len(values))
	for _, value := range values {
		value = notification.UserID(strings.TrimSpace(value.String()))
		if value != "" && !seen[value] {
			seen[value], result = true, append(result, value)
		}
	}
	return result
}

func containsUser(values []notification.UserID, target notification.UserID) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func normalizeQueryTime(field, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return "", invalid("backend.notification.inbox_query_time_invalid", "field", field)
	}
	return notification.Timestamp(parsed), nil
}

func firstSurface(configuration *Configuration) notification.Surface {
	for surface := range configuration.surfaces {
		return surface
	}
	return ""
}
