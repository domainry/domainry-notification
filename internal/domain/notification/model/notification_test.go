package model_test

import (
	"slices"
	"strings"
	"testing"

	sqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence"
)

func TestOwnedTablesAreCanonicalAndSorted(t *testing.T) {
	tables := sqlstore.OwnedTables()
	if !slices.IsSorted(tables) {
		t.Fatalf("notification table ownership must remain sorted: %v", tables)
	}
	seen := map[string]bool{}
	for _, table := range tables {
		if table != strings.TrimSpace(table) || !strings.HasPrefix(table, "_notification_") || seen[table] {
			t.Fatalf("invalid notification-owned table %q", table)
		}
		seen[table] = true
	}
	if len(seen) != 7 || !seen["_notification_user_settings"] || !seen["_notification_deliveries"] {
		t.Fatalf("notification table ownership=%v", tables)
	}
}
