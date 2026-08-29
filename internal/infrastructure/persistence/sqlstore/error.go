package sqlstore

import (
	"errors"

	inboxstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/sqlstore/inbox"
)

var (
	ErrIncompleteConfig    = errors.New("notification sqlstore configuration is incomplete")
	ErrLeaseLost           = errors.New("notification durable-work lease was lost")
	ErrMutationConflict    = inboxstore.ErrMutationConflict
	ErrIdempotencyConflict = errors.New("notification request identity was reused with different content")
)
