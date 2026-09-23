package schema

import (
	"fmt"
	ormschema "github.com/domainry/domainry-orm/schema"
	"strings"

	"github.com/domainry/domainry-notification-sdk/modulehost"
	ormmigration "github.com/domainry/domainry-orm/migration"
)

type Profile interface {
	ColumnType(ColumnKind) (string, error)
	SchemaColumn(string, ColumnKind) (ormschema.ColumnDefinition, error)
}

type ApplicationScope struct {
	WorkspaceID, ApplicationKey string
}

// SchemaMigration is one ordered, immutable schema change. Hosts execute and
// record these versions with their existing migration ledger; this library does
// not create a second migration-history table inside the same database.
type SchemaMigration = ormmigration.Migration

// SchemaBaseline is the complete physical contract used only to adopt a
// pre-extraction Module schema. It deliberately excludes database-generated
// indexes while including every source-declared index.
type SchemaBaseline = ormmigration.Baseline
type SchemaBaselineTable = ormmigration.Table
type SchemaBaselineColumn = ormmigration.Column
type SchemaBaselineIndex = ormmigration.Index

// SchemaMigrations renders the complete migration history for one physical
// naming configuration. Existing installations must verify and baseline
// version 1 instead of re-running it over Plane-owned legacy tables.
func SchemaMigrations(profile Profile, dialect modulehost.Dialect, tablePrefix string) ([]SchemaMigration, error) {
	statements, err := renderBaseSchema(profile, tablePrefix, dialect, nil)
	if err != nil {
		return nil, err
	}
	return []SchemaMigration{{Version: 1, Name: "create_notification_schema", Statements: statements}}, nil
}

// ModuleSchemaBaseline renders the exact legacy physical shape for one
// dialect. A host must match this contract before recording migration 1 as an
// adopted baseline.
func ModuleSchemaBaseline(profile Profile, tablePrefix string) (SchemaBaseline, error) {
	result := SchemaBaseline{Tables: make([]SchemaBaselineTable, len(baseSchemaTables))}
	byName := make(map[string]*SchemaBaselineTable, len(baseSchemaTables))
	for tableIndex, table := range baseSchemaTables {
		value := SchemaBaselineTable{Name: tablePrefix + table.name, Columns: make([]SchemaBaselineColumn, len(table.columns))}
		for columnIndex, column := range table.columns {
			physicalType, err := profile.ColumnType(column.kind)
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
// exact application namespace. Every row carries workspace and application
// ownership, including system-scoped template/policy rows. Each standalone
// database is bound to exactly one application.
func ApplicationSchemaMigrations(profile Profile, dialect modulehost.Dialect, tablePrefix string, scope ApplicationScope) ([]SchemaMigration, error) {
	scope.WorkspaceID, scope.ApplicationKey = strings.TrimSpace(scope.WorkspaceID), strings.TrimSpace(scope.ApplicationKey)
	if scope.WorkspaceID == "" || scope.ApplicationKey == "" {
		return nil, fmt.Errorf("notification SaaS application scope is incomplete")
	}
	statements, err := renderBaseSchema(profile, tablePrefix, dialect, &scope)
	if err != nil {
		return nil, err
	}
	return []SchemaMigration{{Version: 1, Name: "create_application_schema", Statements: statements}}, nil
}

func SharedOperationSchemaMigrations(profile Profile, dialect modulehost.Dialect) ([]SchemaMigration, error) {
	statements, err := renderSchema(profile, "", dialect, nil, sharedOperationTables, sharedOperationIndexes)
	if err != nil {
		return nil, err
	}
	return []SchemaMigration{{Version: 1, Name: "create_shared_operations", Statements: statements}}, nil
}

type schemaColumn struct {
	name         string
	kind         schemaColumnKind
	nullable     bool
	defaultValue any
	defaultSet   bool
	primaryKey   bool
}

type ColumnKind uint8
type schemaColumnKind = ColumnKind

const (
	IdentifierColumn ColumnKind = iota
	IndexedTextColumn
	DocumentColumn
	PlainTextColumn
	IntegerColumn
	BigIntegerColumn
	BooleanColumn
)

const (
	identifierColumn  = IdentifierColumn
	indexedTextColumn = IndexedTextColumn
	documentColumn    = DocumentColumn
	plainTextColumn   = PlainTextColumn
	integerColumn     = IntegerColumn
	bigIntegerColumn  = BigIntegerColumn
	booleanColumn     = BooleanColumn
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

func renderBaseSchema(profile Profile, indexPrefix string, dialect modulehost.Dialect, application *ApplicationScope) ([]string, error) {
	return renderSchema(profile, indexPrefix, dialect, application, baseSchemaTables, baseSchemaIndexes)
}

func renderSchema(profile Profile, indexPrefix string, dialect modulehost.Dialect, application *ApplicationScope, tables []schemaTable, indexes []schemaIndex) ([]string, error) {
	statements := make([]string, 0, len(tables)+len(indexes))
	for _, table := range tables {
		columns := append([]schemaColumn(nil), table.columns...)
		if application != nil {
			columns = append([]schemaColumn{
				defaulted("application_key", identifierColumn, application.ApplicationKey),
			}, columns...)
		}
		definitions := make([]ormschema.ColumnDefinition, len(columns))
		primaryColumns := []string{}
		for index, column := range columns {
			definition, err := profile.SchemaColumn(column.name, column.kind)
			if err != nil {
				return nil, err
			}
			if !column.nullable {
				definition = definition.NotNull()
			}
			if column.defaultSet {
				definition = definition.DefaultValue(column.defaultValue)
			}
			if column.primaryKey {
				primaryColumns = append(primaryColumns, column.name)
			}
			definitions[index] = definition
		}
		create := ormschema.NewTable(dialect, table.name).Columns(definitions...)
		if len(primaryColumns) > 0 {
			create.PrimaryKey(primaryColumns...)
		}
		statement, _, err := create.Build()
		if err != nil {
			return nil, err
		}
		statements = append(statements, statement)
	}
	for _, index := range indexes {
		create := ormschema.NewIndex(dialect, indexPrefix+index.name, index.table).Columns(index.columns...)
		if index.unique {
			create.Unique()
		}
		statement, _, err := create.Build()
		if err != nil {
			return nil, err
		}
		statements = append(statements, statement)
	}
	return statements, nil
}
