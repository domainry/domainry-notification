package inbox

import (
	"context"

	"github.com/domainry/domainry-notification"
)

// EventStore owns durable intents and atomic materialization. Materialize must
// write inbox items, alert transitions, channel plans, and the terminal event
// state in one transaction.
type EventStore interface {
	Enqueue(context.Context, Event) (Event, bool, error)
	ListDue(context.Context, string, int) ([]Event, error)
	Claim(context.Context, notification.WorkspaceID, string, string, string, string) (Event, bool, error)
	Materialize(context.Context, Event, []Item) error
	Retry(context.Context, Event, string, string, string, string) error
	Fail(context.Context, Event, string, string, string) error
}

type MailboxStore interface {
	ListItems(context.Context, Query) ([]Item, bool, error)
	GetItem(context.Context, Query, string) (Item, bool, error)
	CountFacets(context.Context, Query) (Facets, error)
	SetRead(context.Context, Query, string, string, string) (Item, bool, error)
	SetArchived(context.Context, Query, string, string, string) (Item, bool, error)
	AcknowledgeAlert(context.Context, Query, string, notification.UserID, string) (Item, bool, error)
	MarkAllRead(context.Context, Query, string) (int, error)
}

type SavedViewStore interface {
	ListSavedViews(context.Context, notification.WorkspaceID, notification.UserID, notification.Surface) ([]SavedView, error)
	SaveSavedView(context.Context, notification.WorkspaceID, notification.UserID, notification.Surface, SavedView) (SavedView, error)
	DeleteSavedView(context.Context, notification.WorkspaceID, notification.UserID, notification.Surface, string) (bool, error)
}

type DelegationStore interface {
	ListDelegations(context.Context, notification.WorkspaceID, notification.UserID, notification.Surface) ([]Delegation, error)
	SaveDelegation(context.Context, Delegation) (Delegation, error)
	DeleteDelegation(context.Context, notification.WorkspaceID, notification.UserID, string) (bool, error)
	ListActiveDelegatedOwnerIDs(context.Context, notification.WorkspaceID, notification.UserID, notification.Surface, string) ([]notification.UserID, error)
}

type MetricsStore interface {
	GovernanceMetrics(context.Context, notification.WorkspaceID, string) (GovernanceMetrics, error)
}
