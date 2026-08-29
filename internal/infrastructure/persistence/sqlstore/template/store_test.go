package templatestore_test

import (
	"database/sql"
	"testing"

	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/sqlstore"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/sqlstore/testkit"
)

func migratedStore(t *testing.T) (*sql.DB, *sqlstore.Store) { return testkit.OpenMigrated(t) }
