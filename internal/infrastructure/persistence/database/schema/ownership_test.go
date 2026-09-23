package schema_test

import (
	"slices"
	"testing"

	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/schema"
)

func TestSchemaOwnershipSeparatesSystemAndWorkspaceState(t *testing.T) {
	ownership := schema.SchemaOwnership()
	if len(ownership) != 7 {
		t.Fatalf("owned table count=%d", len(ownership))
	}
	system := []string{}
	workspace := []string{}
	for _, table := range ownership {
		switch table.Scope {
		case schema.SystemData:
			system = append(system, table.Name)
		case schema.WorkspaceData:
			workspace = append(workspace, table.Name)
		default:
			t.Fatalf("table %q has unknown scope %q", table.Name, table.Scope)
		}
	}
	wantSystem := []string{}
	if !slices.Equal(system, wantSystem) {
		t.Fatalf("system tables=%v", system)
	}
	if len(workspace) != 7 || !slices.Contains(workspace, "_notification_user_settings") || !slices.Contains(workspace, "_notification_deliveries") {
		t.Fatalf("workspace tables=%v", workspace)
	}
	flat := schema.OwnedTables()
	flat[0] = "mutated"
	if schema.OwnedTables()[0] == "mutated" {
		t.Fatal("owned table inventory leaked mutable state")
	}
}
