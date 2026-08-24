package template

import (
	"context"
	"errors"
)

var (
	ErrRecordConflict      = errors.New("notification template record changed")
	ErrRecordNotFound      = errors.New("notification template record not found")
	ErrPublicationConflict = errors.New("notification publication request changed")
)

// Store is the durable boundary for template records, immutable versions, and
// publication requests. Delivery policy and inbox persistence intentionally use
// separate capability-owned interfaces.
type Store interface {
	SyncPublished(context.Context, []Template) error
	List(context.Context) ([]Record, error)
	Get(context.Context, string) (Record, bool, error)
	ListVersions(context.Context, string) ([]Version, error)
	GetVersion(context.Context, string, int) (Version, bool, error)
	SaveDraft(context.Context, Template, string, string) (Record, error)
	Publish(context.Context, Template, string, string) (Record, error)
	Disable(context.Context, string, string, string) (Record, error)

	ListPublicationRequests(context.Context, string) ([]PublicationRequest, error)
	GetPublicationRequest(context.Context, string) (PublicationRequest, bool, error)
	CreatePublicationRequest(context.Context, PublicationRequest) error
	TransitionPublicationRequest(context.Context, string, PublicationStatus, PublicationTransition) (PublicationRequest, error)
	ListDuePublicationRequests(context.Context, string, string, int) ([]PublicationRequest, error)
	ClaimPublicationRequest(context.Context, string, string, string, string) (PublicationRequest, bool, error)
	HasOpenPublicationRequest(context.Context, string) (bool, error)
}

// RevisionStore is an optional optimization used to avoid reloading an
// unchanged published catalog. Store remains correct without it.
type RevisionStore interface {
	PublishedRevision(context.Context) (string, error)
}
