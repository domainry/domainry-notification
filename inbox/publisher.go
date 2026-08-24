package inbox

import (
	"context"
	"fmt"

	"github.com/domainry/domainry-notification"
)

// WorkNotifier emits a best-effort acceleration hint after EventStore.Enqueue
// commits. Durable recovery must not depend on receiving the hint.
type WorkNotifier interface {
	Notify(context.Context, notification.Work)
}

type Publisher struct {
	compiler *Compiler
	events   EventStore
	notifier WorkNotifier
}

func NewPublisher(compiler *Compiler, events EventStore, notifier WorkNotifier) (*Publisher, error) {
	if compiler == nil || events == nil {
		return nil, fmt.Errorf("notification inbox publisher dependencies are required")
	}
	return &Publisher{compiler: compiler, events: events, notifier: notifier}, nil
}

func (p *Publisher) PublishIntent(ctx context.Context, intent Intent) (Event, bool, error) {
	event, err := p.compiler.Compile(intent)
	if err != nil {
		return Event{}, false, err
	}
	return p.PublishEvent(ctx, event)
}

func (p *Publisher) PublishEvent(ctx context.Context, event Event) (Event, bool, error) {
	stored, created, err := p.events.Enqueue(ctx, event)
	if err != nil {
		return Event{}, false, err
	}
	if created && p.notifier != nil {
		p.notifier.Notify(ctx, notification.Work{Kind: notification.WorkInboxEvent, WorkspaceID: stored.WorkspaceID, TaskID: stored.ID})
	}
	return stored, created, nil
}
