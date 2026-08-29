package persistence

import (
	"errors"
)

var ErrIncompleteConfig = errors.New("notification sqlstore configuration is incomplete")
