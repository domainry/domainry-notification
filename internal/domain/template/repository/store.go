package repository

import (
	"context"
	"errors"

	templatemodel "github.com/domainry/domainry-notification/internal/domain/template/model"
)

var (
	ErrRecordConflict      = errors.New("notification template record changed")
	ErrRecordNotFound      = errors.New("notification template record not found")
	ErrPublicationConflict = errors.New("notification publication request changed")
	ErrPublicationNotFound = errors.New("notification publication request not found")
)

// Store is the durable boundary for template records, immutable versions, and
// publication requests. Delivery policy and inbox persistence intentionally use
// separate capability-owned interfaces.
type Store interface {
	SyncPublished(context.Context, []templatemodel.Template) error
	List(context.Context) ([]templatemodel.Record, error)
	Get(context.Context, string) (templatemodel.Record, bool, error)
	ListVersions(context.Context, string) ([]templatemodel.Version, error)
	GetVersion(context.Context, string, int) (templatemodel.Version, bool, error)
	SaveDraft(context.Context, templatemodel.Template, string, string) (templatemodel.Record, error)
	Publish(context.Context, templatemodel.Template, string, string) (templatemodel.Record, error)
	Disable(context.Context, string, string, string) (templatemodel.Record, error)

	ListPublicationRequests(context.Context, string) ([]templatemodel.PublicationRequest, error)
	GetPublicationRequest(context.Context, string) (templatemodel.PublicationRequest, bool, error)
	CreatePublicationRequest(context.Context, templatemodel.PublicationRequest) error
	TransitionPublicationRequest(context.Context, string, templatemodel.PublicationStatus, templatemodel.PublicationTransition) (templatemodel.PublicationRequest, error)
	ListDuePublicationRequests(context.Context, string, string, int) ([]templatemodel.PublicationRequest, error)
	ClaimPublicationRequest(context.Context, string, string, string, string) (templatemodel.PublicationRequest, bool, error)
	HasOpenPublicationRequest(context.Context, string) (bool, error)
}

// RevisionStore is an optional optimization used to avoid reloading an
// unchanged published catalog. Store remains correct without it.
type RevisionStore interface {
	PublishedRevision(context.Context) (string, error)
}
