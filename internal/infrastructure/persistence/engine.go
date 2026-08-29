package persistence

import (
	"fmt"

	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"
	mysqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/mysql"
	postgresstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/postgres"
	storeschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/schema"
	sqlitestore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/sqlite"
)

type databaseEngine interface {
	storeschema.Profile
	base.MigrationLocker
}

var databaseEngines = map[Driver]databaseEngine{
	SQLite:   sqlitestore.NewEngine(),
	Postgres: postgresstore.NewEngine(),
	MySQL:    mysqlstore.NewEngine(),
}

func databaseEngineFor(driver Driver) (databaseEngine, error) {
	engine := databaseEngines[driver]
	if engine == nil {
		return nil, fmt.Errorf("notification database driver %q is unsupported", driver)
	}
	return engine, nil
}
