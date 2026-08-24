package notification

import (
	"slices"
	"strings"
	"testing"
)

func TestOwnedTablesAreCanonicalAndSorted(t *testing.T) {
	if !slices.IsSorted(OwnedTables[:]) {
		t.Fatalf("notification table ownership must remain sorted: %v", OwnedTables)
	}
	seen := map[string]bool{}
	for _, table := range OwnedTables {
		if table != strings.TrimSpace(table) || !strings.HasPrefix(table, "notification_") || seen[table] {
			t.Fatalf("invalid notification-owned table %q", table)
		}
		seen[table] = true
	}
	if len(seen) != 14 {
		t.Fatalf("notification table ownership count=%d, want 14", len(seen))
	}
}
