package mysql

import (
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"
	mysqlmigration "github.com/domainry/domainry-notification/internal/infrastructure/persistence/mysql/migration"
	mysqlschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/mysql/schema"
	storeschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/schema"
)

type Engine struct {
	storeschema.Profile
	base.MigrationLocker
}

func NewEngine() Engine {
	return Engine{Profile: mysqlschema.Profile{}, MigrationLocker: mysqlmigration.Profile{}}
}
