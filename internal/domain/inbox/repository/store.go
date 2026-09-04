package repository

import (
	"context"

	inboxmodel "github.com/domainry/domainry-notification/internal/domain/inbox/model"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

// EventStore owns durable intents and atomic materialization. Materialize must
// write inbox items, alert transitions, channel plans, and the terminal event
// state in one transaction.
type EventStore interface {
	Enqueue(context.Context, inboxmodel.Event) (inboxmodel.Event, bool, error)
	ListDue(context.Context, string, int) ([]inboxmodel.Event, error)
	Claim(context.Context, notification.WorkspaceID, string, string, string, string) (inboxmodel.Event, bool, error)
	Materialize(context.Context, inboxmodel.Event, []inboxmodel.Item) error
	Retry(context.Context, inboxmodel.Event, string, string, string, string) error
	Fail(context.Context, inboxmodel.Event, string, string, string) error
}

type MailboxStore interface {
	ListItems(context.Context, inboxmodel.Query) ([]inboxmodel.Item, bool, error)
	GetItem(context.Context, inboxmodel.Query, string) (inboxmodel.Item, bool, error)
	CountFacets(context.Context, inboxmodel.Query) (inboxmodel.Facets, error)
	SetRead(context.Context, inboxmodel.Query, string, string, string) (inboxmodel.Item, bool, error)
	SetArchived(context.Context, inboxmodel.Query, string, string, string) (inboxmodel.Item, bool, error)
	AcknowledgeAlert(context.Context, inboxmodel.Query, string, notification.UserID, string) (inboxmodel.Item, bool, error)
	MarkAllRead(context.Context, inboxmodel.Query, string) (int, error)
}

type SavedViewStore interface {
	ListSavedViews(context.Context, notification.WorkspaceID, notification.UserID) ([]inboxmodel.SavedView, error)
	SaveSavedView(context.Context, notification.WorkspaceID, notification.UserID, inboxmodel.SavedView) (inboxmodel.SavedView, error)
	DeleteSavedView(context.Context, notification.WorkspaceID, notification.UserID, string) (bool, error)
}

type DelegationStore interface {
	ListDelegations(context.Context, notification.WorkspaceID, notification.UserID) ([]inboxmodel.Delegation, error)
	SaveDelegation(context.Context, inboxmodel.Delegation) (inboxmodel.Delegation, error)
	DeleteDelegation(context.Context, notification.WorkspaceID, notification.UserID, string) (bool, error)
	ListActiveDelegatedOwnerIDs(context.Context, notification.WorkspaceID, notification.UserID, string) ([]notification.UserID, error)
}

type MetricsStore interface {
	GovernanceMetrics(context.Context, notification.WorkspaceID, string) (inboxmodel.GovernanceMetrics, error)
}
