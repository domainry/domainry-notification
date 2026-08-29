package schema

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestBaseSchemaMatchesOwnershipAndRunsOnSQLite(t *testing.T) {
	if len(ownedSchemaTables()) != len(tableOwnership) {
		t.Fatalf("schema tables=%d ownership=%d", len(ownedSchemaTables()), len(tableOwnership))
	}
	owned := map[string]bool{}
	for _, table := range tableOwnership {
		owned[table.Name] = true
	}
	defined := map[string]bool{}
	for _, table := range baseSchemaTables {
		if defined[table.name] || !owned[table.name] {
			t.Fatalf("invalid schema table %q", table.name)
		}
		defined[table.name] = true
	}
	for _, table := range retentionArchiveTables {
		if defined[table.name] || !owned[table.name] {
			t.Fatalf("invalid schema table %q", table.name)
		}
		defined[table.name] = true
	}
	for _, table := range migrationControlTables {
		if defined[table.name] || !owned[table.name] {
			t.Fatalf("invalid schema table %q", table.name)
		}
		defined[table.name] = true
	}
	for _, index := range baseSchemaIndexes {
		if !defined[index.table] {
			t.Fatalf("index %q references unowned table %q", index.name, index.table)
		}
	}
	migrations, err := SchemaMigrations(SQLite, "", "")
	if err != nil || len(migrations) != 3 || migrations[0].Version != 1 || migrations[0].Name != "create_notification_schema" || migrations[1].Version != 2 || migrations[2].Version != 3 || migrations[2].Name != "create_notification_migration_control" {
		t.Fatalf("migrations=%+v err=%v", migrations, err)
	}
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, migration := range migrations {
		for _, statement := range migration.Statements {
			if _, err := db.Exec(statement); err != nil {
				t.Fatalf("execute %q: %v", statement, err)
			}
		}
	}
	for table := range owned {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("table %q count=%d err=%v", table, count, err)
		}
	}
}

func TestSchemaMigrationsRenderPhysicalNamesAndMySQLTypes(t *testing.T) {
	migrations, err := SchemaMigrations(MySQL, "tenant", "domainry_")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(migrations[0].Statements, "\n")
	for _, fragment := range []string{
		"`tenant`.`domainry_notification_events`",
		"`domainry_uniq_notification_event_workspace_identity`",
		"VARCHAR(191) CHARACTER SET ascii COLLATE ascii_bin",
		"LONGTEXT",
		"`failure` TEXT NOT NULL",
	} {
		if !strings.Contains(joined, fragment) {
			t.Fatalf("migration does not contain %q", fragment)
		}
	}
}

func TestSchemaMigrationStatementsDoNotLeakMutableState(t *testing.T) {
	first, err := SchemaMigrations(SQLite, "", "")
	if err != nil {
		t.Fatal(err)
	}
	first[0].Statements[0] = "mutated"
	second, err := SchemaMigrations(SQLite, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if second[0].Statements[0] == "mutated" {
		t.Fatal("schema migration leaked mutable state")
	}
}

func TestModuleSchemaBaselineCoversEveryColumnAndDeclaredIndex(t *testing.T) {
	baseline, err := ModuleSchemaBaseline(MySQL, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(baseline.Tables) != len(baseSchemaTables) {
		t.Fatalf("baseline tables=%d want=%d", len(baseline.Tables), len(baseSchemaTables))
	}
	for tableIndex, table := range baseline.Tables {
		definition := baseSchemaTables[tableIndex]
		if table.Name != definition.name || len(table.Columns) != len(definition.columns) {
			t.Fatalf("baseline table[%d]=%+v", tableIndex, table)
		}
		for columnIndex, column := range table.Columns {
			if column.Name != definition.columns[columnIndex].name || strings.Contains(column.Type, " ") {
				t.Fatalf("baseline column %s.%s=%+v", table.Name, column.Name, column)
			}
		}
	}
	indexes := 0
	for _, table := range baseline.Tables {
		indexes += len(table.Indexes)
	}
	if indexes != len(baseSchemaIndexes) {
		t.Fatalf("baseline indexes=%d want=%d", indexes, len(baseSchemaIndexes))
	}
}

func TestApplicationSchemaMigrationsPersistExactOwnership(t *testing.T) {
	scope := ApplicationScope{TenantID: "tenant-'one", WorkspaceID: "workspace-one", ApplicationKey: "application-one"}
	migrations, err := ApplicationSchemaMigrations(SQLite, "", "app_one_", scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 3 || migrations[0].Name != "create_application_schema" || migrations[1].Version != 2 || migrations[2].Version != 3 {
		t.Fatalf("migrations=%+v", migrations)
	}
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, migration := range migrations {
		for _, statement := range migration.Statements {
			if _, err := db.Exec(statement); err != nil {
				t.Fatalf("execute %q: %v", statement, err)
			}
		}
	}
	if _, err := db.Exec(`INSERT INTO app_one_notification_events (id, workspace_id, source, source_event_id, status, payload_json, occurred_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, "event-one", scope.WorkspaceID, "test", "source-event-one", "pending", `{}`, "1", "1", "1"); err != nil {
		t.Fatal(err)
	}
	var tenantID, applicationKey string
	if err := db.QueryRow(`SELECT tenant_id, application_key FROM app_one_notification_events WHERE id = ?`, "event-one").Scan(&tenantID, &applicationKey); err != nil {
		t.Fatal(err)
	}
	if tenantID != scope.TenantID || applicationKey != scope.ApplicationKey {
		t.Fatalf("ownership=(%q,%q), want (%q,%q)", tenantID, applicationKey, scope.TenantID, scope.ApplicationKey)
	}
}

func TestApplicationSchemaMigrationsUseDistinctPhysicalNamespaces(t *testing.T) {
	left, err := ApplicationSchemaMigrations(SQLite, "", "left_", ApplicationScope{TenantID: "tenant", WorkspaceID: "workspace", ApplicationKey: "left"})
	if err != nil {
		t.Fatal(err)
	}
	right, err := ApplicationSchemaMigrations(SQLite, "", "right_", ApplicationScope{TenantID: "tenant", WorkspaceID: "workspace", ApplicationKey: "right"})
	if err != nil {
		t.Fatal(err)
	}
	leftSQL, rightSQL := strings.Join(left[0].Statements, "\n"), strings.Join(right[0].Statements, "\n")
	if !strings.Contains(leftSQL, `"left_notification_events"`) || strings.Contains(leftSQL, `"right_notification_events"`) {
		t.Fatalf("left migration has an invalid physical namespace")
	}
	if !strings.Contains(rightSQL, `"right_notification_events"`) || strings.Contains(rightSQL, `"left_notification_events"`) {
		t.Fatalf("right migration has an invalid physical namespace")
	}
}

func TestApplicationSchemaMigrationsRequireExactScope(t *testing.T) {
	for _, scope := range []ApplicationScope{
		{WorkspaceID: "workspace", ApplicationKey: "application"},
		{TenantID: "tenant", ApplicationKey: "application"},
		{TenantID: "tenant", WorkspaceID: "workspace"},
	} {
		if _, err := ApplicationSchemaMigrations(SQLite, "", "app_", scope); err == nil {
			t.Fatalf("expected incomplete scope error for %+v", scope)
		}
	}
}
