package persistence

import (
	"fmt"

	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"
	mysqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/mysql"
	postgresstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/postgres"
	sqlitestore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/sqlite"
)

var migrationLockers = map[Driver]base.MigrationLocker{
	SQLite: sqlitestore.MigrationLocker{}, Postgres: postgresstore.MigrationLocker{}, MySQL: mysqlstore.MigrationLocker{},
}

func MigrationLocker(driver Driver) (base.MigrationLocker, error) {
	locker := migrationLockers[driver]
	if locker == nil {
		return nil, fmt.Errorf("notification database driver %q is unsupported", driver)
	}
	return locker, nil
}
