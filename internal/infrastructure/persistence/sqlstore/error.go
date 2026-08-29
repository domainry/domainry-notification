package sqlstore

import (
	"errors"

	deliverystore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/sqlstore/delivery"
	inboxstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/sqlstore/inbox"
)

var (
	ErrIncompleteConfig    = errors.New("notification sqlstore configuration is incomplete")
	ErrLeaseLost           = deliverystore.ErrLeaseLost
	ErrMutationConflict    = inboxstore.ErrMutationConflict
	ErrIdempotencyConflict = errors.New("notification request identity was reused with different content")
)
