package persistence

import (
	"fmt"

	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"
	storeschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/schema"
	mysqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/mysql"
	postgresstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/postgres"
	sqlitestore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/sqlite"
	ormdialect "github.com/domainry/domainry-orm/dialect"
)

type DatabaseEngine interface {
	storeschema.Profile
	base.MigrationLocker
	Renderer(string, string) (ormdialect.Renderer, error)
}

var databaseEngineFactories = map[Driver]func() DatabaseEngine{
	SQLite:   func() DatabaseEngine { return sqlitestore.NewEngine() },
	Postgres: func() DatabaseEngine { return postgresstore.NewEngine() },
	MySQL:    func() DatabaseEngine { return mysqlstore.NewEngine() },
}

func NewEngine(driver Driver) (DatabaseEngine, error) {
	factory := databaseEngineFactories[driver]
	if factory == nil {
		return nil, fmt.Errorf("notification database driver %q is unsupported", driver)
	}
	return factory(), nil
}
