package saas

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/domainry/domainry-foundation/modulecapability"
	identitysdk "github.com/domainry/domainry-identity-sdk"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/contract"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	notificationremote "github.com/domainry/domainry-notification-sdk/remote"
	moduleassembly "github.com/domainry/domainry-notification/internal/assembly/module"
	sqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence"
	ormdialect "github.com/domainry/domainry-orm/dialect"
	_ "modernc.org/sqlite"
)

type inMemoryHandlerTransport struct {
	mu       sync.Mutex
	handler  http.Handler
	failPath string
	failure  error
}

func (t *inMemoryHandlerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.mu.Lock()
	fail := request.URL.Path == t.failPath
	failure := t.failure
	handler := t.handler
	t.mu.Unlock()
	if fail {
		return nil, failure
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	result := response.Result()
	result.Request = request
	return result, nil
}

func (t *inMemoryHandlerTransport) fail(path string, failure error) {
	t.mu.Lock()
	t.failPath, t.failure = path, failure
	t.mu.Unlock()
}

func (t *inMemoryHandlerTransport) recover() {
	t.mu.Lock()
	t.failPath, t.failure = "", nil
	t.mu.Unlock()
}

type parityClock struct{ value time.Time }

func (c parityClock) Now() time.Time { return c.value }

func TestModuleAndRemoteSaaSPreserveBusinessAndCapabilitySemantics(t *testing.T) {
	application := notificationsdk.ApplicationRef{TenantID: "tenant", WorkspaceID: "workspace", ApplicationKey: "runtime"}
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	catalog := modulehost.Catalog{
		DefaultLocale: "en", TemplateCapabilities: []contract.NotificationTemplateCapability{{Channel: "in_app"}},
		EventTypes: []contract.NotificationEventType{{
			Key: "report.completed", Source: "report", Category: "report", DefaultSeverity: "info", MandatoryInApp: true,
			TemplateKey: "report.completed", DefaultLocale: "en", Locales: map[string]contract.NotificationInboxEventTypeContent{"en": {Title: "Report ready", Body: "The report is ready."}}, Version: 1, Status: "published",
		}},
	}

	moduleDB := openSaaSIntegrationDatabase(t, t.Name()+"-module")
	moduleDialect, err := ormdialect.ParseRenderer("sqlite", "", "")
	if err != nil {
		t.Fatal(err)
	}
	moduleHost := &cutoverModuleHost{
		saasApplicationHost: &saasApplicationHost{
			application: application, database: moduleDB, dialect: moduleDialect, identity: applicationIdentityStub{}, catalog: catalog, clock: parityClock{value: now}, workerID: "module-worker",
			notifier: discardWorkNotifier{}, projection: identityRecipientResolver{application: application, projection: projectionStub{}}, audiences: snapshotOnlyAudienceResolver{}, gateway: applicationGatewayStub{},
		},
		migrations: &cutoverMigrationRegistrar{database: moduleDB},
	}
	moduleBinding, err := moduleassembly.NewFactory(moduleassembly.Options{}).OpenModule(t.Context(), application, moduleHost)
	if err != nil {
		t.Fatal(err)
	}

	saasDB := openSaaSIntegrationDatabase(t, t.Name()+"-saas")
	persistence, err := NewSQLPersistence(SQLPersistenceOptions{Database: saasDB, Driver: sqlstore.SQLite})
	if err != nil {
		t.Fatal(err)
	}
	saasFactory, err := NewSQLApplicationFactory(SQLApplicationFactoryOptions{Persistence: persistence, Catalog: catalog, Clock: parityClock{value: now}, WorkerID: "saas-worker", DeliveryGateway: applicationGatewayStub{}})
	if err != nil {
		t.Fatal(err)
	}
	localSaaS, err := saasFactory.OpenSaaS(t.Context(), application, applicationIdentityStub{})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(&serviceAuthenticationStub{}, &bindingResolverStub{binding: localSaaS})
	if err != nil {
		t.Fatal(err)
	}
	transport := &inMemoryHandlerTransport{handler: handler}
	moduleCapability := moduleBinding.(modulecapability.Binding)
	moduleSummary, err := moduleCapability.CapabilitySummary(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	remoteBinding, err := NewRemoteFactory(notificationremote.NewFactory(notificationremote.Config{
		BaseURL: "http://notification.test", ServiceCredential: "service", CapabilityContractSHA256: moduleSummary.Identity.ContractSHA256,
		HTTPClient: &http.Client{Transport: transport}, Retry: notificationremote.RetryPolicy{MaxAttempts: 1},
	})).Open(t.Context(), application)
	if err != nil {
		t.Fatal(err)
	}
	if moduleBinding.Descriptor().Mode != notificationsdk.DeploymentModeModule || remoteBinding.Descriptor().Mode != notificationsdk.DeploymentModeSaaS {
		t.Fatalf("descriptors module=%+v saas=%+v", moduleBinding.Descriptor(), remoteBinding.Descriptor())
	}

	intent := contract.NotificationIntent{
		ID: "report-event", WorkspaceID: "workspace", SourceEventID: "report:one", EventType: "report.completed",
		RecipientUserIDs: []string{"user"}, OccurredAt: "2026-09-03T11:59:00Z", SubjectType: "report", SubjectID: "one", SubjectVersion: "1",
	}
	moduleEvent, moduleCreated, moduleErr := moduleBinding.Publisher().PublishIntent(t.Context(), intent)
	remoteEvent, remoteCreated, remoteErr := remoteBinding.Publisher().PublishIntent(t.Context(), intent)
	if moduleErr != nil || remoteErr != nil || !moduleCreated || !remoteCreated || !reflect.DeepEqual(moduleEvent, remoteEvent) {
		t.Fatalf("publication parity module=(%+v,%v,%v) remote=(%+v,%v,%v)", moduleEvent, moduleCreated, moduleErr, remoteEvent, remoteCreated, remoteErr)
	}
	moduleDuplicate, moduleCreated, moduleErr := moduleBinding.Publisher().PublishIntent(t.Context(), intent)
	remoteDuplicate, remoteCreated, remoteErr := remoteBinding.Publisher().PublishIntent(t.Context(), intent)
	if moduleErr != nil || remoteErr != nil || moduleCreated || remoteCreated || !reflect.DeepEqual(moduleDuplicate, remoteDuplicate) {
		t.Fatalf("idempotency parity module=(%+v,%v,%v) remote=(%+v,%v,%v)", moduleDuplicate, moduleCreated, moduleErr, remoteDuplicate, remoteCreated, remoteErr)
	}
	conflict := intent
	conflict.SubjectVersion = "2"
	_, _, moduleErr = moduleBinding.Publisher().PublishIntent(t.Context(), conflict)
	_, _, remoteErr = remoteBinding.Publisher().PublishIntent(t.Context(), conflict)
	assertSaaSIntegrationErrorParity(t, moduleErr, remoteErr, http.StatusConflict, "notification.request_identity_conflict", false)
	crossWorkspace := intent
	crossWorkspace.ID, crossWorkspace.SourceEventID, crossWorkspace.WorkspaceID = "other", "report:other", "other-workspace"
	_, _, moduleErr = moduleBinding.Publisher().PublishIntent(t.Context(), crossWorkspace)
	_, _, remoteErr = remoteBinding.Publisher().PublishIntent(t.Context(), crossWorkspace)
	assertSaaSIntegrationErrorParity(t, moduleErr, remoteErr, http.StatusForbidden, "notification.application_scope_mismatch", false)

	remoteCapability, ok := remoteBinding.(modulecapability.Binding)
	if !ok {
		t.Fatal("Remote SaaS binding did not expose capability contract")
	}
	remoteSummary, err := remoteCapability.CapabilitySummary(t.Context())
	if err != nil || !reflect.DeepEqual(moduleSummary, remoteSummary) {
		t.Fatalf("capability summaries module=%+v remote=%+v err=%v", moduleSummary, remoteSummary, err)
	}
	for _, category := range moduleSummary.Categories {
		direct, directErr := moduleCapability.CapabilityCategory(t.Context(), category.Key)
		remote, remoteErr := remoteCapability.CapabilityCategory(t.Context(), category.Key)
		if directErr != nil || remoteErr != nil || !reflect.DeepEqual(direct, remote) {
			t.Fatalf("capability category %q direct=%+v err=%v remote=%+v err=%v", category.Key, direct, directErr, remote, remoteErr)
		}
	}
	invalidTemplate, _ := json.Marshal(contract.NotificationTemplate{Key: "Invalid Key", Status: "draft"})
	validation := modulecapability.ValidationRequest{
		ContractVersion: modulecapability.ValidationContractVersion, ModuleKey: "notification", CategoryKey: "notification.templates", ContractSHA256: moduleSummary.Identity.ContractSHA256,
		Kind: "notification.template", Candidate: modulecapability.AuthoringFragment{Collection: "notification_templates", Key: "invalid", Value: invalidTemplate},
	}
	directValidation, directErr := moduleCapability.ValidateCapabilityCandidate(t.Context(), validation)
	remoteValidation, remoteErr := remoteCapability.ValidateCapabilityCandidate(t.Context(), validation)
	if directErr != nil || remoteErr != nil || !reflect.DeepEqual(directValidation, remoteValidation) || len(remoteValidation.Diagnostics) == 0 {
		t.Fatalf("validation parity direct=%+v err=%v remote=%+v err=%v", directValidation, directErr, remoteValidation, remoteErr)
	}
	_, err = notificationremote.NewFactory(notificationremote.Config{
		BaseURL: "http://notification.test", ServiceCredential: "service", CapabilityContractSHA256: strings.Repeat("0", 64), HTTPClient: &http.Client{Transport: transport},
	}).Open(t.Context(), application)
	if err == nil || !strings.Contains(err.Error(), "notification.capability_contract_mismatch") {
		t.Fatalf("Remote SaaS accepted a stale capability digest: %v", err)
	}

	transport.fail("/notification/v1/events:publish", errors.New("transport disconnected"))
	uncommitted := intent
	uncommitted.ID, uncommitted.SourceEventID, uncommitted.SubjectID = "transport-event", "report:transport", "transport"
	if _, _, err := remoteBinding.Publisher().PublishIntent(t.Context(), uncommitted); !isSaaSIntegrationSDKError(err, http.StatusServiceUnavailable, "notification.remote_unavailable", true) {
		t.Fatalf("transport error=%v", err)
	}
	transport.recover()
	var rows int
	if err := saasDB.QueryRow(`SELECT COUNT(*) FROM _notification_events WHERE workspace_id = ? AND source_event_id = ?`, application.WorkspaceID, uncommitted.SourceEventID).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("transport failure committed rows=%d err=%v", rows, err)
	}
}

func openSaaSIntegrationDatabase(t *testing.T, name string) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func assertSaaSIntegrationErrorParity(t *testing.T, direct, remote error, status int, code string, retryable bool) {
	t.Helper()
	if !isSaaSIntegrationSDKError(direct, status, code, retryable) || !isSaaSIntegrationSDKError(remote, status, code, retryable) {
		t.Fatalf("error parity direct=%v remote=%v", direct, remote)
	}
}

func isSaaSIntegrationSDKError(err error, status int, code string, retryable bool) bool {
	var sdkError *notificationsdk.Error
	return errors.As(err, &sdkError) && sdkError.StatusCode == status && sdkError.Code == code && sdkError.Retryable == retryable
}

var _ http.RoundTripper = (*inMemoryHandlerTransport)(nil)
var _ modulehost.Clock = parityClock{}
var _ identitysdk.Binding = applicationIdentityStub{}
