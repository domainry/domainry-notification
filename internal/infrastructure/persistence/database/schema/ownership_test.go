package schema_test

import (
	"slices"
	"testing"

	"github.com/domainry/domainry-foundation/schemaownership"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/schema"
)

func TestSchemaOwnershipCoversSevenWorkspaceTables(t *testing.T) {
	ownership := schema.SchemaOwnership()
	if err := schemaownership.ValidateAll(ownership); err != nil {
		t.Fatal(err)
	}
	if len(ownership) != 7 {
		t.Fatalf("owned table count=%d", len(ownership))
	}
	for _, table := range ownership {
		if table.WorkspaceScope != schemaownership.ScopeWorkspace {
			t.Fatalf("table %q has scope %q", table.Name, table.WorkspaceScope)
		}
	}
	if !slices.Equal(schema.OwnedTables(), schemaownership.Names(ownership)) {
		t.Fatalf("owned tables=%v ownership=%+v", schema.OwnedTables(), ownership)
	}
	ownership[0].PrimaryKey[0] = "mutated"
	if schema.SchemaOwnership()[0].PrimaryKey[0] == "mutated" {
		t.Fatal("schema ownership leaked mutable state")
	}
}
