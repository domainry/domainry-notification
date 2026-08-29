package mysql

import (
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"
	mysqlmigration "github.com/domainry/domainry-notification/internal/infrastructure/persistence/mysql/migration"
	mysqlschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/mysql/schema"
	storeschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/schema"
	ormdialect "github.com/domainry/domainry-orm/dialect"
)

type Engine struct {
	storeschema.Profile
	base.MigrationLocker
	dialect ormdialect.Dialect
}

func NewEngine() Engine {
	dialect, _ := ormdialect.New(ormdialect.MySQL)
	return Engine{Profile: mysqlschema.Profile{}, MigrationLocker: mysqlmigration.Profile{}, dialect: dialect}
}

func (engine Engine) Renderer(schema, prefix string) (ormdialect.Renderer, error) {
	return engine.dialect.WithNamespace(schema, prefix)
}
