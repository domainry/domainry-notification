package templatestore

import (
	metadatasdk "github.com/domainry/domainry-metadata-sdk"
	notificationmodulehost "github.com/domainry/domainry-notification-sdk/modulehost"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"
)

type Config struct {
	SQLStore        *base.SQLStore
	Clock           notification.Clock
	WorkspaceID     notification.WorkspaceID
	DefinitionStore metadatasdk.DefinitionStore
	OperationStore  notificationmodulehost.ManagedOperationStore
}
type Store struct {
	*base.SQLStore
	clock       notification.Clock
	workspaceID notification.WorkspaceID
	definitions metadatasdk.DefinitionStore
	operations  notificationmodulehost.ManagedOperationStore
}

func New(config Config) *Store {
	return &Store{SQLStore: config.SQLStore, clock: config.Clock, workspaceID: config.WorkspaceID, definitions: config.DefinitionStore, operations: config.OperationStore}
}

type scanner interface{ Scan(...any) error }

func (s *Store) columns(columns []string) string { return s.Columns(columns) }
