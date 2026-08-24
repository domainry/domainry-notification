package sqlstore

import (
	"fmt"
	"strings"
)

// SchemaMigration is one ordered, immutable schema change. Hosts execute and
// record these versions with their existing migration ledger; this library does
// not create a second migration-history table inside the same database.
type SchemaMigration struct {
	Version    uint
	Name       string
	Statements []string
}

// SchemaMigrations renders the complete migration history for one physical
// naming configuration. Existing installations must verify and baseline
// version 1 instead of re-running it over Plane-owned legacy tables.
func SchemaMigrations(driver Driver, schema, tablePrefix string) ([]SchemaMigration, error) {
	dialect, err := NewDialect(driver, schema, tablePrefix)
	if err != nil {
		return nil, err
	}
	statements, err := renderBaseSchema(driver, tablePrefix, dialect)
	if err != nil {
		return nil, err
	}
	return []SchemaMigration{{Version: 1, Name: "create_notification_schema", Statements: statements}}, nil
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

func renderBaseSchema(driver Driver, indexPrefix string, dialect Dialect) ([]string, error) {
	statements := make([]string, 0, len(baseSchemaTables)+len(baseSchemaIndexes))
	for _, table := range baseSchemaTables {
		parts := make([]string, len(table.columns))
		for index, column := range table.columns {
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
