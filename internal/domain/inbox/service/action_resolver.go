package inbox

import (
	"context"
	"strings"
	"time"

	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

const NavigationSurfaceRoute = "surface_route"

type MailboxItemReader interface {
	Get(context.Context, Query, string) (Item, error)
}

// ActionResolver converts an inbox action into host-router input. It never
// authorizes or executes the referenced business resource; the host must do so.
type ActionResolver struct {
	mailbox MailboxItemReader
	catalog *Catalog
	clock   notification.Clock
}

func NewActionResolver(mailbox MailboxItemReader, catalog *Catalog, clock notification.Clock) (*ActionResolver, error) {
	if mailbox == nil || catalog == nil || clock == nil {
		return nil, unavailable("backend.notification.inbox_action_unavailable", nil)
	}
	return &ActionResolver{mailbox: mailbox, catalog: catalog, clock: clock}, nil
}

func (r *ActionResolver) Resolve(ctx context.Context, query Query, notificationID, actionKey string) (ResolvedAction, error) {
	if query.Scope == ScopeTeam || query.Scope == ScopeDelegated {
		return ResolvedAction{}, forbidden("backend.notification.inbox_team_action_forbidden")
	}
	item, err := r.mailbox.Get(ctx, query, strings.TrimSpace(notificationID))
	if err != nil {
		return ResolvedAction{}, err
	}
	if item.ActionState == ActionCompleted || item.ActionState == ActionExpired || item.ActionState == ActionCancelled {
		return ResolvedAction{}, conflict("backend.notification.inbox_action_unavailable", "action_state", string(item.ActionState))
	}
	actionKey = strings.TrimSpace(actionKey)
	var action *ActionRef
	for index := range item.Actions {
		if item.Actions[index].Key == actionKey {
			action = &item.Actions[index]
			break
		}
	}
	if action == nil {
		return ResolvedAction{}, notFound("backend.notification.inbox_action_not_found", "action_key", actionKey)
	}
	if item.ExpiresAt != "" {
		expiresAt, parseErr := time.Parse(time.RFC3339Nano, item.ExpiresAt)
		if parseErr != nil || !r.clock.Now().UTC().Before(expiresAt) {
			return ResolvedAction{}, conflict("backend.notification.inbox_action_expired")
		}
	}
	descriptor, found := r.catalog.Action(action.Key)
	routeKey := strings.TrimSpace(descriptor.SurfaceRoutes[string(query.Surface)])
	if !found || descriptor.Kind != action.Kind || descriptor.ResourceType != action.ResourceType || routeKey == "" {
		return ResolvedAction{}, conflict("backend.notification.inbox_action_unavailable", "action_key", action.Key)
	}
	params := map[string]string{"resource_type": action.ResourceType, "resource_id": action.ResourceID}
	if action.ResourceType == "project_record" {
		params["object_key"] = item.SubjectType
	}
	return ResolvedAction{
		Key: action.Key, Label: action.Label, Style: action.Style,
		NavigationKind: NavigationSurfaceRoute, RouteKey: routeKey,
		RouteParams: params, Status: "available",
	}, nil
}
