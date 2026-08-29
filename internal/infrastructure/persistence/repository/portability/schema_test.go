package portabilitystore

import (
	"fmt"

	storeschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/schema"
	ormbuilder "github.com/domainry/domainry-orm/builder"
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

func (portableTestProfile) SchemaColumn(name string, kind storeschema.ColumnKind) (ormbuilder.SchemaColumn, error) {
	var columnType ormbuilder.ColumnType
	switch kind {
	case storeschema.IdentifierColumn, storeschema.IndexedTextColumn:
		columnType = ormbuilder.TextKeyType(191)
	case storeschema.DocumentColumn:
		columnType = ormbuilder.LongTextType()
	case storeschema.PlainTextColumn:
		columnType = ormbuilder.TextType()
	case storeschema.IntegerColumn:
		columnType = ormbuilder.IntegerType()
	case storeschema.BigIntegerColumn:
		columnType = ormbuilder.BigIntType()
	case storeschema.BooleanColumn:
		columnType = ormbuilder.BooleanType()
	default:
		return ormbuilder.SchemaColumn{}, fmt.Errorf("portable test column kind %d is unsupported", kind)
	}
	return ormbuilder.DefineColumn(name, columnType), nil
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
