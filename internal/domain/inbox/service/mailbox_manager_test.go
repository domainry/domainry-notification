package inbox_test

import (
	"context"
	"testing"
	"time"

	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

type mailboxStore struct {
	item      inbox.Item
	query     inbox.Query
	readAt    string
	updatedAt string
}

func (s *mailboxStore) ListItems(_ context.Context, query inbox.Query) ([]inbox.Item, bool, error) {
	s.query = query
	return []inbox.Item{s.item}, true, nil
}
func (s *mailboxStore) GetItem(_ context.Context, query inbox.Query, _ string) (inbox.Item, bool, error) {
	s.query = query
	return s.item, s.item.ID != "", nil
}
func (s *mailboxStore) CountFacets(context.Context, inbox.Query) (inbox.Facets, error) {
	return inbox.Facets{}, nil
}
func (s *mailboxStore) SetRead(_ context.Context, query inbox.Query, _ string, readAt, updatedAt string) (inbox.Item, bool, error) {
	s.query, s.readAt, s.updatedAt = query, readAt, updatedAt
	return s.item, true, nil
}
func (s *mailboxStore) SetArchived(context.Context, inbox.Query, string, string, string) (inbox.Item, bool, error) {
	return s.item, true, nil
}
func (s *mailboxStore) AcknowledgeAlert(context.Context, inbox.Query, string, notification.UserID, string) (inbox.Item, bool, error) {
	return s.item, true, nil
}
func (s *mailboxStore) MarkAllRead(context.Context, inbox.Query, string) (int, error) { return 1, nil }

type delegationStore struct{ saved inbox.Delegation }

func (*delegationStore) ListDelegations(context.Context, notification.WorkspaceID, notification.UserID, notification.Surface) ([]inbox.Delegation, error) {
	return nil, nil
}
func (s *delegationStore) SaveDelegation(_ context.Context, value inbox.Delegation) (inbox.Delegation, error) {
	s.saved = value
	return value, nil
}
func (*delegationStore) DeleteDelegation(context.Context, notification.WorkspaceID, notification.UserID, string) (bool, error) {
	return true, nil
}
func (*delegationStore) ListActiveDelegatedOwnerIDs(context.Context, notification.WorkspaceID, notification.UserID, notification.Surface, string) ([]notification.UserID, error) {
	return nil, nil
}

func newMailboxManager(t *testing.T, mailboxes inbox.MailboxStore, delegations inbox.DelegationStore) *inbox.MailboxManager {
	t.Helper()
	configuration, err := inbox.NewConfiguration([]notification.Surface{"custom_surface"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := inbox.NewValidator(configuration)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := inbox.NewMailboxManager(inbox.MailboxManagerDependencies{
		Validator: validator, Mailboxes: mailboxes, Delegations: delegations,
		Clock: fixedClock{value: time.Date(2026, 8, 24, 1, 2, 3, 0, time.FixedZone("CST", 8*60*60))},
	})
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func TestMailboxManagerScopesPersonalMutationAndUsesUTC(t *testing.T) {
	store := &mailboxStore{item: inbox.Item{ID: "item-1"}}
	manager := newMailboxManager(t, store, nil)
	query := inbox.Query{WorkspaceID: " workspace-1 ", ViewerUserID: " user-1 ", Surface: "custom_surface", Scope: inbox.ScopeMine}
	if _, err := manager.SetRead(t.Context(), query, "item-1", true); err != nil {
		t.Fatal(err)
	}
	if len(store.query.RecipientUserIDs) != 1 || store.query.RecipientUserIDs[0] != "user-1" {
		t.Fatalf("query escaped personal scope: %+v", store.query)
	}
	if store.readAt != "2026-08-23T17:02:03.000000000Z" || store.updatedAt != store.readAt {
		t.Fatalf("timestamps read=%q updated=%q", store.readAt, store.updatedAt)
	}

	query.Scope, query.ReportingUserIDs = inbox.ScopeTeam, []notification.UserID{"user-2"}
	if _, err := manager.MarkAllRead(t.Context(), query); notification.ErrorCode(err) != "backend.notification.inbox_team_mutation_forbidden" {
		t.Fatalf("error=%v", err)
	} else if notification.ErrorKindOf(err) != notification.ErrorForbidden {
		t.Fatalf("kind=%q", notification.ErrorKindOf(err))
	}
}

func TestMailboxManagerDelegationUsesConfiguredSurface(t *testing.T) {
	mailboxes, delegations := &mailboxStore{}, &delegationStore{}
	manager := newMailboxManager(t, mailboxes, delegations)
	value, err := manager.SaveDelegation(t.Context(), inbox.Delegation{
		WorkspaceID: "workspace-1", OwnerUserID: "owner-1", DelegateUserID: "delegate-1", Surface: "custom_surface",
		StartsAt: "2026-08-24T00:00:00+08:00", EndsAt: "2026-08-25T00:00:00+08:00", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if value.ID == "" || value.Surface != "custom_surface" || value.StartsAt != "2026-08-23T16:00:00.000000000Z" {
		t.Fatalf("delegation=%+v", value)
	}
	if _, err := manager.SaveDelegation(t.Context(), inbox.Delegation{WorkspaceID: "workspace-1", OwnerUserID: "owner-1", DelegateUserID: "owner-1", Surface: "custom_surface"}); notification.ErrorCode(err) != "backend.notification.inbox_delegation_invalid" {
		t.Fatalf("error=%v", err)
	}
}

type itemReader struct{ item inbox.Item }

func (r itemReader) Get(context.Context, inbox.Query, string) (inbox.Item, error) { return r.item, nil }

func TestActionResolverReturnsSemanticRouteAndRejectsTeamActions(t *testing.T) {
	validator := inboxValidator(t)
	eventType := validEventType()
	eventType.Actions = []inbox.ActionDescriptor{{Key: "workflow.task.open", Kind: "route", ResourceType: "project_record", SurfaceRoutes: map[string]string{"business_workspace": "workflow.task.detail"}}}
	content := eventType.Locales["en-US"]
	content.ActionLabels = map[string]string{"workflow.task.open": "Open"}
	eventType.Locales["en-US"] = content
	catalog, err := inbox.NewCatalog(validator, []inbox.EventType{eventType}, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := inbox.NewActionResolver(itemReader{item: inbox.Item{
		ID: "item-1", SubjectType: "ticket", ActionState: inbox.ActionOpen,
		Actions:   []inbox.ActionRef{{Key: "workflow.task.open", Kind: "route", Label: "Open", ResourceType: "project_record", ResourceID: "record-1"}},
		ExpiresAt: "2026-08-25T00:00:00Z",
	}}, catalog, fixedClock{value: time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	query := inbox.Query{Surface: "business_workspace", Scope: inbox.ScopeMine}
	resolved, err := resolver.Resolve(t.Context(), query, "item-1", "workflow.task.open")
	if err != nil || resolved.RouteKey != "workflow.task.detail" || resolved.RouteParams["object_key"] != "ticket" {
		t.Fatalf("resolved=%+v err=%v", resolved, err)
	}
	query.Scope = inbox.ScopeDelegated
	if _, err := resolver.Resolve(t.Context(), query, "item-1", "workflow.task.open"); notification.ErrorCode(err) != "backend.notification.inbox_team_action_forbidden" {
		t.Fatalf("error=%v", err)
	} else if notification.ErrorKindOf(err) != notification.ErrorForbidden {
		t.Fatalf("kind=%q", notification.ErrorKindOf(err))
	}
}

func TestSavedViewAllowsDelegatedScope(t *testing.T) {
	configuration, _ := inbox.NewConfiguration([]notification.Surface{"custom_surface"}, nil)
	validator, _ := inbox.NewValidator(configuration)
	value, err := validator.ValidateSavedView(inbox.SavedView{Key: "delegated.alerts", Name: "Delegated alerts", Scope: inbox.ScopeDelegated, TeamMemberID: "owner-1"})
	if err != nil || value.Scope != inbox.ScopeDelegated {
		t.Fatalf("view=%+v err=%v", value, err)
	}
}
