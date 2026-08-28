package server

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	identitysdk "github.com/domainry/domainry-identity-sdk"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/contract"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	"github.com/domainry/domainry-notification/sqlstore"

	_ "modernc.org/sqlite"
)

type applicationIdentityStub struct{ identitysdk.Binding }
type applicationAuthenticationStub struct{ identitysdk.Authentication }
type applicationTokensStub struct{ identitysdk.TokenVerifier }
type applicationAuthorizationStub struct{ identitysdk.Authorization }

func (applicationIdentityStub) Authentication() identitysdk.Authentication {
	return applicationAuthenticationStub{}
}
func (applicationIdentityStub) Tokens() identitysdk.TokenVerifier { return applicationTokensStub{} }
func (applicationIdentityStub) Authorization() identitysdk.Authorization {
	return applicationAuthorizationStub{}
}
func (applicationIdentityStub) Directory() identitysdk.Directory { return directoryStub{} }

type applicationGatewayStub struct{}

func (applicationGatewayStub) Dispatch(context.Context, modulehost.DeliveryRequest) (modulehost.DeliveryReceipt, error) {
	return modulehost.DeliveryReceipt{MessageID: "message"}, nil
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
