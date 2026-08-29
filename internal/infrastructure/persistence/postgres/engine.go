package postgres

import (
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"
	postgresmigration "github.com/domainry/domainry-notification/internal/infrastructure/persistence/postgres/migration"
	postgresschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/postgres/schema"
	storeschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/schema"
)

type Engine struct {
	storeschema.Profile
	base.MigrationLocker
}

func NewEngine() Engine {
	return Engine{Profile: postgresschema.Profile{}, MigrationLocker: postgresmigration.Profile{}}
}
