package inboxstore

import (
	"context"
	"strings"

	"github.com/domainry/domainry-notification-sdk/modulehost"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/shared"
	"github.com/domainry/domainry-orm/sqlhost"
)

// WorkspaceScope attaches the tenant boundary required by the host database.
type WorkspaceScope interface {
	Context(context.Context, notification.WorkspaceID) context.Context
}

type Config struct {
	Database       sqlhost.Database
	Dialect        modulehost.Dialect
	WorkspaceScope WorkspaceScope
	Clock          notification.Clock
}

type Store struct {
	database       sqlhost.Database
	dialect        modulehost.Dialect
	workspaceScope WorkspaceScope
	clock          notification.Clock
}

var ErrMutationConflict = shared.ErrMutationConflict

func New(config Config) *Store {
	return &Store{
		database:       config.Database,
		dialect:        config.Dialect,
		workspaceScope: config.WorkspaceScope,
		clock:          config.Clock,
	}
}

type scanner interface{ Scan(...any) error }

func (s *Store) columns(columns []string) string {
	quoted := make([]string, len(columns))
	for index, column := range columns {
		quoted[index] = s.dialect.Identifier(column)
	}
	return strings.Join(quoted, ", ")
}
