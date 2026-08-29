package templatestore

import (
	"strings"

	"github.com/domainry/domainry-notification-sdk/modulehost"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-orm/sqlhost"
)

type Config struct {
	Database sqlhost.Database
	Dialect  modulehost.Dialect
	Clock    notification.Clock
}
type Store struct {
	database sqlhost.Database
	dialect  modulehost.Dialect
	clock    notification.Clock
}

func New(config Config) *Store {
	return &Store{database: config.Database, dialect: config.Dialect, clock: config.Clock}
}

type scanner interface{ Scan(...any) error }

func (s *Store) columns(columns []string) string {
	quoted := make([]string, len(columns))
	for i, column := range columns {
		quoted[i] = s.dialect.Identifier(column)
	}
	return strings.Join(quoted, ", ")
}
