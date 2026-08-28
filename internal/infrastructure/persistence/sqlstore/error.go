package sqlstore

import "errors"

var (
	ErrIncompleteConfig    = errors.New("notification sqlstore configuration is incomplete")
	ErrLeaseLost           = errors.New("notification durable-work lease was lost")
	ErrMutationConflict    = errors.New("notification state changed concurrently")
	ErrIdempotencyConflict = errors.New("notification request identity was reused with different content")
)
