package persistence

import (
	"fmt"

	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"
	mysqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/mysql"
	postgresstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/postgres"
	storeschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/schema"
	sqlitestore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/sqlite"
	ormdialect "github.com/domainry/domainry-orm/dialect"
)

type DatabaseEngine interface {
	storeschema.Profile
	base.MigrationLocker
	Renderer(string, string) (ormdialect.Renderer, error)
}

var databaseEngines = map[Driver]DatabaseEngine{
	SQLite:   sqlitestore.NewEngine(),
	Postgres: postgresstore.NewEngine(),
	MySQL:    mysqlstore.NewEngine(),
}

func NewEngine(driver Driver) (DatabaseEngine, error) {
	engine := databaseEngines[driver]
	if engine == nil {
		return nil, fmt.Errorf("notification database driver %q is unsupported", driver)
	}
	return engine, nil
}
