package portabilitystore

import (
	"fmt"
	ormschema "github.com/domainry/domainry-orm/schema"

	storeschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/schema"
	ormdialect "github.com/domainry/domainry-orm/dialect"
)

const SQLite = "sqlite"

type ApplicationScope = storeschema.ApplicationScope
type SchemaMigration = storeschema.SchemaMigration

type portableTestProfile struct{}

func (portableTestProfile) ColumnType(kind storeschema.ColumnKind) (string, error) {
	switch kind {
	case storeschema.IdentifierColumn, storeschema.IndexedTextColumn, storeschema.DocumentColumn, storeschema.PlainTextColumn:
		return "TEXT", nil
	case storeschema.IntegerColumn:
		return "INTEGER", nil
	case storeschema.BigIntegerColumn:
		return "BIGINT", nil
	case storeschema.BooleanColumn:
		return "BOOLEAN", nil
	default:
		return "", fmt.Errorf("portable test column kind %d is unsupported", kind)
	}
}

func (portableTestProfile) SchemaColumn(name string, kind storeschema.ColumnKind) (ormschema.ColumnDefinition, error) {
	var columnType ormschema.ColumnType
	switch kind {
	case storeschema.IdentifierColumn, storeschema.IndexedTextColumn:
		columnType = ormschema.TextKey(191)
	case storeschema.DocumentColumn:
		columnType = ormschema.LongText()
	case storeschema.PlainTextColumn:
		columnType = ormschema.Text()
	case storeschema.IntegerColumn:
		columnType = ormschema.Integer()
	case storeschema.BigIntegerColumn:
		columnType = ormschema.BigInt()
	case storeschema.BooleanColumn:
		columnType = ormschema.Boolean()
	default:
		return ormschema.ColumnDefinition{}, fmt.Errorf("portable test column kind %d is unsupported", kind)
	}
	return ormschema.Column(name, columnType), nil
}

func SchemaMigrations(_ string, schemaName, prefix string) ([]SchemaMigration, error) {
	renderer, err := ormdialect.ParseRenderer("sqlite", schemaName, prefix)
	if err != nil {
		return nil, err
	}
	return storeschema.SchemaMigrations(portableTestProfile{}, renderer, prefix)
}

func ApplicationSchemaMigrations(_ string, schemaName, prefix string, scope ApplicationScope) ([]SchemaMigration, error) {
	renderer, err := ormdialect.ParseRenderer("sqlite", schemaName, prefix)
	if err != nil {
		return nil, err
	}
	return storeschema.ApplicationSchemaMigrations(portableTestProfile{}, renderer, prefix, scope)
}
