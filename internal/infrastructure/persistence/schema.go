package persistence

import (
	"fmt"

	mysqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/mysql"
	postgresstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/postgres"
	storeschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/schema"
	sqlitestore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/sqlite"
	ormdialect "github.com/domainry/domainry-orm/dialect"
)

type Driver = ormdialect.Name

const (
	SQLite   = ormdialect.SQLite
	Postgres = ormdialect.Postgres
	MySQL    = ormdialect.MySQL
)

type ApplicationScope = storeschema.ApplicationScope
type SchemaMigration = storeschema.SchemaMigration
type SchemaBaseline = storeschema.SchemaBaseline
type SchemaBaselineTable = storeschema.SchemaBaselineTable
type SchemaBaselineColumn = storeschema.SchemaBaselineColumn
type SchemaBaselineIndex = storeschema.SchemaBaselineIndex

var schemaProfiles = map[Driver]storeschema.Profile{
	SQLite: sqlitestore.SchemaProfile{}, Postgres: postgresstore.SchemaProfile{}, MySQL: mysqlstore.SchemaProfile{},
}

func schemaProfile(driver Driver) (storeschema.Profile, error) {
	profile := schemaProfiles[driver]
	if profile == nil {
		return nil, fmt.Errorf("notification database driver %q is unsupported", driver)
	}
	return profile, nil
}

func SchemaMigrations(driver Driver, schemaName, tablePrefix string) ([]SchemaMigration, error) {
	profile, err := schemaProfile(driver)
	if err != nil {
		return nil, err
	}
	renderer, err := ormdialect.ParseRenderer(string(driver), schemaName, tablePrefix)
	if err != nil {
		return nil, err
	}
	return storeschema.SchemaMigrations(profile, renderer, tablePrefix)
}

func ModuleSchemaBaseline(driver Driver, tablePrefix string) (SchemaBaseline, error) {
	profile, err := schemaProfile(driver)
	if err != nil {
		return SchemaBaseline{}, err
	}
	return storeschema.ModuleSchemaBaseline(profile, tablePrefix)
}

func ApplicationSchemaMigrations(driver Driver, schemaName, tablePrefix string, scope ApplicationScope) ([]SchemaMigration, error) {
	profile, err := schemaProfile(driver)
	if err != nil {
		return nil, err
	}
	renderer, err := ormdialect.ParseRenderer(string(driver), schemaName, tablePrefix)
	if err != nil {
		return nil, err
	}
	return storeschema.ApplicationSchemaMigrations(profile, renderer, tablePrefix, scope)
}

var SchemaOwnership = storeschema.SchemaOwnership
var OwnedTables = storeschema.OwnedTables
