package mysql

import (
	"fmt"

	storeschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/schema"
	ormbuilder "github.com/domainry/domainry-orm/builder"
)

type SchemaProfile struct{}

func (SchemaProfile) ColumnType(kind storeschema.ColumnKind) (string, error) {
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

func (SchemaProfile) SchemaColumn(name string, kind storeschema.ColumnKind) (ormbuilder.SchemaColumn, error) {
	var columnType ormbuilder.ColumnType
	switch kind {
	case storeschema.IdentifierColumn:
		columnType = ormbuilder.TextKeyType(191)
	case storeschema.IndexedTextColumn:
		return ormbuilder.DefineColumn(name, ormbuilder.TextKeyType(191)).CharacterSet("ascii").Collation("ascii_bin"), nil
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
		return ormbuilder.SchemaColumn{}, fmt.Errorf("notification MySQL column kind %d is unsupported", kind)
	}
	return ormbuilder.DefineColumn(name, columnType), nil
}
