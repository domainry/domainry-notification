package schema

import (
	"fmt"
	ormschema "github.com/domainry/domainry-orm/schema"

	storeschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/schema"
)

type Profile struct{}

func (Profile) ColumnType(kind storeschema.ColumnKind) (string, error) {
	switch kind {
	case storeschema.IdentifierColumn:
		return "VARCHAR(191)", nil
	case storeschema.IndexedTextColumn:
		return "VARCHAR(191) CHARACTER SET ascii COLLATE ascii_bin", nil
	case storeschema.DocumentColumn:
		return "LONGTEXT", nil
	case storeschema.PlainTextColumn:
		return "TEXT", nil
	case storeschema.IntegerColumn:
		return "INTEGER", nil
	case storeschema.BigIntegerColumn:
		return "BIGINT", nil
	case storeschema.BooleanColumn:
		return "BOOLEAN", nil
	default:
		return "", fmt.Errorf("notification MySQL column kind %d is unsupported", kind)
	}
}

func (Profile) SchemaColumn(name string, kind storeschema.ColumnKind) (ormschema.ColumnDefinition, error) {
	var columnType ormschema.ColumnType
	switch kind {
	case storeschema.IdentifierColumn:
		columnType = ormschema.TextKey(191)
	case storeschema.IndexedTextColumn:
		return ormschema.Column(name, ormschema.TextKey(191)).CharacterSet("ascii").Collation("ascii_bin"), nil
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
		return ormschema.ColumnDefinition{}, fmt.Errorf("notification MySQL column kind %d is unsupported", kind)
	}
	return ormschema.Column(name, columnType), nil
}
