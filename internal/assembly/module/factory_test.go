package module

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/domainry/domainry-foundation/modulecapability"
	capabilitycontracttest "github.com/domainry/domainry-foundation/modulecapability/contracttest"
	"github.com/domainry/domainry-foundation/modulehttp"
	"github.com/domainry/domainry-foundation/requestcontext"
	identitysdk "github.com/domainry/domainry-identity-sdk"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/contract"
	"github.com/domainry/domainry-notification-sdk/contracttest"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	notificationapplication "github.com/domainry/domainry-notification/internal/application"
	sqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence"
	ormdialect "github.com/domainry/domainry-orm/dialect"
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

type testRecipientResolver struct{}

func (testRecipientResolver) FindRecipient(context.Context, string, string) (modulehost.Recipient, bool, error) {
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
type principalResolverStub struct{ identitysdk.PrincipalResolver }
type identityBindingStub struct{ identitysdk.Binding }

func (identityBindingStub) Tokens() identitysdk.TokenVerifier          { return tokenVerifierStub{} }
func (identityBindingStub) Authentication() identitysdk.Authentication { return authenticationStub{} }
func (identityBindingStub) Authorization() identitysdk.Authorization   { return authorizationStub{} }
func (identityBindingStub) Principals() identitysdk.PrincipalResolver  { return principalResolverStub{} }
func (identityBindingStub) Close(context.Context) error                { return nil }

func (principalResolverStub) Resolve(ctx context.Context, request identitysdk.PrincipalResolutionRequest) (identitysdk.PrincipalResolution, error) {
	bundle := identitysdk.AccessBundle{FunctionGrants: []identitysdk.FunctionGrant{{Resource: "*", Action: "*", Effect: identitysdk.EffectAllow}}}
	return identitysdk.PrincipalResolution{
		Principal:    identitysdk.Principal{Known: true, WorkspaceID: requestcontext.WorkspaceID(ctx), UserID: string(request.SubjectID), AccessBundle: &bundle},
		AccessBundle: bundle,
	}, nil
}

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
	return modulehost.Catalog{DefaultLocale: "en", TemplateCapabilities: []contract.NotificationTemplateCapability{{Channel: "in_app"}}}
}
func (testHost) WorkNotifier() modulehost.WorkNotifier                           { return testWorkNotifier{} }
func (testHost) RecipientResolver() modulehost.RecipientResolver                 { return testRecipientResolver{} }
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
	dialect, err := ormdialect.ParseRenderer("sqlite", "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return testHost{database: database, dialect: dialect, migrations: &testMigrationRegistrar{database: database}}
}

func TestModuleFactoryContractAndBorrowedDatabaseLifecycle(t *testing.T) {
	host := newTestHost(t)
	host.database.SetMaxOpenConns(3)
	host.database.SetMaxIdleConns(1)
	factory := NewFactory(Options{})
	contracttest.Run(t, func(testing.TB) (notificationsdk.Factory, notificationsdk.ApplicationRef) {
		return moduleFactoryContractAdapter{factory: factory, host: host}, notificationsdk.ApplicationRef{WorkspaceID: "workspace", ApplicationKey: "runtime"}
	})
	binding, err := factory.OpenModule(t.Context(), notificationsdk.ApplicationRef{WorkspaceID: "workspace", ApplicationKey: "runtime"}, host)
	if err != nil {
		t.Fatal(err)
	}
	if err := binding.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := host.database.PingContext(t.Context()); err != nil {
		t.Fatalf("Module closed borrowed database: %v", err)
	}
	if stats := host.database.Stats(); stats.MaxOpenConnections != 3 {
		t.Fatalf("Notification Module reinitialized host pool: max open=%d", stats.MaxOpenConnections)
	}
	provider, ok := binding.(modulehttp.Provider)
	if !ok || len(provider.HTTPAdapters()) != 1 {
		t.Fatalf("Notification Module HTTP adapters=%v", provider)
	}
	if err := modulehttp.ValidateAdapter(provider.HTTPAdapters()[0]); err != nil {
		t.Fatal(err)
	}
	adapter := provider.HTTPAdapters()[0]
	routes := adapter.Routes()
	if len(routes) != 41 {
		t.Fatalf("Notification routes=%d", len(routes))
	}
	actions, err := notificationapplication.AuthorizationActions()
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != len(routes) {
		t.Fatalf("Notification Action/route counts=%d/%d", len(actions), len(routes))
	}
	for index := range actions {
		if !reflect.DeepEqual(routes[index].Action, actions[index]) {
			t.Fatalf("route %d is not an exact Action projection: route=%+v action=%+v", index, routes[index].Action, actions[index])
		}
	}
	openAPI, ok := adapter.(modulehttp.OpenAPIProvider)
	if !ok {
		t.Fatal("Notification Adapter does not project OpenAPI from its Actions")
	}
	operations := openAPI.OpenAPIOperations()
	if len(operations) != len(routes) {
		t.Fatalf("Notification OpenAPI/route counts=%d/%d", len(operations), len(routes))
	}
	for _, route := range routes {
		if _, found := operations[route.Pattern()]; !found {
			t.Fatalf("Notification Action route %q has no OpenAPI projection", route.Pattern())
		}
	}
	capabilitycontracttest.VerifyBinding(t, binding)
	summary, err := binding.CapabilitySummary(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	wantCounts := map[string]int{
		"notification.inbox":               20,
		"notification.delivery_governance": 7,
		"notification.templates":           14,
	}
	for _, category := range summary.Categories {
		if category.OperationCount != wantCounts[category.Key] {
			t.Fatalf("Notification category %q operations=%d want=%d", category.Key, category.OperationCount, wantCounts[category.Key])
		}
		delete(wantCounts, category.Key)
	}
	if len(wantCounts) != 0 {
		t.Fatalf("Notification categories are missing: %+v", wantCounts)
	}
	invalidTemplate, _ := json.Marshal(contract.NotificationTemplate{Key: "Invalid Key", Status: "draft"})
	validation := modulecapability.ValidationRequest{ContractVersion: modulecapability.ValidationContractVersion, ModuleKey: "notification", CategoryKey: "notification.templates", ContractSHA256: summary.Identity.ContractSHA256, Kind: "notification.template", Candidate: modulecapability.AuthoringFragment{Collection: "notification_templates", Key: "invalid", Value: invalidTemplate}}
	result, err := binding.ValidateCapabilityCandidate(t.Context(), validation)
	if err != nil || len(result.Diagnostics) == 0 || result.Diagnostics[0].Owner != "notification" {
		t.Fatalf("Notification owner validation result=%+v err=%v", result, err)
	}
	capabilitycontracttest.VerifyModuleRemoteParity(t, binding, capabilitycontracttest.ValidationCase{Name: "invalid template", Request: validation})
}

func TestModuleSystemTemplatesSynchronizeThroughOwnedStore(t *testing.T) {
	host := newTestHost(t)
	binding, err := NewFactory(Options{}).OpenModule(t.Context(), notificationsdk.ApplicationRef{WorkspaceID: "workspace", ApplicationKey: "runtime"}, host)
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
	application := notificationsdk.ApplicationRef{WorkspaceID: "workspace", ApplicationKey: "runtime"}
	binding, err := NewFactory(Options{}).OpenModule(t.Context(), application, host)
	if err != nil {
		t.Fatal(err)
	}
	migration, ok := binding.(notificationsdk.SystemMigrationBinding)
	if !ok || migration.SystemMigration() == nil {
		t.Fatal("Module Binding did not expose system migration")
	}
	if _, err := host.database.Exec(`INSERT INTO _notification_retention_archive_entries (id, workspace_id, policy_key, policy_version, job_id, source_table, resource_id, payload_hash, payload_json, archived_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "archive", "workspace", "notification.history.v1", "1", "job", "_notification_events", "event", "hash", `{}`, "now"); err != nil {
		t.Fatal(err)
	}
	const snapshotPayload = `{"recipient_user_ids":["user"],"snapshot":{"title":"Frozen title","body":"Frozen body","template_key":"report.completed","template_version":7,"template_content_hash":"sha256"}}`
	if _, err := host.database.Exec(`INSERT INTO _notification_events (id, workspace_id, source, source_event_id, status, payload_json, lease_owner, occurred_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "leased-event", "workspace", "test", "source", "processing", snapshotPayload, "worker", "now", "now", "now"); err != nil {
		t.Fatal(err)
	}
	command := contract.NotificationMigrationCommand{MigrationID: "migration", At: time.Date(2026, 8, 29, 2, 0, 0, 0, time.UTC)}
	if status, err := migration.SystemMigration().Freeze(t.Context(), command); err != nil || status.State != contract.NotificationMigrationFrozen {
		t.Fatalf("freeze status=%+v err=%v", status, err)
	}
	if _, _, err := binding.Publisher().PublishIntent(t.Context(), contract.NotificationIntent{}); err == nil {
		t.Fatal("frozen source accepted a publication")
	} else {
		var sdkError *notificationsdk.Error
		if !errors.As(err, &sdkError) || sdkError.Code != "notification.migration_writes_frozen" || !sdkError.Retryable {
			t.Fatalf("frozen publication error=%v", err)
		}
	}
	workers, _ := binding.LocalWorkers()
	if _, err := workers.ProcessDueInboxEvents(t.Context(), 1); err == nil {
		t.Fatal("frozen source worker accepted work")
	}
	inboxAPI, templatesAPI, deliveryAPI := binding.Inbox(), binding.Templates(), binding.Delivery()
	emptyAuthority := notificationsdk.UserAuthority{}
	frozenWrites := []func() error{
		func() error { _, err := inboxAPI.SetRead(t.Context(), emptyAuthority, "item", true); return err },
		func() error { _, err := inboxAPI.SetArchived(t.Context(), emptyAuthority, "item", true); return err },
		func() error { _, err := inboxAPI.AcknowledgeAlert(t.Context(), emptyAuthority, "item"); return err },
		func() error {
			_, err := inboxAPI.MarkAllRead(t.Context(), emptyAuthority, contract.NotificationInboxQuery{})
			return err
		},
		func() error {
			_, err := inboxAPI.SaveDelegation(t.Context(), emptyAuthority, contract.NotificationInboxDelegation{})
			return err
		},
		func() error { return inboxAPI.DeleteDelegation(t.Context(), emptyAuthority, "delegation") },
		func() error {
			_, err := inboxAPI.SaveSavedView(t.Context(), emptyAuthority, contract.NotificationInboxSavedView{})
			return err
		},
		func() error { return inboxAPI.DeleteSavedView(t.Context(), emptyAuthority, "view") },
		func() error {
			_, err := inboxAPI.SavePreference(t.Context(), emptyAuthority, contract.NotificationRecipientPreference{})
			return err
		},
		func() error {
			_, err := templatesAPI.SaveDraft(t.Context(), emptyAuthority, "template", contract.NotificationTemplate{}, "")
			return err
		},
		func() error {
			_, err := templatesAPI.RestoreVersionDraft(t.Context(), emptyAuthority, "template", 1, "")
			return err
		},
		func() error { _, err := templatesAPI.Disable(t.Context(), emptyAuthority, "template", ""); return err },
		func() error {
			_, err := templatesAPI.RequestPublication(t.Context(), emptyAuthority, "template", "", "")
			return err
		},
		func() error {
			_, err := templatesAPI.ApprovePublication(t.Context(), emptyAuthority, "publication")
			return err
		},
		func() error {
			_, err := templatesAPI.RejectPublication(t.Context(), emptyAuthority, "publication", "reason")
			return err
		},
		func() error {
			_, err := templatesAPI.CancelPublication(t.Context(), emptyAuthority, "publication")
			return err
		},
		func() error {
			_, err := deliveryAPI.SavePolicy(t.Context(), emptyAuthority, contract.NotificationDeliveryPolicy{})
			return err
		},
		func() error {
			_, err := deliveryAPI.SaveRecipientPreference(t.Context(), emptyAuthority, contract.NotificationRecipientPreference{})
			return err
		},
		func() error {
			return binding.(notificationsdk.SystemTemplateBinding).SystemTemplates().SyncPublished(t.Context(), nil)
		},
		func() error {
			_, err := binding.(notificationsdk.SystemSubjectBinding).SystemSubjects().EraseSubject(t.Context(), "workspace", "subject", nil)
			return err
		},
		func() error {
			_, err := binding.(notificationsdk.SystemRetentionBinding).SystemRetention().ProcessBatch(t.Context(), contract.NotificationRetentionBatchRequest{})
			return err
		},
	}
	for index, call := range frozenWrites {
		var sdkError *notificationsdk.Error
		if err := call(); !errors.As(err, &sdkError) || sdkError.Code != "notification.migration_writes_frozen" {
			t.Fatalf("frozen write %d error=%v", index, err)
		}
	}
	if _, err := migration.SystemMigration().Export(t.Context()); err == nil {
		t.Fatal("source exported while an active lease remained")
	}
	if _, err := host.database.Exec(`UPDATE _notification_events SET lease_owner = '' WHERE id = ?`, "leased-event"); err != nil {
		t.Fatal(err)
	}
	exported, err := migration.SystemMigration().Export(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if exported.Bundle.Source != (contract.NotificationPortableScope{WorkspaceID: "workspace", ApplicationKey: "runtime"}) || exported.Bundle.Fingerprint == "" || len(exported.Bundle.Tables) != 15 {
		t.Fatalf("export=%+v", exported)
	}
	if status, err := migration.SystemMigration().Status(t.Context()); err != nil || status.BundleFingerprint != exported.Bundle.Fingerprint || status.ActiveLeases != 0 {
		t.Fatalf("source status=%+v err=%v", status, err)
	}
	targetHost := newTestHost(t)
	targetBinding, err := NewFactory(Options{}).OpenModule(t.Context(), application, targetHost)
	if err != nil {
		t.Fatal(err)
	}
	target := targetBinding.(notificationsdk.SystemMigrationBinding).SystemMigration()
	receipt, err := target.Import(t.Context(), exported.Bundle)
	if err != nil || receipt.AlreadyPresent || receipt.Fingerprint != exported.Bundle.Fingerprint {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	repeated, err := target.Import(t.Context(), exported.Bundle)
	if err != nil || !repeated.AlreadyPresent || repeated.Fingerprint != receipt.Fingerprint {
		t.Fatalf("repeated receipt=%+v err=%v", repeated, err)
	}
	var importedSnapshot string
	if err := targetHost.database.QueryRow(`SELECT payload_json FROM _notification_events WHERE workspace_id = ? AND id = ?`, "workspace", "leased-event").Scan(&importedSnapshot); err != nil {
		t.Fatal(err)
	}
	if importedSnapshot != snapshotPayload {
		t.Fatalf("recipient snapshot changed during migration: %s", importedSnapshot)
	}
	transition := contract.NotificationMigrationCommand{MigrationID: command.MigrationID, BundleFingerprint: exported.Bundle.Fingerprint, At: command.At.Add(time.Minute)}
	if status, err := target.Activate(t.Context(), transition); err != nil || status.State != contract.NotificationMigrationCutover || status.Role != sqlstore.MigrationRoleTarget {
		t.Fatalf("target activation=%+v err=%v", status, err)
	}
	if status, err := migration.SystemMigration().Rollback(t.Context(), transition); err != nil || status.State != contract.NotificationMigrationActive || status.Role != sqlstore.MigrationRoleSource {
		t.Fatalf("source rollback=%+v err=%v", status, err)
	}
}

func TestMigrationFreezeWaitsForInFlightWriterAndFencesFollowingWrites(t *testing.T) {
	host := newTestHost(t)
	application := notificationsdk.ApplicationRef{WorkspaceID: "workspace", ApplicationKey: "runtime"}
	opened, err := NewFactory(Options{}).OpenModule(t.Context(), application, host)
	if err != nil {
		t.Fatal(err)
	}
	b := opened.(*binding)
	release, err := b.beginMigrationSensitiveWrite(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() {
		_, freezeErr := b.SystemMigration().Freeze(t.Context(), contract.NotificationMigrationCommand{MigrationID: "concurrent-freeze", At: time.Date(2026, 8, 29, 4, 0, 0, 0, time.UTC)})
		finished <- freezeErr
	}()
	select {
	case err := <-finished:
		t.Fatalf("freeze crossed an in-flight writer: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	release()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("freeze did not finish after the in-flight writer exited")
	}
	if release, err := b.beginMigrationSensitiveWrite(t.Context()); err == nil {
		release()
		t.Fatal("write crossed a completed migration freeze")
	}
}

type moduleFactoryContractAdapter struct {
	factory *Factory
	host    modulehost.Host
}

func (a moduleFactoryContractAdapter) Open(ctx context.Context, application notificationsdk.ApplicationRef) (notificationsdk.Binding, error) {
	return a.factory.OpenModule(ctx, application, a.host)
}
