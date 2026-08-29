package mysql

import (
	"fmt"

	storeschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/schema"
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
