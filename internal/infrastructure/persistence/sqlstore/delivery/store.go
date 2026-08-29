package deliverystore

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/domainry/domainry-notification-sdk/modulehost"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-orm/sqlhost"
)

type WorkspaceScope interface {
	Context(context.Context, notification.WorkspaceID) context.Context
}

type QueueScopeIndex interface {
	Workspaces(context.Context, sqlhost.Queryer, notification.WorkKind, int) ([]notification.WorkspaceID, error)
}

type Config struct {
	Database       sqlhost.Database
	Dialect        modulehost.Dialect
	WorkspaceScope WorkspaceScope
	QueueScopes    QueueScopeIndex
}

type Store struct {
	database       sqlhost.Database
	dialect        modulehost.Dialect
	workspaceScope WorkspaceScope
	queueScopes    QueueScopeIndex
}

var (
	ErrLeaseLost       = errors.New("notification durable-work lease was lost")
	failureCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,159}$`)
)

var channelPlanColumns = []string{
	"id", "workspace_id", "event_id", "channel", "status", "payload_json", "attempt_count", "next_attempt_at",
	"last_error_code", "outbox_message_id", "lease_owner", "lease_expires_at", "fencing_token", "created_at", "updated_at",
}

func New(config Config) *Store {
	return &Store{database: config.Database, dialect: config.Dialect, workspaceScope: config.WorkspaceScope, queueScopes: config.QueueScopes}
}

type scanner interface{ Scan(...any) error }

func (s *Store) columns(columns []string) string {
	quoted := make([]string, len(columns))
	for index, column := range columns {
		quoted[index] = s.dialect.Identifier(column)
	}
	return strings.Join(quoted, ", ")
}

func workspaceScanLimit(limit int) int { return min(256, max(32, limit*2)) }
