package schema_test

import (
	"slices"
	"testing"

	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/sqlstore/schema"
)

func TestSchemaOwnershipSeparatesSystemAndWorkspaceState(t *testing.T) {
	ownership := schema.SchemaOwnership()
	if len(ownership) != 16 {
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
	wantSystem := []string{
		"notification_delivery_policy",
		"notification_template_publication_locks",
		"notification_template_publication_requests",
		"notification_template_records",
		"notification_template_versions",
	}
	if !slices.Equal(system, wantSystem) {
		t.Fatalf("system tables=%v", system)
	}
	if len(workspace) != 11 || !slices.Contains(workspace, "notification_retention_archive") || !slices.Contains(workspace, "notification_migration_controls") {
		t.Fatalf("workspace tables=%v", workspace)
	}
	flat := schema.OwnedTables()
	flat[0] = "mutated"
	if schema.OwnedTables()[0] == "mutated" {
		t.Fatal("owned table inventory leaked mutable state")
	}
}
