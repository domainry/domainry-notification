package sqlite

import (
	"fmt"

	storeschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/schema"
)

type SchemaProfile struct{}

func (SchemaProfile) ColumnType(kind storeschema.ColumnKind) (string, error) {
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
