package sqlstore

import (
	storeschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/sqlstore/schema"
)

type Driver = storeschema.Driver

const (
	SQLite   = storeschema.SQLite
	Postgres = storeschema.Postgres
	MySQL    = storeschema.MySQL
)

type ApplicationScope = storeschema.ApplicationScope
type SchemaMigration = storeschema.SchemaMigration
type SchemaBaseline = storeschema.SchemaBaseline
type SchemaBaselineTable = storeschema.SchemaBaselineTable
type SchemaBaselineColumn = storeschema.SchemaBaselineColumn
type SchemaBaselineIndex = storeschema.SchemaBaselineIndex

var SchemaMigrations = storeschema.SchemaMigrations
var ModuleSchemaBaseline = storeschema.ModuleSchemaBaseline
var ApplicationSchemaMigrations = storeschema.ApplicationSchemaMigrations
var SchemaOwnership = storeschema.SchemaOwnership
var OwnedTables = storeschema.OwnedTables
