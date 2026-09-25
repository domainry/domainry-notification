package base

import "testing"

func TestTimestampColumnsRequirePrecomputedMilliseconds(t *testing.T) {
	columns := []string{"id", "created_at", "updated_at"}
	if err := validateTimestampValues(columns, []any{"item", int64(1_790_000_000_000), int64(0)}); err != nil {
		t.Fatal(err)
	}
	if err := validateTimestampValues(columns, []any{"item", "2026-09-25T00:00:00Z", int64(0)}); err == nil {
		t.Fatal("accepted a string timestamp for durable storage")
	}
}
