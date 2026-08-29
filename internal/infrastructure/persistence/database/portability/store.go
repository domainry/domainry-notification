package portabilitystore

import (
	"context"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"
)

type WorkspaceScope interface {
	Context(context.Context, notification.WorkspaceID) context.Context
}
type Config struct {
	SQLStore       *base.SQLStore
	WorkspaceScope WorkspaceScope
}
type Store struct {
	*base.SQLStore
	workspaceScope WorkspaceScope
}

func New(config Config) *Store {
	return &Store{SQLStore: config.SQLStore, workspaceScope: config.WorkspaceScope}
}
func (s *Store) columns(columns []string) string { return s.Columns(columns) }
