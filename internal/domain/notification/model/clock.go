package model

import "time"

// Clock is shared by processors that make durable time-based transitions.
// Hosts may inject one clock across the composed Runtime.
type Clock interface {
	Now() time.Time
}
