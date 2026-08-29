package templatestore_test

import (
	"database/sql"
	"testing"

	sqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/testkit"
)

func migratedStore(t *testing.T) (*sql.DB, *sqlstore.Store) { return testkit.OpenMigrated(t) }
