package migrationstore

import storeschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/schema"

const SQLite = storeschema.SQLite

type ApplicationScope = storeschema.ApplicationScope
type SchemaMigration = storeschema.SchemaMigration

var SchemaMigrations = storeschema.SchemaMigrations
var ApplicationSchemaMigrations = storeschema.ApplicationSchemaMigrations
