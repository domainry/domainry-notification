package module

import (
	"slices"
	"testing"

	"github.com/domainry/domainry-foundation/schemaownership"
)

func TestPublicSchemaOwnershipMatchesNotificationInventory(t *testing.T) {
	tables := SchemaOwnership()
	if err := schemaownership.ValidateAll(tables); err != nil {
		t.Fatal(err)
	}
	if len(tables) != 7 || !slices.Equal(OwnedTables(), schemaownership.Names(tables)) {
		t.Fatalf("Notification schema ownership=%d tables=%v", len(tables), OwnedTables())
	}
}
