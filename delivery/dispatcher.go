package delivery

import (
	"context"
	"time"

	"github.com/domainry/domainry-notification"
	"github.com/domainry/domainry-notification/template"
)

// DispatchRequest is the immutable boundary between notification channel
// planning and the host-owned Integration Outbox.
type DispatchRequest struct {
	WorkspaceID      notification.WorkspaceID
	PlanID           string
	EventID          string
	Channel          string
	ConnectorKey     string
	ConnectionKey    string
	Operation        string
	DeduplicationKey string
	Content          template.Rendered
	Decision         Decision
	Fallbacks        []DispatchFallback
	CreatedAt        time.Time
}

// DispatchFallback is a pre-rendered immutable fallback hop. Integration may
// execute it after a provider failure without consulting mutable templates.
type DispatchFallback struct {
	ConnectorKey  string
	ConnectionKey string
	Operation     string
	Content       template.Rendered
}

// DispatchReceipt records acceptance by the dispatch boundary, not provider
// delivery. Provider status belongs to Integration and Connector ledgers.
type DispatchReceipt struct {
	MessageID  string
	AcceptedAt time.Time
}

// Dispatcher accepts an external delivery request. A Plane implementation
// writes to Integration Outbox; a standalone deployment may use another durable
// transport. It never reports provider delivery as part of this call.
type Dispatcher interface {
	Dispatch(context.Context, DispatchRequest) (DispatchReceipt, error)
}
