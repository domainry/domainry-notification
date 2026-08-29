package deliverystore

import (
	"context"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/shared"
	"github.com/domainry/domainry-orm/sqlhost"
	"regexp"
)

type WorkspaceScope interface {
	Context(context.Context, notification.WorkspaceID) context.Context
}

type QueueScopeIndex interface {
	Workspaces(context.Context, sqlhost.Queryer, notification.WorkKind, int) ([]notification.WorkspaceID, error)
}

type Config struct {
	SQLStore       *base.SQLStore
	WorkspaceScope WorkspaceScope
	QueueScopes    QueueScopeIndex
}

type Store struct {
	*base.SQLStore
	workspaceScope WorkspaceScope
	queueScopes    QueueScopeIndex
}

var (
	ErrLeaseLost       = shared.ErrLeaseLost
	failureCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,159}$`)
)

var channelPlanColumns = []string{
	"id", "workspace_id", "event_id", "channel", "status", "payload_json", "attempt_count", "next_attempt_at",
	"last_error_code", "outbox_message_id", "lease_owner", "lease_expires_at", "fencing_token", "created_at", "updated_at",
}

func New(config Config) *Store {
	return &Store{SQLStore: config.SQLStore, workspaceScope: config.WorkspaceScope, queueScopes: config.QueueScopes}
}

type scanner interface{ Scan(...any) error }

func (s *Store) columns(columns []string) string { return s.Columns(columns) }

func workspaceScanLimit(limit int) int { return min(256, max(32, limit*2)) }
