package sqlstore

import (
	"fmt"
	"strings"

	ormdialect "github.com/domainry/domainry-orm/dialect"
)

type Driver string

const (
	SQLite   Driver = "sqlite"
	Postgres Driver = "postgres"
	MySQL    Driver = "mysql"
)

type standardDialect struct {
	dialect ormdialect.Dialect
	schema  string
	prefix  string
}

// NewDialect constructs the standalone SQL dialect used when a host does not
// adapt an existing database abstraction. Schema and prefix must be static
// trusted identifiers; dynamic domain values never enter SQL identifiers.
func NewDialect(driver Driver, schema, tablePrefix string) (Dialect, error) {
	dialect, err := ormdialect.Parse(string(driver))
	if err != nil || driver != SQLite && driver != Postgres && driver != MySQL {
		return nil, fmt.Errorf("notification SQL driver %q is unsupported", driver)
	}
	schema, tablePrefix = strings.TrimSpace(schema), strings.TrimSpace(tablePrefix)
	if schema != "" && !ormdialect.ValidIdentifier(schema) {
		return nil, fmt.Errorf("notification SQL schema is invalid")
	}
	if tablePrefix != "" && !ormdialect.ValidIdentifier(tablePrefix) {
		return nil, fmt.Errorf("notification SQL table prefix is invalid")
	}
	return standardDialect{dialect: dialect, schema: schema, prefix: tablePrefix}, nil
}

func (d standardDialect) Identifier(value string) string {
	return d.dialect.Identifier(value)
}

func (d standardDialect) Table(value string) string {
	table := d.Identifier(d.prefix + value)
	if d.schema != "" {
		return d.Identifier(d.schema) + "." + table
	}
	return table
}

func (d standardDialect) Placeholder(position int) string {
	return d.dialect.Placeholder(position)
}

func (d standardDialect) Insert(table string, columns []string) string {
	quoted, placeholders := make([]string, len(columns)), d.dialect.Placeholders(len(columns))
	for index, column := range columns {
		quoted[index] = d.Identifier(column)
	}
	return "INSERT INTO " + d.Table(table) + " (" + strings.Join(quoted, ", ") + ") VALUES (" + strings.Join(placeholders, ", ") + ")"
}
