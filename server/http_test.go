package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/contract"
	notificationremote "github.com/domainry/domainry-notification-sdk/remote"
)

type serviceAuthenticationStub struct {
	request ServiceRequest
	err     error
}

func TestRemoteFactoryAndPublisherUseServerWireContract(t *testing.T) {
	publisher := &httpPublisherStub{}
	authenticator := &serviceAuthenticationStub{}
	handler, err := NewHandler(authenticator, &bindingResolverStub{binding: &httpBindingStub{publisher: publisher}})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	factory := notificationremote.NewFactory(notificationremote.Config{BaseURL: httpServer.URL, ServiceCredential: "service-token", HTTPClient: httpServer.Client()})
	application := notificationsdk.ApplicationRef{TenantID: "tenant-a", WorkspaceID: "workspace-a", ApplicationKey: "runtime-a"}
	binding, err := factory.Open(t.Context(), application)
	if err != nil {
		t.Fatal(err)
	}
	intent := contract.NotificationIntent{ID: "request-a", WorkspaceID: "workspace-a", SourceEventID: "record-a:created", EventType: "record.created", Surface: "business_workspace", RecipientUserIDs: []string{"user-a"}, OccurredAt: "2026-08-28T00:00:00Z"}
	event, created, err := binding.Publisher().PublishIntent(t.Context(), intent)
	if err != nil {
		t.Fatal(err)
	}
	if !created || event.ID != "remote-event" || publisher.intent.ID != intent.ID || authenticator.request.Credential != "service-token" || authenticator.request.Application != application {
		t.Fatalf("created=%v event=%+v published=%+v auth=%+v", created, event, publisher.intent, authenticator.request)
	}
}

func (a *serviceAuthenticationStub) Authenticate(_ context.Context, request ServiceRequest) (ServiceAuthority, error) {
	a.request = request
	return ServiceAuthority{Application: request.Application}, a.err
}

type bindingResolverStub struct {
	application notificationsdk.ApplicationRef
	binding     notificationsdk.Binding
}

func (r *bindingResolverStub) Resolve(_ context.Context, application notificationsdk.ApplicationRef) (notificationsdk.Binding, error) {
	r.application = application
	return r.binding, nil
}

type httpBindingStub struct {
	publisher *httpPublisherStub
	closed    int
}

func (*httpBindingStub) Descriptor() notificationsdk.Descriptor             { return notificationsdk.Descriptor{} }
func (b *httpBindingStub) Publisher() notificationsdk.Publisher             { return b.publisher }
func (*httpBindingStub) Inbox() notificationsdk.Inbox                       { return nil }
func (*httpBindingStub) Templates() notificationsdk.Templates               { return nil }
func (*httpBindingStub) Delivery() notificationsdk.Delivery                 { return nil }
func (*httpBindingStub) Administration() notificationsdk.Administration     { return nil }
func (*httpBindingStub) LocalWorkers() (notificationsdk.LocalWorkers, bool) { return nil, false }
func (b *httpBindingStub) Close(context.Context) error                      { b.closed++; return nil }

type httpPublisherStub struct{ intent contract.NotificationIntent }

func (p *httpPublisherStub) PublishIntent(_ context.Context, intent contract.NotificationIntent) (contract.NotificationEvent, bool, error) {
	p.intent = intent
	return contract.NotificationEvent{ID: "remote-event"}, true, nil
}

func TestHandlerAuthenticatesAndPublishesScopedIntent(t *testing.T) {
	publisher := &httpPublisherStub{}
	authenticator := &serviceAuthenticationStub{}
	resolver := &bindingResolverStub{binding: &httpBindingStub{publisher: publisher}}
	handler, err := NewHandler(authenticator, resolver)
	if err != nil {
		t.Fatal(err)
	}
	intent := contract.NotificationIntent{ID: "request-a", WorkspaceID: "workspace-a", SourceEventID: "record-a:created", EventType: "record.created", Surface: "business_workspace", OccurredAt: "2026-08-28T00:00:00Z"}
	body, _ := json.Marshal(intent)
	request := httptest.NewRequest(http.MethodPost, "/v1/events:publish", bytes.NewReader(body))
	request.Header.Set("X-Domainry-Service-Credential", "service-token")
	request.Header.Set("X-Domainry-Tenant-ID", "tenant-a")
	request.Header.Set("X-Domainry-Workspace-ID", "workspace-a")
	request.Header.Set("X-Domainry-Application-Key", "runtime-a")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || publisher.intent.ID != intent.ID {
		t.Fatalf("status=%d body=%s intent=%+v", response.Code, response.Body.String(), publisher.intent)
	}
	application := notificationsdk.ApplicationRef{TenantID: "tenant-a", WorkspaceID: "workspace-a", ApplicationKey: "runtime-a"}
	if authenticator.request.Credential != "service-token" || authenticator.request.Application != application || resolver.application != application {
		t.Fatalf("auth=%+v resolved=%+v", authenticator.request, resolver.application)
	}
}

func TestHandlerRejectsWorkspacePayloadMismatchBeforePublication(t *testing.T) {
	publisher := &httpPublisherStub{}
	handler, _ := NewHandler(&serviceAuthenticationStub{}, &bindingResolverStub{binding: &httpBindingStub{publisher: publisher}})
	intent := contract.NotificationIntent{ID: "request-a", WorkspaceID: "workspace-b", SourceEventID: "record-a:created", EventType: "record.created", Surface: "business_workspace", OccurredAt: "2026-08-28T00:00:00Z"}
	body, _ := json.Marshal(intent)
	request := httptest.NewRequest(http.MethodPost, "/v1/events:publish", bytes.NewReader(body))
	request.Header.Set("X-Domainry-Service-Credential", "service-token")
	request.Header.Set("X-Domainry-Tenant-ID", "tenant-a")
	request.Header.Set("X-Domainry-Workspace-ID", "workspace-a")
	request.Header.Set("X-Domainry-Application-Key", "runtime-a")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || publisher.intent.ID != "" {
		t.Fatalf("status=%d body=%s published=%+v", response.Code, response.Body.String(), publisher.intent)
	}
}

func TestHandlerDescriptorReportsSaaSModeForExactApplication(t *testing.T) {
	handler, _ := NewHandler(&serviceAuthenticationStub{}, &bindingResolverStub{binding: &httpBindingStub{publisher: &httpPublisherStub{}}})
	request := httptest.NewRequest(http.MethodGet, "/v1/descriptor", nil)
	request.Header.Set("X-Domainry-Service-Credential", "service-token")
	request.Header.Set("X-Domainry-Tenant-ID", "tenant-a")
	request.Header.Set("X-Domainry-Workspace-ID", "workspace-a")
	request.Header.Set("X-Domainry-Application-Key", "runtime-a")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var descriptor notificationsdk.Descriptor
	if err := json.Unmarshal(response.Body.Bytes(), &descriptor); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || descriptor.Mode != notificationsdk.DeploymentModeSaaS || descriptor.Audience != "runtime-a" {
		t.Fatalf("status=%d descriptor=%+v", response.Code, descriptor)
	}
}
