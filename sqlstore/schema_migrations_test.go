package sqlstore

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestBaseSchemaMatchesOwnershipAndRunsOnSQLite(t *testing.T) {
	if len(baseSchemaTables) != len(tableOwnership) {
		t.Fatalf("schema tables=%d ownership=%d", len(baseSchemaTables), len(tableOwnership))
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
	for _, index := range baseSchemaIndexes {
		if !defined[index.table] {
			t.Fatalf("index %q references unowned table %q", index.name, index.table)
		}
	}
	migrations, err := SchemaMigrations(SQLite, "", "")
	if err != nil || len(migrations) != 1 || migrations[0].Version != 1 || migrations[0].Name != "create_notification_schema" {
		t.Fatalf("migrations=%+v err=%v", migrations, err)
	}
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range migrations[0].Statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("execute %q: %v", statement, err)
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
