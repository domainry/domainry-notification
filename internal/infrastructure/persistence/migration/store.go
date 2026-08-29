package migrationstore

import (
	"context"
	"strings"

	"github.com/domainry/domainry-notification-sdk/modulehost"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-orm/sqlhost"
)

type WorkspaceScope interface {
	Context(context.Context, notification.WorkspaceID) context.Context
}
type Config struct {
	Database       sqlhost.Database
	Dialect        modulehost.Dialect
	WorkspaceScope WorkspaceScope
}
type Store struct {
	database       sqlhost.Database
	dialect        modulehost.Dialect
	workspaceScope WorkspaceScope
}

func New(config Config) *Store {
	return &Store{database: config.Database, dialect: config.Dialect, workspaceScope: config.WorkspaceScope}
}
func (s *Store) columns(columns []string) string {
	quoted := make([]string, len(columns))
	for i, column := range columns {
		quoted[i] = s.dialect.Identifier(column)
	}
	return strings.Join(quoted, ", ")
}
