package sqlstore

import (
	"fmt"
	"strings"
)

type ApplicationScope struct {
	TenantID, WorkspaceID, ApplicationKey string
}

// SchemaMigration is one ordered, immutable schema change. Hosts execute and
// record these versions with their existing migration ledger; this library does
// not create a second migration-history table inside the same database.
type SchemaMigration struct {
	Version    uint
	Name       string
	Statements []string
}

// SchemaBaseline is the complete physical contract used only to adopt a
// pre-extraction Module schema. It deliberately excludes database-generated
// indexes while including every source-declared index.
type SchemaBaseline struct {
	Tables []SchemaBaselineTable
}

type SchemaBaselineTable struct {
	Name    string
	Columns []SchemaBaselineColumn
	Indexes []SchemaBaselineIndex
}

type SchemaBaselineColumn struct {
	Name       string
	Type       string
	Nullable   bool
	PrimaryKey bool
}

type SchemaBaselineIndex struct {
	Name    string
	Unique  bool
	Columns []string
}

// SchemaMigrations renders the complete migration history for one physical
// naming configuration. Existing installations must verify and baseline
// version 1 instead of re-running it over Plane-owned legacy tables.
func SchemaMigrations(driver Driver, schema, tablePrefix string) ([]SchemaMigration, error) {
	dialect, err := NewDialect(driver, schema, tablePrefix)
	if err != nil {
		return nil, err
	}
	statements, err := renderBaseSchema(driver, tablePrefix, dialect, nil)
	if err != nil {
		return nil, err
	}
	return []SchemaMigration{{Version: 1, Name: "create_notification_schema", Statements: statements}}, nil
}

// ModuleSchemaBaseline renders the exact legacy physical shape for one
// dialect. A host must match this contract before recording migration 1 as an
// adopted baseline.
func ModuleSchemaBaseline(driver Driver, tablePrefix string) (SchemaBaseline, error) {
	result := SchemaBaseline{Tables: make([]SchemaBaselineTable, len(baseSchemaTables))}
	byName := make(map[string]*SchemaBaselineTable, len(baseSchemaTables))
	for tableIndex, table := range baseSchemaTables {
		value := SchemaBaselineTable{Name: tablePrefix + table.name, Columns: make([]SchemaBaselineColumn, len(table.columns))}
		for columnIndex, column := range table.columns {
			physicalType, err := renderColumnType(driver, column.kind)
			if err != nil {
				return SchemaBaseline{}, err
			}
			if separator := strings.IndexByte(physicalType, ' '); separator >= 0 {
				physicalType = physicalType[:separator]
			}
			value.Columns[columnIndex] = SchemaBaselineColumn{Name: column.name, Type: physicalType, Nullable: column.nullable, PrimaryKey: column.primaryKey}
		}
		result.Tables[tableIndex] = value
		byName[table.name] = &result.Tables[tableIndex]
	}
	for _, index := range baseSchemaIndexes {
		table := byName[index.table]
		if table == nil {
			return SchemaBaseline{}, fmt.Errorf("notification schema index %q references unknown table %q", index.name, index.table)
		}
		table.Indexes = append(table.Indexes, SchemaBaselineIndex{Name: tablePrefix + index.name, Unique: index.unique, Columns: append([]string(nil), index.columns...)})
	}
	return result, nil
}

// ApplicationSchemaMigrations renders the standalone SaaS schema for one
// exact application namespace. Every row carries explicit tenant and
// application ownership, including system-scoped template/policy rows. The
// table prefix provides an additional physical isolation boundary while all
// application namespaces may share one service-owned database pool.
func ApplicationSchemaMigrations(driver Driver, schema, tablePrefix string, scope ApplicationScope) ([]SchemaMigration, error) {
	scope.TenantID, scope.WorkspaceID, scope.ApplicationKey = strings.TrimSpace(scope.TenantID), strings.TrimSpace(scope.WorkspaceID), strings.TrimSpace(scope.ApplicationKey)
	if scope.TenantID == "" || scope.WorkspaceID == "" || scope.ApplicationKey == "" {
		return nil, fmt.Errorf("notification SaaS application scope is incomplete")
	}
	dialect, err := NewDialect(driver, schema, tablePrefix)
	if err != nil {
		return nil, err
	}
	statements, err := renderBaseSchema(driver, tablePrefix, dialect, &scope)
	if err != nil {
		return nil, err
	}
	return []SchemaMigration{{Version: 1, Name: "create_notification_saas_application_schema", Statements: statements}}, nil
}

type schemaColumn struct {
	name       string
	kind       schemaColumnKind
	nullable   bool
	defaultSQL string
	primaryKey bool
}

type schemaColumnKind uint8

const (
	identifierColumn schemaColumnKind = iota
	indexedTextColumn
	documentColumn
	plainTextColumn
	integerColumn
	bigIntegerColumn
	booleanColumn
)

type schemaTable struct {
	name    string
	columns []schemaColumn
}

type schemaIndex struct {
	name    string
	table   string
	unique  bool
	columns []string
}

func renderBaseSchema(driver Driver, indexPrefix string, dialect Dialect, application *ApplicationScope) ([]string, error) {
	statements := make([]string, 0, len(baseSchemaTables)+len(baseSchemaIndexes))
	for _, table := range baseSchemaTables {
		columns := append([]schemaColumn(nil), table.columns...)
		if application != nil {
			columns = append([]schemaColumn{
				defaulted("tenant_id", identifierColumn, sqlStringLiteral(application.TenantID)),
				defaulted("application_key", identifierColumn, sqlStringLiteral(application.ApplicationKey)),
			}, columns...)
		}
		parts := make([]string, len(columns))
		for index, column := range columns {
			columnType, err := renderColumnType(driver, column.kind)
			if err != nil {
				return nil, err
			}
			part := dialect.Identifier(column.name) + " " + columnType
			if !column.nullable {
				part += " NOT NULL"
			}
			if column.defaultSQL != "" {
				part += " DEFAULT " + column.defaultSQL
			}
			if column.primaryKey {
				part += " PRIMARY KEY"
			}
			parts[index] = part
		}
		statements = append(statements, "CREATE TABLE "+dialect.Table(table.name)+" ("+strings.Join(parts, ", ")+")")
	}
	for _, index := range baseSchemaIndexes {
		columns := make([]string, len(index.columns))
		for position, column := range index.columns {
			columns[position] = dialect.Identifier(column)
		}
		unique := ""
		if index.unique {
			unique = "UNIQUE "
		}
		statements = append(statements, "CREATE "+unique+"INDEX "+dialect.Identifier(indexPrefix+index.name)+" ON "+dialect.Table(index.table)+" ("+strings.Join(columns, ", ")+")")
	}
	return statements, nil
}

func sqlStringLiteral(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }

func renderColumnType(driver Driver, kind schemaColumnKind) (string, error) {
	switch kind {
	case identifierColumn:
		if driver == MySQL {
			return "VARCHAR(191)", nil
		}
		return "TEXT", nil
	case indexedTextColumn:
		if driver == MySQL {
			return "VARCHAR(191) CHARACTER SET ascii COLLATE ascii_bin", nil
		}
		return "TEXT", nil
	case documentColumn:
		if driver == MySQL {
			return "LONGTEXT", nil
		}
		return "TEXT", nil
	case plainTextColumn:
		return "TEXT", nil
	case integerColumn:
		return "INTEGER", nil
	case bigIntegerColumn:
		return "BIGINT", nil
	case booleanColumn:
		return "BOOLEAN", nil
	default:
		return "", fmt.Errorf("notification schema column kind %d is unsupported", kind)
	}
}
