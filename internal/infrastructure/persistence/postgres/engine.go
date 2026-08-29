package postgres

import (
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"
	postgresmigration "github.com/domainry/domainry-notification/internal/infrastructure/persistence/postgres/migration"
	postgresschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/postgres/schema"
	storeschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/schema"
	ormdialect "github.com/domainry/domainry-orm/dialect"
)

type Engine struct {
	storeschema.Profile
	base.MigrationLocker
	dialect ormdialect.Dialect
}

func NewEngine() Engine {
	dialect, _ := ormdialect.New(ormdialect.Postgres)
	return Engine{Profile: postgresschema.Profile{}, MigrationLocker: postgresmigration.Profile{}, dialect: dialect}
}

func (engine Engine) Renderer(schema, prefix string) (ormdialect.Renderer, error) {
	return engine.dialect.WithNamespace(schema, prefix)
}
