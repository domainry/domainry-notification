package persistence

import (
	storeschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/schema"
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

func schemaProfile(driver Driver) (DatabaseEngine, error) {
	return databaseEngineFor(driver)
}

func SchemaMigrations(driver Driver, schemaName, tablePrefix string) ([]SchemaMigration, error) {
	profile, err := schemaProfile(driver)
	if err != nil {
		return nil, err
	}
	renderer, err := profile.Renderer(schemaName, tablePrefix)
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
	renderer, err := profile.Renderer(schemaName, tablePrefix)
	if err != nil {
		return nil, err
	}
	return storeschema.ApplicationSchemaMigrations(profile, renderer, tablePrefix, scope)
}

var SchemaOwnership = storeschema.SchemaOwnership
var OwnedTables = storeschema.OwnedTables
