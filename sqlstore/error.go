package sqlstore

import "errors"

var (
	ErrIncompleteConfig = errors.New("notification sqlstore configuration is incomplete")
	ErrLeaseLost        = errors.New("notification durable-work lease was lost")
)
