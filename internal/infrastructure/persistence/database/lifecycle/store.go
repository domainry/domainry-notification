package lifecyclestore

import (
	"github.com/domainry/domainry-notification-sdk/modulehost"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"
)

type Config struct {
	SQLStore *base.SQLStore
	Archives modulehost.RetentionArchiveStore
}
type Store struct {
	*base.SQLStore
	archives modulehost.RetentionArchiveStore
}

func New(config Config) *Store {
	if config.SQLStore == nil {
		config.SQLStore = &base.SQLStore{}
	}
	return &Store{SQLStore: config.SQLStore, archives: config.Archives}
}
