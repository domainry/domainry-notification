package saas

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"

	actioncontract "github.com/domainry/domainry-foundation/action"
	"github.com/domainry/domainry-foundation/modulehttp"
	"github.com/domainry/domainry-foundation/requestcontext"
	identitysdk "github.com/domainry/domainry-identity-sdk"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/contract"
	"github.com/domainry/domainry-notification-sdk/deliverygateway"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	notificationremote "github.com/domainry/domainry-notification-sdk/remote"
	notificationapplication "github.com/domainry/domainry-notification/internal/application"
	sqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/artifactkernel"

	_ "modernc.org/sqlite"
)

type loseFirstPublicationResponseTransport struct {
	base http.RoundTripper
	lost bool
}

func (t *loseFirstPublicationResponseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err == nil && request.URL.Path == "/notification/v1/events:publish" && !t.lost {
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
func (applicationIdentityStub) Projection() identitysdk.Projection { return projectionStub{} }

func (applicationPrincipalResolverStub) Resolve(ctx context.Context, request identitysdk.PrincipalResolutionRequest) (identitysdk.PrincipalResolution, error) {
	bundle := identitysdk.AccessBundle{FunctionGrants: []identitysdk.FunctionGrant{{Resource: "*", Action: "*", Effect: identitysdk.EffectAllow}}}
	return identitysdk.PrincipalResolution{
		Principal:    identitysdk.Principal{Known: true, WorkspaceID: requestcontext.WorkspaceID(ctx), UserID: string(request.SubjectID), AccessBundle: &bundle},
		AccessBundle: bundle,
	}, nil
}

type applicationGatewayStub struct{}

func (applicationGatewayStub) Dispatch(context.Context, modulehost.DeliveryRequest) (modulehost.DeliveryReceipt, error) {
	return modulehost.DeliveryReceipt{MessageID: "message"}, nil
}

type remoteGatewayStub struct{ request deliverygateway.Request }

func newTestArtifactContent(t *testing.T) *artifactkernel.ContentFiles {
	t.Helper()
	content, err := artifactkernel.NewContentFiles(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = content.Close() })
	return content
}

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
			DefaultLocale: "en", TemplateCapabilities: []contract.NotificationTemplateCapability{{Channel: "in_app"}},
			EventTypes: []contract.NotificationEventType{{Key: "report.completed", Source: "report", Category: "report", DefaultSeverity: "info", MandatoryInApp: true, TemplateKey: "report.completed", DefaultLocale: "en", Locales: map[string]contract.NotificationInboxEventTypeContent{"en": {Title: "Report ready", Body: "The report is ready."}}, Version: 1, Status: "published"}},
		},
		WorkerID:        "notification-test",
		DeliveryGateway: applicationGatewayStub{},
		ArtifactContent: newTestArtifactContent(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	application := notificationsdk.ApplicationRef{WorkspaceID: "workspace", ApplicationKey: "application"}
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
	intent := contract.NotificationIntent{ID: "event", WorkspaceID: "another-workspace", SourceEventID: "source", EventType: "report.completed", RecipientUserIDs: []string{"user"}, OccurredAt: "2026-08-28T00:00:00.000000000Z", SubjectType: "report", SubjectID: "report", SubjectVersion: "one"}
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
			DefaultLocale: "en",
			EventTypes:    []contract.NotificationEventType{{Key: "report.completed", Source: "report", Category: "report", DefaultSeverity: "info", MandatoryInApp: true, TemplateKey: "report.completed", DefaultLocale: "en", Locales: map[string]contract.NotificationInboxEventTypeContent{"en": {Title: "Report ready", Body: "Ready"}}, Version: 1, Status: "published"}},
		},
		WorkerID: "notification-reconciliation-test", DeliveryGateway: applicationGatewayStub{}, ArtifactContent: newTestArtifactContent(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	application := notificationsdk.ApplicationRef{WorkspaceID: "workspace", ApplicationKey: "application"}
	local, err := factory.OpenSaaS(t.Context(), application, applicationIdentityStub{})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(&serviceAuthenticationStub{}, &bindingResolverStub{binding: local})
	if err != nil {
		t.Fatal(err)
	}
	handlerTransport := &inMemoryHandlerTransport{handler: handler}
	transport := &loseFirstPublicationResponseTransport{base: handlerTransport}
	remoteBinding, err := NewRemoteFactory(notificationremote.NewFactory(notificationremote.Config{BaseURL: "http://notification.test", ServiceCredential: "service", HTTPClient: &http.Client{Transport: transport}})).Open(t.Context(), application)
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := remoteBinding.(modulehttp.Provider)
	if !ok || len(provider.HTTPAdapters()) != 1 || len(provider.HTTPAdapters()[0].Routes()) != 41 {
		t.Fatalf("SaaS Notification HTTP adapters=%v", provider)
	}
	actionProvider, ok := remoteBinding.(actioncontract.Provider)
	if !ok {
		t.Fatal("SaaS Notification binding does not expose its canonical Action manifest")
	}
	providedActions, err := actionProvider.AuthorizationActions()
	if err != nil {
		t.Fatal(err)
	}
	canonicalActions, err := notificationapplication.AuthorizationActions()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(providedActions, canonicalActions) {
		t.Fatal("SaaS Notification binding drifted from the source-owned Action manifest")
	}
	for index, route := range provider.HTTPAdapters()[0].Routes() {
		if !reflect.DeepEqual(route.Action, canonicalActions[index]) {
			t.Fatalf("SaaS route %d drifted from the source-owned Action manifest", index)
		}
	}
	intent := contract.NotificationIntent{ID: "event", WorkspaceID: "workspace", SourceEventID: "source", EventType: "report.completed", RecipientUserIDs: []string{"user"}, OccurredAt: "2026-08-29T00:00:00Z", SubjectType: "report", SubjectID: "report", SubjectVersion: "one"}
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
	adapter := remoteDeliveryGatewayAdapter{application: notificationsdk.ApplicationRef{WorkspaceID: "workspace", ApplicationKey: "application"}, gateway: remote}
	receipt, err := adapter.Dispatch(t.Context(), modulehost.DeliveryRequest{WorkspaceID: "workspace", PlanID: "plan", EventID: "event", Channel: "email", ConnectorKey: "smtp", Operation: "send", CreatedAt: "now"})
	if err != nil || receipt.MessageID != "remote-message" {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	if remote.request.RequestID != "plan" || remote.request.DedupeKey != "plan" {
		t.Fatalf("request=%+v", remote.request)
	}
}
