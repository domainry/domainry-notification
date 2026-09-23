package schema

import (
	"fmt"
	"strings"

	sharedoperation "github.com/domainry/domainry-foundation/operation"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	ormmigration "github.com/domainry/domainry-orm/migration"
	ormschema "github.com/domainry/domainry-orm/schema"
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

// SchemaMigrations renders the canonical final Notification schema for one
// physical naming configuration.
func SchemaMigrations(profile Profile, dialect modulehost.Dialect, tablePrefix string) ([]SchemaMigration, error) {
	statements, err := renderBaseSchema(profile, tablePrefix, dialect, nil)
	if err != nil {
		return nil, err
	}
	return []SchemaMigration{{Version: 1, Name: "create_notification_schema", Statements: statements}}, nil
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

func SharedOperationSchemaMigrations(_ Profile, dialect modulehost.Dialect) ([]SchemaMigration, error) {
	return sharedoperation.SchemaMigrationsForDialect(dialect)
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
		primaryColumns = workspaceFirstPrimaryKey(primaryColumns)
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

func workspaceFirstPrimaryKey(columns []string) []string {
	for index, column := range columns {
		if column != "workspace_id" || index == 0 {
			continue
		}
		ordered := make([]string, 0, len(columns))
		ordered = append(ordered, "workspace_id")
		ordered = append(ordered, columns[:index]...)
		ordered = append(ordered, columns[index+1:]...)
		return ordered
	}
	return columns
}
