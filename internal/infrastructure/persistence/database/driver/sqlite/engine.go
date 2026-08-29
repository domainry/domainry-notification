package sqlite

import (
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/base"
	sqlitemigration "github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/driver/sqlite/migration"
	sqliteschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/driver/sqlite/schema"
	storeschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/schema"
	ormdialect "github.com/domainry/domainry-orm/dialect"
)

type Engine struct {
	storeschema.Profile
	base.MigrationLocker
	dialect ormdialect.Dialect
}

func NewEngine() Engine {
	dialect, _ := ormdialect.New(ormdialect.SQLite)
	return Engine{Profile: sqliteschema.Profile{}, MigrationLocker: sqlitemigration.Profile{}, dialect: dialect}
}

func (engine Engine) Renderer(schema, prefix string) (ormdialect.Renderer, error) {
	return engine.dialect.WithNamespace(schema, prefix)
}
