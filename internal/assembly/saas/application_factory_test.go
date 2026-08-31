package saas

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/domainry/domainry-foundation/modulehttp"
	identitysdk "github.com/domainry/domainry-identity-sdk"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/contract"
	"github.com/domainry/domainry-notification-sdk/deliverygateway"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	notificationremote "github.com/domainry/domainry-notification-sdk/remote"
	sqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence"

	_ "modernc.org/sqlite"
)

type loseFirstPublicationResponseTransport struct {
	base http.RoundTripper
	lost bool
}

func (t *loseFirstPublicationResponseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err == nil && request.URL.Path == "/v1/events:publish" && !t.lost {
		t.lost = true
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		return nil, errors.New("publication response lost after server commit")
	}
	return response, err
}

type applicationIdentityStub struct{ identitysdk.Binding }
type applicationAuthenticationStub struct{ identitysdk.Authentication }
type applicationTokensStub struct{ identitysdk.TokenVerifier }
type applicationAuthorizationStub struct{ identitysdk.Authorization }
type applicationPrincipalResolverStub struct{ identitysdk.PrincipalResolver }

func (applicationIdentityStub) Authentication() identitysdk.Authentication {
	return applicationAuthenticationStub{}
}
func (applicationIdentityStub) Tokens() identitysdk.TokenVerifier { return applicationTokensStub{} }
func (applicationIdentityStub) Authorization() identitysdk.Authorization {
	return applicationAuthorizationStub{}
}
func (applicationIdentityStub) Principals() identitysdk.PrincipalResolver {
	return applicationPrincipalResolverStub{}
}
func (applicationIdentityStub) Directory() identitysdk.Directory { return directoryStub{} }

func (applicationPrincipalResolverStub) Resolve(_ context.Context, request identitysdk.PrincipalResolutionRequest) (identitysdk.PrincipalResolution, error) {
	bundle := identitysdk.AccessBundle{FunctionGrants: []identitysdk.FunctionGrant{{Resource: "*", Action: "*", Effect: identitysdk.EffectAllow}}}
	return identitysdk.PrincipalResolution{
		Principal:    identitysdk.Principal{Known: true, WorkspaceID: string(request.Application.WorkspaceID), UserID: string(request.SubjectID), AccessBundle: &bundle},
		AccessBundle: bundle,
	}, nil
}

type applicationGatewayStub struct{}

func (applicationGatewayStub) Dispatch(context.Context, modulehost.DeliveryRequest) (modulehost.DeliveryReceipt, error) {
	return modulehost.DeliveryReceipt{MessageID: "message"}, nil
}

type remoteGatewayStub struct{ request deliverygateway.Request }

func (g *remoteGatewayStub) Dispatch(_ context.Context, _ notificationsdk.ApplicationRef, request deliverygateway.Request) (deliverygateway.Receipt, error) {
	g.request = request
	return deliverygateway.Receipt{RequestID: request.RequestID, MessageID: "remote-message"}, nil
}

func TestSQLApplicationFactoryOpensSharedSaaSDomainApplication(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	persistence, err := NewSQLPersistence(SQLPersistenceOptions{Database: db, Driver: sqlstore.SQLite, OwnsDatabase: true})
	if err != nil {
		t.Fatal(err)
	}
	factory, err := NewSQLApplicationFactory(SQLApplicationFactoryOptions{
		Persistence: persistence,
		Catalog: modulehost.Catalog{
			DefaultLocale: "en", Surfaces: []string{"business_workspace"}, TemplateCapabilities: []contract.NotificationTemplateCapability{{Channel: "in_app"}},
			EventTypes: []contract.NotificationEventType{{Key: "report.completed", Source: "report", Category: "report", DefaultSeverity: "info", Surfaces: []string{"business_workspace"}, MandatoryInApp: true, TemplateKey: "report.completed", DefaultLocale: "en", Locales: map[string]contract.NotificationInboxEventTypeContent{"en": {Title: "Report ready", Body: "The report is ready."}}, Version: 1, Status: "published"}},
		},
		WorkerID:        "notification-test",
		DeliveryGateway: applicationGatewayStub{},
	})
	if err != nil {
		t.Fatal(err)
	}
	application := notificationsdk.ApplicationRef{TenantID: "tenant", WorkspaceID: "workspace", ApplicationKey: "application"}
	binding, err := factory.OpenSaaS(t.Context(), application, applicationIdentityStub{})
	if err != nil {
		t.Fatal(err)
	}
	if binding.Descriptor().Mode != notificationsdk.DeploymentModeSaaS {
		t.Fatalf("descriptor=%+v", binding.Descriptor())
	}
	if workers, local := binding.LocalWorkers(); !local || workers == nil {
		t.Fatal("server-owned workers are unavailable")
	}
	intent := contract.NotificationIntent{ID: "event", WorkspaceID: "another-workspace", SourceEventID: "source", EventType: "report.completed", Surface: "business_workspace", RecipientUserIDs: []string{"user"}, OccurredAt: "2026-08-28T00:00:00.000000000Z", SubjectType: "report", SubjectID: "report", SubjectVersion: "one"}
	if _, _, err := binding.Publisher().PublishIntent(t.Context(), intent); err == nil {
		t.Fatal("cross-workspace publication was accepted")
	}
	intent.WorkspaceID = application.WorkspaceID
	first, created, err := binding.Publisher().PublishIntent(t.Context(), intent)
	if err != nil || !created {
		t.Fatalf("first ingest created=%v event=%+v err=%v", created, first, err)
	}
	second, created, err := binding.Publisher().PublishIntent(t.Context(), intent)
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("duplicate ingest created=%v event=%+v err=%v", created, second, err)
	}
	intent.SubjectVersion = "two"
	if _, _, err := binding.Publisher().PublishIntent(t.Context(), intent); err == nil {
		t.Fatal("conflicting retry was accepted")
	} else {
		var sdkError *notificationsdk.Error
		if !errors.As(err, &sdkError) || sdkError.StatusCode != 409 || sdkError.Code != "notification.request_identity_conflict" {
			t.Fatalf("conflicting retry error=%v", err)
		}
	}
	if err := binding.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := factory.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := db.PingContext(t.Context()); err == nil {
		t.Fatal("service-owned database remained open")
	}
}

func TestRemotePublicationReconcilesResponseLossWithoutDuplicateIngest(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	persistence, err := NewSQLPersistence(SQLPersistenceOptions{Database: db, Driver: sqlstore.SQLite})
	if err != nil {
		t.Fatal(err)
	}
	factory, err := NewSQLApplicationFactory(SQLApplicationFactoryOptions{
		Persistence: persistence,
		Catalog: modulehost.Catalog{
			DefaultLocale: "en", Surfaces: []string{"business_workspace"},
			EventTypes: []contract.NotificationEventType{{Key: "report.completed", Source: "report", Category: "report", DefaultSeverity: "info", Surfaces: []string{"business_workspace"}, MandatoryInApp: true, TemplateKey: "report.completed", DefaultLocale: "en", Locales: map[string]contract.NotificationInboxEventTypeContent{"en": {Title: "Report ready", Body: "Ready"}}, Version: 1, Status: "published"}},
		},
		WorkerID: "notification-reconciliation-test", DeliveryGateway: applicationGatewayStub{},
	})
	if err != nil {
		t.Fatal(err)
	}
	application := notificationsdk.ApplicationRef{TenantID: "tenant", WorkspaceID: "workspace", ApplicationKey: "application"}
	local, err := factory.OpenSaaS(t.Context(), application, applicationIdentityStub{})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(&serviceAuthenticationStub{}, &bindingResolverStub{binding: local})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	transport := &loseFirstPublicationResponseTransport{base: http.DefaultTransport}
	remoteBinding, err := NewRemoteFactory(notificationremote.NewFactory(notificationremote.Config{BaseURL: server.URL, ServiceCredential: "service", HTTPClient: &http.Client{Transport: transport}})).Open(t.Context(), application)
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := remoteBinding.(modulehttp.Provider)
	if !ok || len(provider.HTTPSurfaces()) != 1 || len(provider.HTTPSurfaces()[0].Routes()) != 22 {
		t.Fatalf("SaaS Notification HTTP surfaces=%v", provider)
	}
	intent := contract.NotificationIntent{ID: "event", WorkspaceID: "workspace", SourceEventID: "source", EventType: "report.completed", Surface: "business_workspace", RecipientUserIDs: []string{"user"}, OccurredAt: "2026-08-29T00:00:00Z", SubjectType: "report", SubjectID: "report", SubjectVersion: "one"}
	event, created, err := remoteBinding.Publisher().PublishIntent(t.Context(), intent)
	if err != nil || created || event.ID == "" || !transport.lost {
		t.Fatalf("event=%+v created=%v response_lost=%v err=%v", event, created, transport.lost, err)
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM _notification_events WHERE workspace_id = ? AND source_event_id = ?`, application.WorkspaceID, intent.SourceEventID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("durable event rows=%d", rows)
	}
}

func TestExactQueueScopeRejectsCrossWorkspaceRegistration(t *testing.T) {
	scope := exactQueueScope{workspaceID: "workspace"}
	if err := scope.Register(t.Context(), nil, "inbox", "other", "now"); err == nil {
		t.Fatal("cross-workspace queue scope was accepted")
	}
	values, err := scope.Workspaces(t.Context(), nil, "inbox", 10)
	if err != nil || len(values) != 1 || values[0] != "workspace" {
		t.Fatalf("values=%v err=%v", values, err)
	}
}

func TestRemoteDeliveryGatewayAdapterPreservesStablePlanIdentity(t *testing.T) {
	remote := &remoteGatewayStub{}
	adapter := remoteDeliveryGatewayAdapter{application: notificationsdk.ApplicationRef{TenantID: "tenant", WorkspaceID: "workspace", ApplicationKey: "application"}, gateway: remote}
	receipt, err := adapter.Dispatch(t.Context(), modulehost.DeliveryRequest{WorkspaceID: "workspace", PlanID: "plan", EventID: "event", Channel: "email", ConnectorKey: "smtp", Operation: "send", CreatedAt: "now"})
	if err != nil || receipt.MessageID != "remote-message" {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	if remote.request.RequestID != "plan" || remote.request.DedupeKey != "plan" {
		t.Fatalf("request=%+v", remote.request)
	}
}
