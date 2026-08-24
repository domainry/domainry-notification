package notification

import "time"

// TimestampLayout is fixed-width so timestamps retain temporal ordering in
// portable TEXT/VARCHAR columns across SQLite, PostgreSQL, and MySQL.
const TimestampLayout = "2006-01-02T15:04:05.000000000Z"

func Timestamp(value time.Time) string {
	return value.UTC().Format(TimestampLayout)
}
