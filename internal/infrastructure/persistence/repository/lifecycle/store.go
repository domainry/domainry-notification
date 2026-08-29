package lifecyclestore

import (
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/base"
)

type Config struct {
	SQLStore *base.SQLStore
}
type Store struct {
	*base.SQLStore
}

func New(config Config) *Store {
	if config.SQLStore == nil {
		config.SQLStore = &base.SQLStore{}
	}
	return &Store{SQLStore: config.SQLStore}
}
