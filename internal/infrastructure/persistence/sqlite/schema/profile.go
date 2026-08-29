package schema

import (
	"fmt"

	storeschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/schema"
	ormbuilder "github.com/domainry/domainry-orm/builder"
)

type Profile struct{}

func (Profile) ColumnType(kind storeschema.ColumnKind) (string, error) {
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
		return "", fmt.Errorf("notification SQLite column kind %d is unsupported", kind)
	}
}

func (Profile) SchemaColumn(name string, kind storeschema.ColumnKind) (ormbuilder.SchemaColumn, error) {
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
		return ormbuilder.SchemaColumn{}, fmt.Errorf("notification SQLite column kind %d is unsupported", kind)
	}
	return ormbuilder.DefineColumn(name, columnType), nil
}
