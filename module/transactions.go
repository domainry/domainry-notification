package module

import (
	"context"
	"fmt"

	"github.com/domainry/domainry-notification-sdk/contract"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	"github.com/domainry/domainry-notification/inbox"
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
		return contract.NotificationEvent{}, err
	}
	return convert[contract.NotificationEvent](event)
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

var _ modulehost.TransactionalBinding = (*binding)(nil)
var _ modulehost.TransactionalPublisher = moduleTransactions{}
