package sqlstore

import (
	"errors"

	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/sqlstore/shared"
)

var (
	ErrIncompleteConfig    = errors.New("notification sqlstore configuration is incomplete")
	ErrLeaseLost           = shared.ErrLeaseLost
	ErrMutationConflict    = shared.ErrMutationConflict
	ErrIdempotencyConflict = shared.ErrIdempotencyConflict
)
