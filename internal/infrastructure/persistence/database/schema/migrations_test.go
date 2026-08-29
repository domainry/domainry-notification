package schema

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	ormbuilder "github.com/domainry/domainry-orm/builder"
	ormdialect "github.com/domainry/domainry-orm/dialect"
	_ "modernc.org/sqlite"
)

type testSchemaProfile struct{ driver string }

func (p testSchemaProfile) ColumnType(kind ColumnKind) (string, error) {
	if p.driver == "mysql" {
		switch kind {
		case IdentifierColumn:
			return "VARCHAR(191)", nil
		case IndexedTextColumn:
			return "VARCHAR(191) CHARACTER SET ascii COLLATE ascii_bin", nil
		case DocumentColumn:
			return "LONGTEXT", nil
		}
	}
	switch kind {
	case IdentifierColumn, IndexedTextColumn, DocumentColumn, PlainTextColumn:
		return "TEXT", nil
	case IntegerColumn:
		return "INTEGER", nil
	case BigIntegerColumn:
		return "BIGINT", nil
	case BooleanColumn:
		return "BOOLEAN", nil
	default:
		return "", fmt.Errorf("test column kind %d is unsupported", kind)
	}
}

func (p testSchemaProfile) SchemaColumn(name string, kind ColumnKind) (ormbuilder.SchemaColumn, error) {
	var columnType ormbuilder.ColumnType
	switch kind {
	case IdentifierColumn, IndexedTextColumn:
		columnType = ormbuilder.TextKeyType(191)
	case DocumentColumn:
		columnType = ormbuilder.LongTextType()
	case PlainTextColumn:
		columnType = ormbuilder.TextType()
	case IntegerColumn:
		columnType = ormbuilder.IntegerType()
	case BigIntegerColumn:
		columnType = ormbuilder.BigIntType()
	case BooleanColumn:
		columnType = ormbuilder.BooleanType()
	default:
		return ormbuilder.SchemaColumn{}, fmt.Errorf("test column kind %d is unsupported", kind)
	}
	column := ormbuilder.DefineColumn(name, columnType)
	if p.driver == "mysql" && kind == IndexedTextColumn {
		column = column.CharacterSet("ascii").Collation("ascii_bin")
	}
	return column, nil
}

func testSchemaMigrations(driver, schemaName, prefix string) ([]SchemaMigration, error) {
	renderer, err := ormdialect.ParseRenderer(driver, schemaName, prefix)
	if err != nil {
		return nil, err
	}
	return SchemaMigrations(testSchemaProfile{driver: driver}, renderer, prefix)
}
func testModuleSchemaBaseline(driver, prefix string) (SchemaBaseline, error) {
	return ModuleSchemaBaseline(testSchemaProfile{driver: driver}, prefix)
}
func testApplicationSchemaMigrations(driver, schemaName, prefix string, scope ApplicationScope) ([]SchemaMigration, error) {
	renderer, err := ormdialect.ParseRenderer(driver, schemaName, prefix)
	if err != nil {
		return nil, err
	}
	return ApplicationSchemaMigrations(testSchemaProfile{driver: driver}, renderer, prefix, scope)
}

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
	migrations, err := testSchemaMigrations("sqlite", "", "")
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
	migrations, err := testSchemaMigrations("mysql", "tenant", "domainry_")
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
			t.Fatalf("migration does not contain %q:\n%s", fragment, joined)
		}
	}
}

func TestSchemaMigrationStatementsDoNotLeakMutableState(t *testing.T) {
	first, err := testSchemaMigrations("sqlite", "", "")
	if err != nil {
		t.Fatal(err)
	}
	first[0].Statements[0] = "mutated"
	second, err := testSchemaMigrations("sqlite", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if second[0].Statements[0] == "mutated" {
		t.Fatal("schema migration leaked mutable state")
	}
}

func TestModuleSchemaBaselineCoversEveryColumnAndDeclaredIndex(t *testing.T) {
	baseline, err := testModuleSchemaBaseline("mysql", "")
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
	migrations, err := testApplicationSchemaMigrations("sqlite", "", "app_one_", scope)
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
	left, err := testApplicationSchemaMigrations("sqlite", "", "left_", ApplicationScope{TenantID: "tenant", WorkspaceID: "workspace", ApplicationKey: "left"})
	if err != nil {
		t.Fatal(err)
	}
	right, err := testApplicationSchemaMigrations("sqlite", "", "right_", ApplicationScope{TenantID: "tenant", WorkspaceID: "workspace", ApplicationKey: "right"})
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
		if _, err := testApplicationSchemaMigrations("sqlite", "", "app_", scope); err == nil {
			t.Fatalf("expected incomplete scope error for %+v", scope)
		}
	}
}
