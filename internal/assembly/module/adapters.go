package module

import (
	"context"
	"fmt"
	"time"

	"github.com/domainry/domainry-notification-sdk/contract"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	sdkadapter "github.com/domainry/domainry-notification/internal/adapter/notificationsdk"
	appinbox "github.com/domainry/domainry-notification/internal/application/inbox"
	apptemplate "github.com/domainry/domainry-notification/internal/application/template"
	"github.com/domainry/domainry-notification/internal/domain/delivery/service"
	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-notification/internal/domain/template/service"
	sqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence"
	"github.com/domainry/domainry-orm/sqlhost"
)

func convert[To any, From any](value From) (To, error) {
	return sdkadapter.Convert[To](value)
}

func convertSlice[To any, From any](values []From) ([]To, error) {
	return sdkadapter.ConvertSlice[To](values)
}

type workspaceScopeAdapter struct{ delegate modulehost.WorkspaceScope }

func (a workspaceScopeAdapter) Context(ctx context.Context, workspaceID notification.WorkspaceID) context.Context {
	return a.delegate.Context(ctx, workspaceID.String())
}

type queueScopeAdapter struct{ delegate modulehost.QueueScopeIndex }

func (a queueScopeAdapter) Register(ctx context.Context, executor sqlhost.Executor, kind notification.WorkKind, workspaceID notification.WorkspaceID, updatedAt string) error {
	return a.delegate.Register(ctx, executor, string(kind), workspaceID.String(), updatedAt)
}
func (a queueScopeAdapter) Workspaces(ctx context.Context, queryer sqlhost.Queryer, kind notification.WorkKind, limit int) ([]notification.WorkspaceID, error) {
	values, err := a.delegate.Workspaces(ctx, queryer, string(kind), limit)
	if err != nil {
		return nil, err
	}
	result := make([]notification.WorkspaceID, len(values))
	for i := range values {
		result[i] = notification.WorkspaceID(values[i])
	}
	return result, nil
}

type workNotifierAdapter struct{ delegate modulehost.WorkNotifier }

func (a workNotifierAdapter) Notify(ctx context.Context, work notification.Work) {
	if a.delegate != nil {
		a.delegate.Notify(ctx, modulehost.WorkLocator{Kind: string(work.Kind), WorkspaceID: work.WorkspaceID.String(), TaskID: work.TaskID})
	}
}

type recipientDirectoryAdapter struct{ delegate modulehost.RecipientDirectory }

func (a recipientDirectoryAdapter) FindRecipient(ctx context.Context, workspaceID notification.WorkspaceID, userID notification.UserID) (notification.Recipient, bool, error) {
	if a.delegate == nil {
		return notification.Recipient{}, false, nil
	}
	value, found, err := a.delegate.FindRecipient(ctx, workspaceID.String(), userID.String())
	return notification.Recipient{ID: userID, Email: value.Email, Locale: value.Locale, Timezone: value.Timezone}, found, err
}

type recipientLocaleAdapter struct{ delegate modulehost.RecipientDirectory }

func (a recipientLocaleAdapter) RecipientLocale(ctx context.Context, workspaceID notification.WorkspaceID, userID notification.UserID) (string, error) {
	value, found, err := a.delegate.FindRecipient(ctx, workspaceID.String(), userID.String())
	if err != nil || !found {
		return "", err
	}
	return value.Locale, nil
}

type audienceAdapter struct{ delegate modulehost.AudienceResolver }

func (a audienceAdapter) ResolveAudience(ctx context.Context, key string, event inbox.Event) ([]notification.UserID, error) {
	if a.delegate == nil {
		return nil, fmt.Errorf("audience resolver is unavailable")
	}
	wire, err := convert[contract.NotificationEvent](event)
	if err != nil {
		return nil, err
	}
	values, err := a.delegate.ResolveAudience(ctx, key, wire)
	if err != nil {
		return nil, err
	}
	result := make([]notification.UserID, len(values))
	for i := range values {
		result[i] = notification.UserID(values[i])
	}
	return result, nil
}

type deliveryGatewayAdapter struct{ delegate modulehost.DeliveryGateway }

func (a deliveryGatewayAdapter) Dispatch(ctx context.Context, request delivery.DispatchRequest) (delivery.DispatchReceipt, error) {
	if a.delegate == nil {
		return delivery.DispatchReceipt{}, fmt.Errorf("delivery gateway is unavailable")
	}
	rendered, err := convert[contract.RenderedNotification](request.Content)
	if err != nil {
		return delivery.DispatchReceipt{}, err
	}
	fallbacks := make([]modulehost.DeliveryFallback, len(request.Fallbacks))
	for index, fallback := range request.Fallbacks {
		content, convertErr := convert[contract.RenderedNotification](fallback.Content)
		if convertErr != nil {
			return delivery.DispatchReceipt{}, convertErr
		}
		fallbacks[index] = modulehost.DeliveryFallback{ConnectorKey: fallback.ConnectorKey, ConnectionKey: fallback.ConnectionKey, Operation: fallback.Operation, Rendered: content}
	}
	receipt, err := a.delegate.Dispatch(ctx, modulehost.DeliveryRequest{WorkspaceID: request.WorkspaceID.String(), PlanID: request.PlanID, EventID: request.EventID, Channel: request.Channel, ConnectorKey: request.ConnectorKey, ConnectionKey: request.ConnectionKey, Operation: request.Operation, DedupeKey: request.DeduplicationKey, DeliverAfter: request.Decision.DeliverAfter, CreatedAt: request.CreatedAt.UTC().Format(time.RFC3339Nano), Rendered: rendered, Fallbacks: fallbacks})
	return delivery.DispatchReceipt{MessageID: receipt.MessageID, AcceptedAt: request.CreatedAt}, err
}

var _ sqlstore.WorkspaceScope = workspaceScopeAdapter{}
var _ sqlstore.QueueScopeIndex = queueScopeAdapter{}
var _ inbox.WorkNotifier = workNotifierAdapter{}
var _ apptemplate.PublicationWorkNotifier = workNotifierAdapter{}
var _ template.RecipientDirectory = recipientDirectoryAdapter{}
var _ appinbox.RecipientLocaleResolver = recipientLocaleAdapter{}
var _ appinbox.AudienceResolver = audienceAdapter{}
var _ delivery.Dispatcher = deliveryGatewayAdapter{}
