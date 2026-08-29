package lifecyclestore_test

import (
	"database/sql"
	"testing"

	sqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/testing"
)

func migratedStore(t *testing.T) (*sql.DB, *sqlstore.Store) { return testkit.OpenMigrated(t) }
