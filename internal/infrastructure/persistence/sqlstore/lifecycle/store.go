package lifecyclestore

import (
	"github.com/domainry/domainry-notification-sdk/modulehost"
	"github.com/domainry/domainry-orm/sqlhost"
)

type Config struct {
	Database sqlhost.Database
	Dialect  modulehost.Dialect
}
type Store struct {
	database sqlhost.Database
	dialect  modulehost.Dialect
}

func New(config Config) *Store { return &Store{database: config.Database, dialect: config.Dialect} }
