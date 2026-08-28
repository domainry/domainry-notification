package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
	system    *httpSystemTemplatesStub
	subjects  *httpSystemSubjectsStub
	retention *httpSystemRetentionStub
	migration *httpSystemMigrationStub
	closed    int
}

func (*httpBindingStub) Descriptor() notificationsdk.Descriptor             { return notificationsdk.Descriptor{} }
func (b *httpBindingStub) Publisher() notificationsdk.Publisher             { return b.publisher }
func (*httpBindingStub) Inbox() notificationsdk.Inbox                       { return nil }
func (*httpBindingStub) Templates() notificationsdk.Templates               { return nil }
func (*httpBindingStub) Delivery() notificationsdk.Delivery                 { return nil }
func (*httpBindingStub) Administration() notificationsdk.Administration     { return nil }
func (b *httpBindingStub) SystemTemplates() notificationsdk.SystemTemplates { return b.system }
func (b *httpBindingStub) SystemSubjects() notificationsdk.SystemSubjects   { return b.subjects }
func (b *httpBindingStub) SystemRetention() notificationsdk.SystemRetention { return b.retention }
func (b *httpBindingStub) SystemMigration() notificationsdk.SystemMigration { return b.migration }
func (*httpBindingStub) LocalWorkers() (notificationsdk.LocalWorkers, bool) { return nil, false }
func (b *httpBindingStub) Close(context.Context) error                      { b.closed++; return nil }

type httpPublisherStub struct{ intent contract.NotificationIntent }

func (p *httpPublisherStub) PublishIntent(_ context.Context, intent contract.NotificationIntent) (contract.NotificationEvent, bool, error) {
	p.intent = intent
	return contract.NotificationEvent{ID: "remote-event"}, true, nil
}

type httpSystemTemplatesStub struct {
	synced []contract.NotificationTemplate
}

type httpSystemSubjectsStub struct{ calls int }
type httpSystemRetentionStub struct{ calls int }
type httpSystemMigrationStub struct{ imports int }

func (s *httpSystemMigrationStub) Export(context.Context) (contract.NotificationPortableExport, error) {
	return contract.NotificationPortableExport{}, nil
}
func (s *httpSystemMigrationStub) Import(_ context.Context, bundle contract.NotificationPortableBundle) (contract.NotificationPortableImportReceipt, error) {
	s.imports++
	return contract.NotificationPortableImportReceipt{FormatVersion: bundle.FormatVersion, Fingerprint: bundle.Fingerprint}, nil
}

func TestHandlerSystemMigrationRejectsCrossApplicationBundleBeforeImport(t *testing.T) {
	migration := &httpSystemMigrationStub{}
	handler, _ := NewHandler(&serviceAuthenticationStub{}, &bindingResolverStub{binding: &httpBindingStub{publisher: &httpPublisherStub{}, migration: migration}})
	bundle := contract.NotificationPortableBundle{
		FormatVersion: contract.NotificationPortableFormatV1,
		Source:        contract.NotificationPortableScope{TenantID: "tenant-a", WorkspaceID: "workspace-b", ApplicationKey: "runtime-a"},
		Tables:        []contract.NotificationPortableTable{{Name: "notification_events", Columns: []string{"id"}, Rows: [][]json.RawMessage{{json.RawMessage(`"event"`)}}}},
		Fingerprint:   "fingerprint",
	}
	body, _ := json.Marshal(bundle)
	request := httptest.NewRequest(http.MethodPost, "/v1/system/migration:import", bytes.NewReader(body))
	request.Header.Set("X-Domainry-Service-Credential", "service-token")
	request.Header.Set("X-Domainry-Tenant-ID", "tenant-a")
	request.Header.Set("X-Domainry-Workspace-ID", "workspace-a")
	request.Header.Set("X-Domainry-Application-Key", "runtime-a")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || migration.imports != 0 {
		t.Fatalf("status=%d imports=%d body=%s", response.Code, migration.imports, response.Body.String())
	}
}

func (s *httpSystemRetentionStub) Preview(context.Context, contract.NotificationRetentionPreviewRequest) (contract.NotificationRetentionPreview, error) {
	s.calls++
	return contract.NotificationRetentionPreview{Rows: 1}, nil
}
func (s *httpSystemRetentionStub) ProcessBatch(context.Context, contract.NotificationRetentionBatchRequest) (contract.NotificationRetentionBatchResult, error) {
	s.calls++
	return contract.NotificationRetentionBatchResult{Scanned: 1, Done: true}, nil
}

func TestHandlerSystemRetentionEnforcesExactApplicationWorkspace(t *testing.T) {
	retention := &httpSystemRetentionStub{}
	handler, _ := NewHandler(&serviceAuthenticationStub{}, &bindingResolverStub{binding: &httpBindingStub{publisher: &httpPublisherStub{}, retention: retention}})
	policy := contract.NotificationRetentionPolicy{Key: contract.NotificationRetentionHistoryPolicy, Version: "1", DefaultRetentionSeconds: 3600}
	for _, workspace := range []string{"workspace-a", "workspace-b"} {
		body, _ := json.Marshal(contract.NotificationRetentionPreviewRequest{WorkspaceID: workspace, Policy: policy, Now: time.Date(2026, 8, 29, 1, 0, 0, 0, time.UTC)})
		request := httptest.NewRequest(http.MethodPost, "/v1/system/retention:preview", bytes.NewReader(body))
		request.Header.Set("X-Domainry-Service-Credential", "service-token")
		request.Header.Set("X-Domainry-Tenant-ID", "tenant-a")
		request.Header.Set("X-Domainry-Workspace-ID", "workspace-a")
		request.Header.Set("X-Domainry-Application-Key", "runtime-a")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		want := http.StatusOK
		if workspace != "workspace-a" {
			want = http.StatusForbidden
		}
		if response.Code != want {
			t.Fatalf("workspace=%s status=%d body=%s", workspace, response.Code, response.Body.String())
		}
	}
	if retention.calls != 1 {
		t.Fatalf("retention calls=%d", retention.calls)
	}
}

func (s *httpSystemSubjectsStub) PreviewSubject(context.Context, string, string) (json.RawMessage, error) {
	s.calls++
	return json.RawMessage(`{"preview":true}`), nil
}
func (s *httpSystemSubjectsStub) ExportSubject(context.Context, string, string) (json.RawMessage, error) {
	s.calls++
	return json.RawMessage(`{"export":true}`), nil
}
func (s *httpSystemSubjectsStub) EraseSubject(context.Context, string, string, json.RawMessage) (json.RawMessage, error) {
	s.calls++
	return json.RawMessage(`{"erased":true}`), nil
}

func TestHandlerSystemSubjectsEnforceExactApplicationWorkspace(t *testing.T) {
	subjects := &httpSystemSubjectsStub{}
	handler, _ := NewHandler(&serviceAuthenticationStub{}, &bindingResolverStub{binding: &httpBindingStub{publisher: &httpPublisherStub{}, subjects: subjects}})
	for _, workspace := range []string{"workspace-a", "workspace-b"} {
		body, _ := json.Marshal(map[string]any{"workspace_id": workspace, "subject_id": "user"})
		request := httptest.NewRequest(http.MethodPost, "/v1/system/subjects:preview", bytes.NewReader(body))
		request.Header.Set("X-Domainry-Service-Credential", "service-token")
		request.Header.Set("X-Domainry-Tenant-ID", "tenant-a")
		request.Header.Set("X-Domainry-Workspace-ID", "workspace-a")
		request.Header.Set("X-Domainry-Application-Key", "runtime-a")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		want := http.StatusOK
		if workspace != "workspace-a" {
			want = http.StatusForbidden
		}
		if response.Code != want {
			t.Fatalf("workspace=%s status=%d body=%s", workspace, response.Code, response.Body.String())
		}
	}
	if subjects.calls != 1 {
		t.Fatalf("subject calls=%d", subjects.calls)
	}
}

func (s *httpSystemTemplatesStub) SyncPublished(_ context.Context, values []contract.NotificationTemplate) error {
	s.synced = append([]contract.NotificationTemplate(nil), values...)
	return nil
}

func (*httpSystemTemplatesStub) ListPublished(context.Context) ([]contract.NotificationTemplateRecord, error) {
	return []contract.NotificationTemplateRecord{{Key: "welcome"}}, nil
}

func TestHandlerSystemTemplatesUseServiceAuthenticationWithoutUserBearer(t *testing.T) {
	system := &httpSystemTemplatesStub{}
	handler, _ := NewHandler(&serviceAuthenticationStub{}, &bindingResolverStub{binding: &httpBindingStub{publisher: &httpPublisherStub{}, system: system}})
	body, _ := json.Marshal(map[string]any{"templates": []contract.NotificationTemplate{{Key: "welcome"}}})
	request := httptest.NewRequest(http.MethodPost, "/v1/system/templates:sync-published", bytes.NewReader(body))
	request.Header.Set("X-Domainry-Service-Credential", "service-token")
	request.Header.Set("X-Domainry-Tenant-ID", "tenant-a")
	request.Header.Set("X-Domainry-Workspace-ID", "workspace-a")
	request.Header.Set("X-Domainry-Application-Key", "runtime-a")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || len(system.synced) != 1 || system.synced[0].Key != "welcome" {
		t.Fatalf("status=%d body=%s synced=%+v", response.Code, response.Body.String(), system.synced)
	}
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
