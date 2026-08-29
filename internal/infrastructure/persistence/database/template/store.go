package templatestore

import (
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"
)

type Config struct {
	SQLStore *base.SQLStore
	Clock    notification.Clock
}
type Store struct {
	*base.SQLStore
	clock notification.Clock
}

func New(config Config) *Store {
	return &Store{SQLStore: config.SQLStore, clock: config.Clock}
}

type scanner interface{ Scan(...any) error }

func (s *Store) columns(columns []string) string { return s.Columns(columns) }
