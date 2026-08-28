package module

import (
	"context"
	"database/sql"
	"testing"
	"time"

	identitysdk "github.com/domainry/domainry-identity-sdk"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/contract"
	"github.com/domainry/domainry-notification-sdk/contracttest"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	"github.com/domainry/domainry-notification/sqlstore"
	_ "modernc.org/sqlite"
)

type testClock struct{}

func (testClock) Now() time.Time { return time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC) }

type testWorkspaceScope struct{}

func (testWorkspaceScope) Context(ctx context.Context, _ string) context.Context { return ctx }

type testQueueScopes struct{}

func (testQueueScopes) Register(context.Context, modulehost.Executor, string, string, string) error {
	return nil
}
func (testQueueScopes) Workspaces(context.Context, modulehost.Queryer, string, int) ([]string, error) {
	return nil, nil
}

type testWorkNotifier struct{}

func (testWorkNotifier) Notify(context.Context, modulehost.WorkLocator) {}

type testDirectory struct{}

func (testDirectory) FindRecipient(context.Context, string, string) (modulehost.Recipient, bool, error) {
	return modulehost.Recipient{}, false, nil
}

type testAudience struct{}

func (testAudience) ResolveAudience(context.Context, string, contract.NotificationEvent) ([]string, error) {
	return nil, nil
}

type testGateway struct{}

func (testGateway) Dispatch(context.Context, modulehost.DeliveryRequest) (modulehost.DeliveryReceipt, error) {
	return modulehost.DeliveryReceipt{MessageID: "message"}, nil
}

type tokenVerifierStub struct{ identitysdk.TokenVerifier }
type authenticationStub struct{ identitysdk.Authentication }
type authorizationStub struct{ identitysdk.Authorization }
type identityBindingStub struct{ identitysdk.Binding }

func (identityBindingStub) Tokens() identitysdk.TokenVerifier          { return tokenVerifierStub{} }
func (identityBindingStub) Authentication() identitysdk.Authentication { return authenticationStub{} }
func (identityBindingStub) Authorization() identitysdk.Authorization   { return authorizationStub{} }
func (identityBindingStub) Close(context.Context) error                { return nil }

type testHost struct {
	database   *sql.DB
	dialect    modulehost.Dialect
	migrations *testMigrationRegistrar
}

type testMigrationRegistrar struct {
	database *sql.DB
	applied  bool
}

func (testMigrationRegistrar) Driver() string { return "sqlite" }
func (testMigrationRegistrar) Schema() string { return "" }
func (r *testMigrationRegistrar) ApplyOwnedMigrations(ctx context.Context, owner string, migrations []modulehost.SchemaMigration) error {
	if owner != "notification" {
		return context.Canceled
	}
	if r.applied {
		return nil
	}
	for _, migration := range migrations {
		for _, statement := range migration.Statements {
			if _, err := r.database.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
	}
	r.applied = true
	return nil
}

func (h testHost) Database() modulehost.Database           { return h.database }
func (h testHost) Dialect() modulehost.Dialect             { return h.dialect }
func (testHost) WorkspaceScope() modulehost.WorkspaceScope { return testWorkspaceScope{} }
func (testHost) QueueScopes() modulehost.QueueScopeIndex   { return testQueueScopes{} }
func (testHost) Identity() identitysdk.Binding             { return identityBindingStub{} }
func (testHost) Clock() modulehost.Clock                   { return testClock{} }
func (testHost) WorkerID() string                          { return "worker" }
func (testHost) Catalog() modulehost.Catalog {
	return modulehost.Catalog{DefaultLocale: "en", Surfaces: []string{"business_workspace"}, TemplateCapabilities: []contract.NotificationTemplateCapability{{Channel: "in_app"}}}
}
func (testHost) WorkNotifier() modulehost.WorkNotifier                           { return testWorkNotifier{} }
func (testHost) RecipientDirectory() modulehost.RecipientDirectory               { return testDirectory{} }
func (testHost) AudienceResolver() modulehost.AudienceResolver                   { return testAudience{} }
func (testHost) DeliveryGateway() modulehost.DeliveryGateway                     { return testGateway{} }
func (testHost) DeliveryMetrics() modulehost.DeliveryMetrics                     { return nil }
func (testHost) ProviderTemplateValidator() modulehost.ProviderTemplateValidator { return nil }
func (h testHost) Migrations() modulehost.MigrationRegistrar {
	return h.migrations
}

func newTestHost(t *testing.T) testHost {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	dialect, err := sqlstore.NewDialect(sqlstore.SQLite, "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return testHost{database: database, dialect: dialect, migrations: &testMigrationRegistrar{database: database}}
}

func TestModuleFactoryContractAndBorrowedDatabaseLifecycle(t *testing.T) {
	host := newTestHost(t)
	factory := NewFactory(Options{})
	contracttest.Run(t, func(testing.TB) (notificationsdk.Factory, notificationsdk.ApplicationRef) {
		return moduleFactoryContractAdapter{factory: factory, host: host}, notificationsdk.ApplicationRef{TenantID: "tenant", WorkspaceID: "workspace", ApplicationKey: "runtime"}
	})
	binding, err := factory.OpenModule(t.Context(), notificationsdk.ApplicationRef{TenantID: "tenant", WorkspaceID: "workspace", ApplicationKey: "runtime"}, host)
	if err != nil {
		t.Fatal(err)
	}
	if err := binding.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := host.database.PingContext(t.Context()); err != nil {
		t.Fatalf("Module closed borrowed database: %v", err)
	}
}

func TestModuleSystemTemplatesSynchronizeThroughOwnedStore(t *testing.T) {
	host := newTestHost(t)
	binding, err := NewFactory(Options{}).OpenModule(t.Context(), notificationsdk.ApplicationRef{TenantID: "tenant", WorkspaceID: "workspace", ApplicationKey: "runtime"}, host)
	if err != nil {
		t.Fatal(err)
	}
	system, ok := binding.(notificationsdk.SystemTemplateBinding)
	if !ok || system.SystemTemplates() == nil {
		t.Fatal("Module Binding did not expose system templates")
	}
	template := contract.NotificationTemplate{Key: "welcome", Name: "Welcome", Channel: "in_app", Status: "active", Version: 1, DefaultLocale: "en", Locales: map[string]contract.NotificationTemplateContent{"en": {Title: "Welcome", Text: "Hello"}}}
	if err := system.SystemTemplates().SyncPublished(t.Context(), []contract.NotificationTemplate{template}); err != nil {
		t.Fatal(err)
	}
	records, err := system.SystemTemplates().ListPublished(t.Context())
	if err != nil || len(records) != 1 || records[0].Key != template.Key || records[0].Published == nil {
		t.Fatalf("records=%+v err=%v", records, err)
	}
}

func TestModuleSystemMigrationExportsAndIdempotentlyReconcilesExactApplication(t *testing.T) {
	host := newTestHost(t)
	application := notificationsdk.ApplicationRef{TenantID: "tenant", WorkspaceID: "workspace", ApplicationKey: "runtime"}
	binding, err := NewFactory(Options{}).OpenModule(t.Context(), application, host)
	if err != nil {
		t.Fatal(err)
	}
	migration, ok := binding.(notificationsdk.SystemMigrationBinding)
	if !ok || migration.SystemMigration() == nil {
		t.Fatal("Module Binding did not expose system migration")
	}
	if _, err := host.database.Exec(`INSERT INTO notification_retention_archive (id, workspace_id, policy_key, policy_version, job_id, source_table, resource_id, payload_hash, payload_json, archived_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "archive", "workspace", "notification.history.v1", "1", "job", "notification_events", "event", "hash", `{}`, "now"); err != nil {
		t.Fatal(err)
	}
	exported, err := migration.SystemMigration().Export(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if exported.Bundle.Source != (contract.NotificationPortableScope{TenantID: "tenant", WorkspaceID: "workspace", ApplicationKey: "runtime"}) || exported.Bundle.Fingerprint == "" || len(exported.Bundle.Tables) != 16 {
		t.Fatalf("export=%+v", exported)
	}
	receipt, err := migration.SystemMigration().Import(t.Context(), exported.Bundle)
	if err != nil || !receipt.AlreadyPresent || receipt.Fingerprint != exported.Bundle.Fingerprint {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
}

type moduleFactoryContractAdapter struct {
	factory *Factory
	host    modulehost.Host
}

func (a moduleFactoryContractAdapter) Open(ctx context.Context, application notificationsdk.ApplicationRef) (notificationsdk.Binding, error) {
	return a.factory.OpenModule(ctx, application, a.host)
}
