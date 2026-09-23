package lifecyclestore_test

import (
	"database/sql"
	"testing"

	sqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/artifactkernel"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/testkit"
)

func migratedStore(t *testing.T) (*sql.DB, *sqlstore.Store) { return testkit.OpenMigrated(t) }

func migratedStoreWithArtifactContent(t *testing.T) (*sql.DB, *sqlstore.Store, *artifactkernel.ContentFiles) {
	return testkit.OpenMigratedWithArtifactContent(t)
}
