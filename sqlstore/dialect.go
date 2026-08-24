package sqlstore

import (
	"fmt"
	"regexp"
	"strings"
)

type Driver string

const (
	SQLite   Driver = "sqlite"
	Postgres Driver = "postgres"
	MySQL    Driver = "mysql"
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type standardDialect struct {
	driver Driver
	schema string
	prefix string
}

// NewDialect constructs the standalone SQL dialect used when a host does not
// adapt an existing database abstraction. Schema and prefix must be static
// trusted identifiers; dynamic domain values never enter SQL identifiers.
func NewDialect(driver Driver, schema, tablePrefix string) (Dialect, error) {
	if driver != SQLite && driver != Postgres && driver != MySQL {
		return nil, fmt.Errorf("notification SQL driver %q is unsupported", driver)
	}
	schema, tablePrefix = strings.TrimSpace(schema), strings.TrimSpace(tablePrefix)
	if schema != "" && !identifierPattern.MatchString(schema) {
		return nil, fmt.Errorf("notification SQL schema is invalid")
	}
	if tablePrefix != "" && !identifierPattern.MatchString(tablePrefix) {
		return nil, fmt.Errorf("notification SQL table prefix is invalid")
	}
	return standardDialect{driver: driver, schema: schema, prefix: tablePrefix}, nil
}

func (d standardDialect) Identifier(value string) string {
	if !identifierPattern.MatchString(value) {
		panic("notification sqlstore: unsafe identifier")
	}
	quote := `"`
	if d.driver == MySQL {
		quote = "`"
	}
	return quote + value + quote
}

func (d standardDialect) Table(value string) string {
	table := d.Identifier(d.prefix + value)
	if d.schema != "" {
		return d.Identifier(d.schema) + "." + table
	}
	return table
}

func (d standardDialect) Placeholder(position int) string {
	if d.driver == Postgres {
		return fmt.Sprintf("$%d", position)
	}
	return "?"
}

func (d standardDialect) Insert(table string, columns []string) string {
	quoted, placeholders := make([]string, len(columns)), make([]string, len(columns))
	for index, column := range columns {
		quoted[index], placeholders[index] = d.Identifier(column), d.Placeholder(index+1)
	}
	return "INSERT INTO " + d.Table(table) + " (" + strings.Join(quoted, ", ") + ") VALUES (" + strings.Join(placeholders, ", ") + ")"
}
