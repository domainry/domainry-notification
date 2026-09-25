package model

import (
	"strconv"
	"strings"
	"time"
)

// TimestampLayout is fixed-width so timestamps retain temporal ordering in
// portable TEXT/VARCHAR columns across SQLite, PostgreSQL, and MySQL.
const TimestampLayout = "2006-01-02T15:04:05.000000000Z"

func Timestamp(value time.Time) string {
	return value.UTC().Truncate(time.Millisecond).Format(TimestampLayout)
}

// TimestampMillis is the only persistence-boundary representation for an
// absolute instant. Empty optional values use numeric zero in current schemas.
func TimestampMillis(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if millis, err := strconv.ParseInt(value, 10, 64); err == nil {
		return millis
	}
	parsed, err := time.Parse(TimestampLayout, value)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339Nano, value)
	}
	if err != nil {
		return 0
	}
	return parsed.UTC().UnixMilli()
}

func MillisTimestamp(value int64) string {
	if value == 0 {
		return ""
	}
	return Timestamp(time.UnixMilli(value))
}
