package notification_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/domainry/domainry-notification/sqlstore"
)

func TestOwnedTablesAreCanonicalAndSorted(t *testing.T) {
	tables := sqlstore.OwnedTables()
	if !slices.IsSorted(tables) {
		t.Fatalf("notification table ownership must remain sorted: %v", tables)
	}
	seen := map[string]bool{}
	for _, table := range tables {
		if table != strings.TrimSpace(table) || !strings.HasPrefix(table, "notification_") || seen[table] {
			t.Fatalf("invalid notification-owned table %q", table)
		}
		seen[table] = true
	}
	if len(seen) != 15 || !seen["notification_retention_archive"] {
		t.Fatalf("notification table ownership=%v", tables)
	}
}
