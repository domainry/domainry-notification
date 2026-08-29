package persistence

import "github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"

func MigrationLocker(driver Driver) (base.MigrationLocker, error) {
	return databaseEngineFor(driver)
}
