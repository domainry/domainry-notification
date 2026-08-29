package inboxstore

import (
	"context"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"
)

// WorkspaceScope attaches the tenant boundary required by the host database.
type WorkspaceScope interface {
	Context(context.Context, notification.WorkspaceID) context.Context
}

type Config struct {
	SQLStore       *base.SQLStore
	WorkspaceScope WorkspaceScope
	Clock          notification.Clock
}

type Store struct {
	*base.SQLStore
	workspaceScope WorkspaceScope
	clock          notification.Clock
}

func New(config Config) *Store {
	return &Store{
		SQLStore:       config.SQLStore,
		workspaceScope: config.WorkspaceScope,
		clock:          config.Clock,
	}
}

type scanner interface{ Scan(...any) error }

func (s *Store) columns(columns []string) string { return s.Columns(columns) }
