package module

import (
	"context"
	"errors"
	"fmt"
	"strings"

	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/contract"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

type moduleTransactions struct{ binding *binding }

func (b *binding) ModuleTransactions() modulehost.TransactionalPublisher {
	return moduleTransactions{binding: b}
}

func (m moduleTransactions) CompileIntent(value contract.NotificationIntent) (contract.NotificationEvent, error) {
	if m.binding == nil || m.binding.compiler == nil {
		return contract.NotificationEvent{}, fmt.Errorf("notification Module compiler is unavailable")
	}
	if err := value.Validate(); err != nil {
		return contract.NotificationEvent{}, err
	}
	intent, err := convert[inbox.Intent](value)
	if err != nil {
		return contract.NotificationEvent{}, err
	}
	event, err := m.binding.compiler.Compile(intent)
	if err != nil {
		return contract.NotificationEvent{}, moduleError(err)
	}
	return convert[contract.NotificationEvent](event)
}

func moduleError(err error) error {
	if err == nil {
		return nil
	}
	var domainError *notification.Error
	if !errors.As(err, &domainError) {
		return err
	}
	status := 500
	switch domainError.Kind {
	case notification.ErrorInvalid:
		status = 400
	case notification.ErrorNotFound:
		status = 404
	case notification.ErrorConflict:
		status = 409
	case notification.ErrorForbidden:
		status = 403
	case notification.ErrorUnavailable:
		status = 503
	}
	return &notificationsdk.Error{StatusCode: status, Code: domainError.Code, Cause: err, Retryable: domainError.Kind == notification.ErrorUnavailable}
}

func (m moduleTransactions) InsertEvent(ctx context.Context, executor modulehost.Executor, value contract.NotificationEvent) error {
	if m.binding == nil || m.binding.store == nil || executor == nil {
		return fmt.Errorf("notification Module transaction boundary is unavailable")
	}
	event, err := convert[inbox.Event](value)
	if err != nil {
		return err
	}
	return m.binding.store.InsertEvent(ctx, executor, event)
}

func (m moduleTransactions) EventCommitted(ctx context.Context, identity modulehost.EventIdentity) (bool, error) {
	if m.binding == nil || m.binding.store == nil {
		return false, fmt.Errorf("notification Module transaction boundary is unavailable")
	}
	workspaceID, err := notification.NewWorkspaceID(identity.WorkspaceID)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(identity.Source) == "" || strings.TrimSpace(identity.SourceEventID) == "" {
		return false, fmt.Errorf("notification event source identity is incomplete")
	}
	return m.binding.store.EventCommitted(ctx, workspaceID, strings.TrimSpace(identity.Source), strings.TrimSpace(identity.SourceEventID))
}

var _ modulehost.TransactionalBinding = (*binding)(nil)
var _ modulehost.TransactionalPublisher = moduleTransactions{}
