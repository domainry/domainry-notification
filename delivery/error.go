package delivery

import "errors"

var (
	ErrFrequencyExceeded = errors.New("notification delivery frequency exceeded")
	ErrDuplicate         = errors.New("duplicate notification delivery")
)
