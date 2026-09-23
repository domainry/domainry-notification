package saas

import (
	"context"

	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/contract"
)

type serviceAuthenticationStub struct{}

func (*serviceAuthenticationStub) Authenticate(_ context.Context, request ServiceRequest) (ServiceAuthority, error) {
	return ServiceAuthority{Application: request.Application}, nil
}

type bindingResolverStub struct{ binding notificationsdk.Binding }

func (r *bindingResolverStub) Resolve(context.Context, notificationsdk.ApplicationRef) (notificationsdk.Binding, error) {
	return r.binding, nil
}

type httpBindingStub struct {
	publisher *httpPublisherStub
	closed    int
}

func (*httpBindingStub) Descriptor() notificationsdk.Descriptor         { return notificationsdk.Descriptor{} }
func (b *httpBindingStub) Publisher() notificationsdk.Publisher         { return b.publisher }
func (*httpBindingStub) Inbox() notificationsdk.Inbox                   { return nil }
func (*httpBindingStub) Templates() notificationsdk.Templates           { return nil }
func (*httpBindingStub) Delivery() notificationsdk.Delivery             { return nil }
func (*httpBindingStub) Administration() notificationsdk.Administration { return nil }
func (*httpBindingStub) LocalWorkers() (notificationsdk.LocalWorkers, bool) {
	return nil, false
}
func (b *httpBindingStub) Close(context.Context) error { b.closed++; return nil }

type httpPublisherStub struct{}

func (*httpPublisherStub) PublishIntent(context.Context, contract.NotificationIntent) (contract.NotificationEvent, bool, error) {
	return contract.NotificationEvent{ID: "remote-event"}, true, nil
}
