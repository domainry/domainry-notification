package sqlite

import (
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"
	storeschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/schema"
	sqlitemigration "github.com/domainry/domainry-notification/internal/infrastructure/persistence/sqlite/migration"
	sqliteschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/sqlite/schema"
)

type Engine struct {
	storeschema.Profile
	base.MigrationLocker
}

func NewEngine() Engine {
	return Engine{Profile: sqliteschema.Profile{}, MigrationLocker: sqlitemigration.Profile{}}
}
