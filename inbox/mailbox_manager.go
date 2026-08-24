package inbox

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/domainry/domainry-notification"
)

// MailboxManager owns recipient mailbox use cases. Authentication, reporting
// relationships, and the decision to grant team or delegated scope remain host
// responsibilities; Query contains the already-authorized recipient set.
type MailboxManager struct {
	validator   *Validator
	mailboxes   MailboxStore
	savedViews  SavedViewStore
	delegations DelegationStore
	metrics     MetricsStore
	clock       notification.Clock
}

type MailboxManagerDependencies struct {
	Validator   *Validator
	Mailboxes   MailboxStore
	SavedViews  SavedViewStore
	Delegations DelegationStore
	Metrics     MetricsStore
	Clock       notification.Clock
}

func NewMailboxManager(dependencies MailboxManagerDependencies) (*MailboxManager, error) {
	if dependencies.Validator == nil || dependencies.Mailboxes == nil || dependencies.Clock == nil {
		return nil, unavailable("backend.notification.inbox_unavailable", nil)
	}
	return &MailboxManager{
		validator: dependencies.Validator, mailboxes: dependencies.Mailboxes,
		savedViews: dependencies.SavedViews, delegations: dependencies.Delegations,
		metrics: dependencies.Metrics, clock: dependencies.Clock,
	}, nil
}

func (m *MailboxManager) List(ctx context.Context, query Query, cursor string) (Page, error) {
	validated, err := m.validator.ValidateQuery(query)
	if err != nil {
		return Page{}, err
	}
	if strings.TrimSpace(cursor) != "" {
		validated.BeforeUpdatedAt, validated.BeforeID, err = decodeCursor(cursor)
		if err != nil {
			return Page{}, err
		}
	}
	items, hasMore, err := m.mailboxes.ListItems(ctx, validated)
	if err != nil {
		return Page{}, err
	}
	page := Page{Items: items, HasMore: hasMore}
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		page.NextCursor = encodeCursor(last.UpdatedAt, last.ID)
	}
	return page, nil
}

func (m *MailboxManager) Get(ctx context.Context, query Query, id string) (Item, error) {
	validated, err := m.validator.ValidateQuery(query)
	if err != nil {
		return Item{}, err
	}
	value, found, err := m.mailboxes.GetItem(ctx, validated, strings.TrimSpace(id))
	return mailboxMutationResult(value, found, err, id)
}

func (m *MailboxManager) Facets(ctx context.Context, query Query) (Facets, error) {
	validated, err := m.validator.ValidateQuery(query)
	if err != nil {
		return Facets{}, err
	}
	return m.mailboxes.CountFacets(ctx, validated)
}

func (m *MailboxManager) SetRead(ctx context.Context, query Query, id string, read bool) (Item, error) {
	validated, err := m.validator.ValidateQuery(query)
	if err != nil {
		return Item{}, err
	}
	now := notification.Timestamp(m.clock.Now().UTC())
	readAt := ""
	if read {
		readAt = now
	}
	value, found, err := m.mailboxes.SetRead(ctx, validated, strings.TrimSpace(id), readAt, now)
	return mailboxMutationResult(value, found, err, id)
}

func (m *MailboxManager) SetArchived(ctx context.Context, query Query, id string, archived bool) (Item, error) {
	validated, err := m.validator.ValidateQuery(query)
	if err != nil {
		return Item{}, err
	}
	now := notification.Timestamp(m.clock.Now().UTC())
	archivedAt := ""
	if archived {
		archivedAt = now
	}
	value, found, err := m.mailboxes.SetArchived(ctx, validated, strings.TrimSpace(id), archivedAt, now)
	return mailboxMutationResult(value, found, err, id)
}

func (m *MailboxManager) AcknowledgeAlert(ctx context.Context, query Query, id string, actor notification.UserID) (Item, error) {
	validated, err := m.validator.ValidateQuery(query)
	if err != nil {
		return Item{}, err
	}
	id, actorValue := strings.TrimSpace(id), strings.TrimSpace(actor.String())
	if actorValue == "" || validated.Scope != ScopeMine || notification.UserID(actorValue) != validated.ViewerUserID {
		return Item{}, invalid("backend.notification.inbox_alert_actor_invalid")
	}
	current, found, err := m.mailboxes.GetItem(ctx, validated, id)
	if err != nil || !found {
		return mailboxMutationResult(Item{}, found, err, id)
	}
	if current.AlertState == "" || current.AlertState == AlertResolved {
		return Item{}, conflict("backend.notification.inbox_alert_not_firing", "alert_state", string(current.AlertState))
	}
	value, found, err := m.mailboxes.AcknowledgeAlert(ctx, validated, id, notification.UserID(actorValue), notification.Timestamp(m.clock.Now().UTC()))
	return mailboxMutationResult(value, found, err, id)
}

func (m *MailboxManager) MarkAllRead(ctx context.Context, query Query) (int, error) {
	validated, err := m.validator.ValidateQuery(query)
	if err != nil {
		return 0, err
	}
	if validated.Scope != ScopeMine {
		return 0, forbidden("backend.notification.inbox_team_mutation_forbidden")
	}
	return m.mailboxes.MarkAllRead(ctx, validated, notification.Timestamp(m.clock.Now().UTC()))
}

func (m *MailboxManager) ListSavedViews(ctx context.Context, workspaceID notification.WorkspaceID, userID notification.UserID, surface notification.Surface) ([]SavedView, error) {
	workspaceID, userID, surface, err := m.validateMailboxOwner(workspaceID, userID, surface)
	if err != nil {
		return nil, err
	}
	if m.savedViews == nil {
		return nil, unavailable("backend.notification.inbox_saved_views_unavailable", nil)
	}
	return m.savedViews.ListSavedViews(ctx, workspaceID, userID, surface)
}

func (m *MailboxManager) SaveSavedView(ctx context.Context, workspaceID notification.WorkspaceID, userID notification.UserID, surface notification.Surface, value SavedView) (SavedView, error) {
	workspaceID, userID, surface, err := m.validateMailboxOwner(workspaceID, userID, surface)
	if err != nil {
		return SavedView{}, err
	}
	if m.savedViews == nil {
		return SavedView{}, unavailable("backend.notification.inbox_saved_views_unavailable", nil)
	}
	validated, err := m.validator.ValidateSavedView(value)
	if err != nil {
		return SavedView{}, err
	}
	now := notification.Timestamp(m.clock.Now().UTC())
	validated.UpdatedAt = now
	if validated.CreatedAt == "" {
		validated.CreatedAt = now
	}
	return m.savedViews.SaveSavedView(ctx, workspaceID, userID, surface, validated)
}

func (m *MailboxManager) DeleteSavedView(ctx context.Context, workspaceID notification.WorkspaceID, userID notification.UserID, surface notification.Surface, key string) error {
	workspaceID, userID, surface, err := m.validateMailboxOwner(workspaceID, userID, surface)
	if err != nil {
		return err
	}
	if m.savedViews == nil {
		return unavailable("backend.notification.inbox_saved_views_unavailable", nil)
	}
	deleted, err := m.savedViews.DeleteSavedView(ctx, workspaceID, userID, surface, strings.TrimSpace(key))
	if err != nil {
		return err
	}
	if !deleted {
		return notFound("backend.notification.inbox_saved_view_not_found", "view_key", key)
	}
	return nil
}

func (m *MailboxManager) ListDelegations(ctx context.Context, workspaceID notification.WorkspaceID, ownerID notification.UserID, surface notification.Surface) ([]Delegation, error) {
	workspaceID, ownerID, surface, err := m.validateMailboxOwner(workspaceID, ownerID, surface)
	if err != nil {
		return nil, err
	}
	if m.delegations == nil {
		return nil, unavailable("backend.notification.inbox_delegations_unavailable", nil)
	}
	return m.delegations.ListDelegations(ctx, workspaceID, ownerID, surface)
}

func (m *MailboxManager) SaveDelegation(ctx context.Context, value Delegation) (Delegation, error) {
	if m.delegations == nil {
		return value, unavailable("backend.notification.inbox_delegations_unavailable", nil)
	}
	workspaceID, ownerID, surface, err := m.validateMailboxOwner(value.WorkspaceID, value.OwnerUserID, value.Surface)
	if err != nil {
		return value, err
	}
	delegateID := notification.UserID(strings.TrimSpace(value.DelegateUserID.String()))
	if delegateID == "" || delegateID == ownerID {
		return value, invalid("backend.notification.inbox_delegation_invalid")
	}
	value.WorkspaceID, value.OwnerUserID, value.DelegateUserID, value.Surface = workspaceID, ownerID, delegateID, surface
	var starts, ends time.Time
	if value.StartsAt, starts, err = normalizeDelegationTime("starts_at", value.StartsAt); err != nil {
		return value, err
	}
	if value.EndsAt, ends, err = normalizeDelegationTime("ends_at", value.EndsAt); err != nil {
		return value, err
	}
	if !starts.IsZero() && !ends.IsZero() && !ends.After(starts) {
		return value, invalid("backend.notification.inbox_delegation_time_invalid", "field", "ends_at")
	}
	if strings.TrimSpace(value.ID) == "" {
		value.ID = stableID(workspaceID.String(), ownerID.String(), delegateID.String(), string(surface))
	}
	now := notification.Timestamp(m.clock.Now().UTC())
	if value.CreatedAt == "" {
		value.CreatedAt = now
	}
	value.UpdatedAt = now
	return m.delegations.SaveDelegation(ctx, value)
}

func (m *MailboxManager) DeleteDelegation(ctx context.Context, workspaceID notification.WorkspaceID, ownerID notification.UserID, id string) error {
	workspaceID = notification.WorkspaceID(strings.TrimSpace(workspaceID.String()))
	ownerID = notification.UserID(strings.TrimSpace(ownerID.String()))
	if workspaceID == "" || ownerID == "" {
		return invalid("backend.notification.inbox_delegation_invalid")
	}
	if m.delegations == nil {
		return unavailable("backend.notification.inbox_delegations_unavailable", nil)
	}
	deleted, err := m.delegations.DeleteDelegation(ctx, workspaceID, ownerID, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if !deleted {
		return notFound("backend.notification.inbox_delegation_not_found", "delegation_id", id)
	}
	return nil
}

func (m *MailboxManager) ActiveDelegatedOwnerIDs(ctx context.Context, workspaceID notification.WorkspaceID, delegateID notification.UserID, surface notification.Surface) ([]notification.UserID, error) {
	workspaceID, delegateID, surface, err := m.validateMailboxOwner(workspaceID, delegateID, surface)
	if err != nil {
		return nil, err
	}
	if m.delegations == nil {
		return nil, unavailable("backend.notification.inbox_delegations_unavailable", nil)
	}
	return m.delegations.ListActiveDelegatedOwnerIDs(ctx, workspaceID, delegateID, surface, notification.Timestamp(m.clock.Now().UTC()))
}

func (m *MailboxManager) GovernanceMetrics(ctx context.Context, workspaceID notification.WorkspaceID, since string) (GovernanceMetrics, error) {
	workspaceID = notification.WorkspaceID(strings.TrimSpace(workspaceID.String()))
	if workspaceID == "" || m.metrics == nil {
		return GovernanceMetrics{}, unavailable("backend.notification.inbox_metrics_unavailable", nil)
	}
	return m.metrics.GovernanceMetrics(ctx, workspaceID, strings.TrimSpace(since))
}

func (m *MailboxManager) validateMailboxOwner(workspaceID notification.WorkspaceID, userID notification.UserID, surface notification.Surface) (notification.WorkspaceID, notification.UserID, notification.Surface, error) {
	workspaceID = notification.WorkspaceID(strings.TrimSpace(workspaceID.String()))
	userID = notification.UserID(strings.TrimSpace(userID.String()))
	surface = notification.Surface(strings.TrimSpace(string(surface)))
	if workspaceID == "" || userID == "" || !m.validator.configuration.SupportsSurface(surface) {
		return "", "", "", invalid("backend.notification.inbox_scope_invalid")
	}
	return workspaceID, userID, surface, nil
}

func mailboxMutationResult(value Item, found bool, err error, id string) (Item, error) {
	if err != nil {
		return Item{}, err
	}
	if !found {
		return Item{}, notFound("backend.notification.inbox_item_not_found", "notification_id", id)
	}
	return value, nil
}

func encodeCursor(updatedAt, id string) string {
	raw, _ := json.Marshal([]string{updatedAt, id})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(value string) (string, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return "", "", invalid("backend.notification.inbox_cursor_invalid")
	}
	var parts []string
	if json.Unmarshal(raw, &parts) != nil || len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", invalid("backend.notification.inbox_cursor_invalid")
	}
	return parts[0], parts[1], nil
}

func normalizeDelegationTime(field, value string) (string, time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return "", time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value, time.Time{}, invalid("backend.notification.inbox_delegation_time_invalid", "field", field)
	}
	return notification.Timestamp(parsed), parsed, nil
}
