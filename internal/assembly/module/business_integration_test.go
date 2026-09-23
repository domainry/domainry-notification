package module

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	shareddefinition "github.com/domainry/domainry-foundation/definition"
	sharedoperation "github.com/domainry/domainry-foundation/operation"
	"github.com/domainry/domainry-foundation/requestcontext"
	identitysdk "github.com/domainry/domainry-identity-sdk"
	metadatasdk "github.com/domainry/domainry-metadata-sdk"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/contract"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	notificationapplication "github.com/domainry/domainry-notification/internal/application"
	ormdialect "github.com/domainry/domainry-orm/dialect"
	_ "modernc.org/sqlite"
)

type integrationClock struct {
	mu    sync.RWMutex
	value time.Time
}

func (c *integrationClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.value
}

func (c *integrationClock) Advance(value time.Duration) {
	c.mu.Lock()
	c.value = c.value.Add(value)
	c.mu.Unlock()
}

type integrationWorkspaceScope struct {
	expected string
	mu       sync.Mutex
	seen     []string
}

func (s *integrationWorkspaceScope) Context(ctx context.Context, workspaceID string) context.Context {
	s.mu.Lock()
	s.seen = append(s.seen, workspaceID)
	s.mu.Unlock()
	if workspaceID == s.expected {
		return ctx
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	return cancelled
}

func (s *integrationWorkspaceScope) allExact() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.seen) == 0 {
		return false
	}
	for _, workspaceID := range s.seen {
		if workspaceID != s.expected {
			return false
		}
	}
	return true
}

type integrationQueueScopes struct{}

func (integrationQueueScopes) Register(ctx context.Context, executor modulehost.Executor, kind, workspaceID, updatedAt string) error {
	_, err := executor.ExecContext(ctx, `INSERT INTO host_notification_queue_scopes (kind, workspace_id, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(kind, workspace_id) DO UPDATE SET updated_at = excluded.updated_at`, kind, workspaceID, updatedAt)
	return err
}

func (integrationQueueScopes) Workspaces(ctx context.Context, queryer modulehost.Queryer, kind string, limit int) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT workspace_id FROM host_notification_queue_scopes WHERE kind = ? ORDER BY workspace_id LIMIT ?`, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []string
	for rows.Next() {
		var workspaceID string
		if err := rows.Scan(&workspaceID); err != nil {
			return nil, err
		}
		values = append(values, workspaceID)
	}
	return values, rows.Err()
}

type integrationMigrationRegistrar struct {
	database *sql.DB
	mu       sync.Mutex
	applied  int
}

func (*integrationMigrationRegistrar) Driver() string { return "sqlite" }
func (*integrationMigrationRegistrar) Schema() string { return "" }
func (r *integrationMigrationRegistrar) ApplyOwnedMigrations(ctx context.Context, owner string, migrations []modulehost.SchemaMigration) error {
	if owner != "notification" && owner != shareddefinition.MigrationOwner && owner != sharedoperation.MigrationOwner {
		return fmt.Errorf("unexpected migration owner %q", owner)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, migration := range migrations {
		var recorded string
		err := r.database.QueryRowContext(ctx, `SELECT checksum FROM _schema_migrations WHERE namespace = ? AND version = ?`, owner, migration.Version).Scan(&recorded)
		checksum := integrationMigrationChecksum(migration)
		if err == nil {
			if recorded != checksum {
				return fmt.Errorf("migration checksum mismatch for %s/%d", owner, migration.Version)
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		tx, err := r.database.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		for _, statement := range migration.Statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO _schema_migrations (namespace, version, checksum, applied_at) VALUES (?, ?, ?, ?)`, owner, migration.Version, checksum, "2026-09-03T00:00:00Z"); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		r.applied++
	}
	return nil
}

func integrationMigrationChecksum(migration modulehost.SchemaMigration) string {
	digest := sha256.New()
	_, _ = fmt.Fprintf(digest, "%d\x00%s\x00", migration.Version, migration.Name)
	for _, statement := range migration.Statements {
		_, _ = digest.Write([]byte(statement))
		_, _ = digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))
}

type integrationNotifier struct {
	mu   sync.Mutex
	work []modulehost.WorkLocator
}

func (n *integrationNotifier) Notify(_ context.Context, work modulehost.WorkLocator) {
	n.mu.Lock()
	n.work = append(n.work, work)
	n.mu.Unlock()
}

func (n *integrationNotifier) contains(kind, taskID string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, work := range n.work {
		if work.Kind == kind && work.TaskID == taskID {
			return true
		}
	}
	return false
}

type integrationRecipientResolver struct{}

func (integrationRecipientResolver) FindRecipient(_ context.Context, workspaceID, userID string) (modulehost.Recipient, bool, error) {
	if workspaceID != "workspace-a" || userID != "user-a" {
		return modulehost.Recipient{}, false, nil
	}
	return modulehost.Recipient{ID: userID, Email: "USER-A@example.test", Locale: "en-US", Timezone: "UTC"}, true, nil
}

type integrationAudience struct{}

func (integrationAudience) ResolveAudience(context.Context, string, contract.NotificationEvent) ([]string, error) {
	return nil, errors.New("unexpected audience resolution")
}

type integrationGateway struct {
	mu             sync.Mutex
	failuresBefore int
	requests       []modulehost.DeliveryRequest
}

func (g *integrationGateway) Dispatch(_ context.Context, request modulehost.DeliveryRequest) (modulehost.DeliveryReceipt, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.requests = append(g.requests, request)
	if g.failuresBefore > 0 {
		g.failuresBefore--
		return modulehost.DeliveryReceipt{}, errors.New("external gateway unavailable")
	}
	return modulehost.DeliveryReceipt{MessageID: fmt.Sprintf("outbox-%d", len(g.requests))}, nil
}

func (g *integrationGateway) snapshot() []modulehost.DeliveryRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]modulehost.DeliveryRequest(nil), g.requests...)
}

type integrationIdentityProfile struct {
	workspaceID, userID string
	actions             []string
}

type integrationIdentity struct {
	identitysdk.Binding
	clock    *integrationClock
	profiles map[string]integrationIdentityProfile
	mu       sync.Mutex
	requests []identitysdk.DecisionRequest
}

type integrationTokenVerifier struct{ identity *integrationIdentity }

func (v integrationTokenVerifier) Verify(_ context.Context, request identitysdk.VerifyTokenRequest) (identitysdk.VerifiedToken, error) {
	profile, found := v.identity.profiles[request.AccessToken]
	if !found {
		return identitysdk.VerifiedToken{}, errors.New("unknown integration token")
	}
	now := v.identity.clock.Now()
	return identitysdk.VerifiedToken{
		Issuer: "integration-identity", Audience: "runtime", SubjectID: identitysdk.SubjectID(profile.userID),
		WorkspaceID: identitysdk.WorkspaceID(profile.workspaceID), AuthorizationRevision: "revision-1", IssuedAt: now.Add(-time.Minute).Unix(),
		ExpiresAt: now.Add(time.Hour).Unix(), TokenID: request.AccessToken,
	}, nil
}

type integrationAuthentication struct {
	identitysdk.Authentication
	identity *integrationIdentity
}

func (a integrationAuthentication) CurrentSession(_ context.Context, request identitysdk.CurrentSessionRequest) (identitysdk.SessionView, error) {
	profile, found := a.identity.profiles[request.AccessToken]
	if !found {
		return identitysdk.SessionView{}, errors.New("unknown integration session")
	}
	return identitysdk.SessionView{WorkspaceID: identitysdk.WorkspaceID(profile.workspaceID), SubjectID: identitysdk.SubjectID(profile.userID), AuthorizationRevision: "revision-1", User: identitysdk.User{ID: profile.userID}}, nil
}

type integrationAuthorization struct {
	identitysdk.Authorization
	identity *integrationIdentity
}

func (a integrationAuthorization) ResolveAccess(_ context.Context, request identitysdk.AccessBundleRequest) (identitysdk.AccessBundle, error) {
	profile, found := a.identity.profiles[request.Identity.AccessToken]
	if !found {
		return identitysdk.AccessBundle{}, errors.New("unknown integration access")
	}
	return integrationAccessBundle(profile, a.identity.clock.Now()), nil
}

func (a integrationAuthorization) Reauthorize(_ context.Context, request identitysdk.DecisionRequest) (identitysdk.AccessDecision, error) {
	a.identity.mu.Lock()
	a.identity.requests = append(a.identity.requests, request)
	a.identity.mu.Unlock()
	profile, found := a.identity.profiles[request.Identity.AccessToken]
	key := strings.TrimSpace(request.Access.ObjectKey) + "." + strings.TrimSpace(request.Access.Action)
	allowed := found && containsIntegrationAction(profile.actions, key)
	return identitysdk.AccessDecision{Allowed: allowed, AuthorizationRevision: "revision-2", ObjectKey: request.Access.ObjectKey, Action: request.Access.Action}, nil
}

func (i *integrationIdentity) Tokens() identitysdk.TokenVerifier {
	return integrationTokenVerifier{identity: i}
}
func (i *integrationIdentity) Authentication() identitysdk.Authentication {
	return integrationAuthentication{identity: i}
}
func (i *integrationIdentity) Authorization() identitysdk.Authorization {
	return integrationAuthorization{identity: i}
}
func (i *integrationIdentity) Principals() identitysdk.PrincipalResolver { return i }
func (*integrationIdentity) Close(context.Context) error                 { return nil }

func (i *integrationIdentity) Resolve(ctx context.Context, request identitysdk.PrincipalResolutionRequest) (identitysdk.PrincipalResolution, error) {
	for _, profile := range i.profiles {
		if profile.workspaceID == requestcontext.WorkspaceID(ctx) && profile.userID == string(request.SubjectID) {
			bundle := integrationAccessBundle(profile, i.clock.Now())
			return identitysdk.PrincipalResolution{Principal: identitysdk.Principal{Known: true, WorkspaceID: profile.workspaceID, UserID: profile.userID}, AccessBundle: bundle}, nil
		}
	}
	return identitysdk.PrincipalResolution{}, errors.New("integration principal not found")
}

func integrationAccessBundle(profile integrationIdentityProfile, now time.Time) identitysdk.AccessBundle {
	grants := make([]identitysdk.FunctionGrant, 0, len(profile.actions))
	policies := make([]identitysdk.DataPolicy, 0, len(profile.actions))
	for _, key := range profile.actions {
		separator := strings.LastIndexByte(key, '.')
		resource, action := key[:separator], key[separator+1:]
		grants = append(grants, identitysdk.FunctionGrant{Resource: identitysdk.ResourceType(resource), Action: identitysdk.Action(action), Effect: identitysdk.EffectAllow})
		policies = append(policies, identitysdk.DataPolicy{Key: key, Resource: identitysdk.ResourceType(resource), Action: identitysdk.Action(action), Effect: identitysdk.EffectAllow, DataScopes: []identitysdk.DataScope{identitysdk.DataScopeAll}})
	}
	return identitysdk.AccessBundle{
		ContractVersion: identitysdk.CurrentPolicyBundleVersion, AuthorizationRevision: "revision-1", ExpiresAt: now.Add(time.Hour),
		Subject:        identitysdk.Subject{WorkspaceID: identitysdk.WorkspaceID(profile.workspaceID), SubjectID: identitysdk.SubjectID(profile.userID)},
		FunctionGrants: grants, DataPolicies: policies,
	}
}

func containsIntegrationAction(actions []string, expected string) bool {
	for _, action := range actions {
		if action == expected {
			return true
		}
	}
	return false
}

func (i *integrationIdentity) reauthorizationRequests() []identitysdk.DecisionRequest {
	i.mu.Lock()
	defer i.mu.Unlock()
	return append([]identitysdk.DecisionRequest(nil), i.requests...)
}

type integrationHost struct {
	database   *sql.DB
	dialect    modulehost.Dialect
	clock      *integrationClock
	scope      *integrationWorkspaceScope
	queues     integrationQueueScopes
	identity   *integrationIdentity
	migrations *integrationMigrationRegistrar
	notifier   *integrationNotifier
	gateway    *integrationGateway
	catalog    modulehost.Catalog
	archives   modulehost.RetentionArchiveStore
}

func (h *integrationHost) Database() modulehost.Database             { return h.database }
func (h *integrationHost) Dialect() modulehost.Dialect               { return h.dialect }
func (h *integrationHost) WorkspaceScope() modulehost.WorkspaceScope { return h.scope }
func (h *integrationHost) QueueScopes() modulehost.QueueScopeIndex   { return h.queues }
func (h *integrationHost) RetentionArchiveStore() modulehost.RetentionArchiveStore {
	return h.archives
}
func (h *integrationHost) Identity() identitysdk.Binding         { return h.identity }
func (h *integrationHost) Clock() modulehost.Clock               { return h.clock }
func (*integrationHost) WorkerID() string                        { return "integration-worker" }
func (h *integrationHost) Catalog() modulehost.Catalog           { return h.catalog }
func (h *integrationHost) WorkNotifier() modulehost.WorkNotifier { return h.notifier }
func (*integrationHost) RecipientResolver() modulehost.RecipientResolver {
	return integrationRecipientResolver{}
}
func (*integrationHost) AudienceResolver() modulehost.AudienceResolver { return integrationAudience{} }
func (h *integrationHost) DeliveryGateway() modulehost.DeliveryGateway { return h.gateway }
func (*integrationHost) DeliveryMetrics() modulehost.DeliveryMetrics   { return nil }
func (*integrationHost) ProviderTemplateValidator() modulehost.ProviderTemplateValidator {
	return nil
}
func (h *integrationHost) Migrations() modulehost.MigrationRegistrar { return h.migrations }

func newIntegrationHost(t *testing.T) *integrationHost {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	for _, statement := range []string{
		`CREATE TABLE _schema_migrations (namespace TEXT NOT NULL, version INTEGER NOT NULL, checksum TEXT NOT NULL, applied_at TEXT NOT NULL, PRIMARY KEY(namespace, version))`,
		`CREATE TABLE host_notification_queue_scopes (kind TEXT NOT NULL, workspace_id TEXT NOT NULL, updated_at TEXT NOT NULL, PRIMARY KEY(kind, workspace_id))`,
		`CREATE TABLE host_business_records (id TEXT PRIMARY KEY, state TEXT NOT NULL)`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	dialect, err := ormdialect.ParseRenderer("sqlite", "", "")
	if err != nil {
		t.Fatal(err)
	}
	clock := &integrationClock{value: time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)}
	fullActions := []string{
		notificationapplication.ActionDeliveryPolicyGet,
		notificationapplication.ActionDeliveryPolicyUpdate,
		notificationapplication.ActionRecipientPreferencesList,
		notificationapplication.ActionRecipientPreferencesUpdate,
		"notification.inbox.list",
	}
	identity := &integrationIdentity{clock: clock, profiles: map[string]integrationIdentityProfile{
		"full-token":      {workspaceID: "workspace-a", userID: "user-a", actions: fullActions},
		"limited-token":   {workspaceID: "workspace-a", userID: "user-a", actions: []string{"notification.inbox.list"}},
		"other-workspace": {workspaceID: "workspace-b", userID: "user-a", actions: fullActions},
	}}
	catalog := modulehost.Catalog{
		DefaultLocale: "en-US", ExternalChannels: []string{"email"},
		TemplateCapabilities: []contract.NotificationTemplateCapability{{Channel: "email", SupportsHTML: true}},
		EventTypes: []contract.NotificationEventType{{
			Key: "invoice.due", Source: "billing", Category: "billing", DefaultSeverity: "warning", MandatoryInApp: true,
			TemplateKey: "invoice.due.in_app", DefaultLocale: "en-US", Locales: map[string]contract.NotificationInboxEventTypeContent{"en-US": {Title: "Invoice {{invoice_number}} is due", Body: "Please pay invoice {{invoice_number}}."}},
			Variables: []contract.NotificationTemplateVariable{{Key: "invoice_number", Type: "text", Required: true}}, Version: 1, Status: "published",
		}},
		Rules: []contract.NotificationRule{{
			EventTypeKey: "invoice.due", Enabled: true, MandatoryInApp: true, MinimumSeverity: "warning",
			Channels: []contract.NotificationRuleChannel{{Channel: "email", TemplateKey: "invoice.due.email", ConnectorKey: "mail", ConnectionKey: "primary", Operation: "send", Mandatory: true}},
		}},
	}
	return &integrationHost{
		database: database, dialect: dialect, clock: clock, scope: &integrationWorkspaceScope{expected: "workspace-a"}, queues: integrationQueueScopes{}, identity: identity,
		migrations: &integrationMigrationRegistrar{database: database}, notifier: &integrationNotifier{}, gateway: &integrationGateway{failuresBefore: 1}, catalog: catalog,
		archives: newTestRetentionArchiveStore(t, database, dialect),
	}
}

func integrationApplication() notificationsdk.ApplicationRef {
	return notificationsdk.ApplicationRef{WorkspaceID: "workspace-a", ApplicationKey: "runtime"}
}

func integrationIntent() contract.NotificationIntent {
	return contract.NotificationIntent{
		ID: "invoice-event-1", WorkspaceID: "workspace-a", SourceEventID: "invoice-42:due", EventType: "invoice.due",
		RecipientUserIDs: []string{"user-a"}, SubjectType: "invoice", SubjectID: "invoice-42", SubjectVersion: "1", DedupeKey: "invoice-42:due",
		OccurredAt: "2026-09-03T09:59:00Z", Locale: "en-US", Variables: map[string]any{"invoice_number": "INV-42"},
	}
}

func TestPublicBindingRunsDurableNotificationLifecycleWithRetryRecovery(t *testing.T) {
	host := newIntegrationHost(t)
	binding, err := NewFactory(Options{}).OpenModule(t.Context(), integrationApplication(), host)
	if err != nil {
		t.Fatal(err)
	}
	systemTemplates := binding.(notificationsdk.SystemTemplateBinding).SystemTemplates()
	if err := systemTemplates.SyncPublished(t.Context(), []contract.NotificationTemplate{{
		Key: "invoice.due.email", Name: "Invoice due email", Channel: "email", Status: "published", Version: 1, DefaultLocale: "en-US",
		Variables: []contract.NotificationTemplateVariable{{Key: "invoice_number", Type: "text", Required: true}},
		Locales:   map[string]contract.NotificationTemplateContent{"en-US": {Subject: "Invoice {{invoice_number}}", HTML: "<p>Invoice {{invoice_number}} is due.</p>"}},
	}}); err != nil {
		t.Fatal(err)
	}
	authority := notificationsdk.UserAuthority{AccessToken: "full-token"}
	policy := contract.NotificationDeliveryPolicy{Revision: metadatasdk.DefinitionNoCurrentVersion, Enabled: true, QuietStart: "22:00", QuietEnd: "08:00", Timezone: "UTC", MaxPerRecipientPerHour: 20, DedupeWindowSeconds: 300, FallbackChannels: []string{"email"}}
	if stored, err := binding.Delivery().SavePolicy(t.Context(), authority, policy); err != nil || stored.UpdatedBy != "user-a" {
		t.Fatalf("stored policy=%+v err=%v", stored, err)
	}
	if _, err := binding.Delivery().SaveRecipientPreference(t.Context(), authority, contract.NotificationRecipientPreference{RecipientKey: "user-a", EnabledChannels: map[string]bool{"email": false}}); err != nil {
		t.Fatal(err)
	}

	intent := integrationIntent()
	event, created, err := binding.Publisher().PublishIntent(t.Context(), intent)
	if err != nil || !created || len(event.ChannelPlans) != 1 {
		t.Fatalf("event=%+v created=%v err=%v", event, created, err)
	}
	if !host.notifier.contains("notification_inbox", event.ID) {
		t.Fatalf("inbox work was not published after durable enqueue: %+v", host.notifier.work)
	}
	workers, local := binding.LocalWorkers()
	if !local || workers == nil {
		t.Fatal("Module binding did not expose local workers")
	}
	if processed, err := workers.ProcessDueInboxEvents(t.Context(), 10); err != nil || processed != 1 {
		t.Fatalf("processed inbox events=%d err=%v", processed, err)
	}
	planID := event.ChannelPlans[0].ID
	if !host.notifier.contains("notification_channel", planID) {
		t.Fatalf("channel work was not published after atomic materialization: %+v", host.notifier.work)
	}
	page, err := binding.Inbox().List(t.Context(), authority, contract.NotificationInboxQuery{Scope: contract.NotificationInboxScopeMine}, "")
	if err != nil || len(page.Items) != 1 || page.Items[0].Title != "Invoice INV-42 is due" || page.Items[0].RecipientUserID != "user-a" {
		t.Fatalf("inbox page=%+v err=%v", page, err)
	}

	if processed, err := workers.ProcessDueChannelPlans(t.Context(), 10); err != nil || processed != 0 {
		t.Fatalf("disabled preference processed=%d err=%v", processed, err)
	}
	if requests := host.gateway.snapshot(); len(requests) != 0 {
		t.Fatalf("disabled preference reached external gateway: %+v", requests)
	}
	assertIntegrationPlanState(t, host.database, planID, "queued", 1, "backend.notification.channel_plan_failed", "")

	if _, err := binding.Delivery().SaveRecipientPreference(t.Context(), authority, contract.NotificationRecipientPreference{RecipientKey: "user-a", EnabledChannels: map[string]bool{"email": true}}); err != nil {
		t.Fatal(err)
	}
	host.clock.Advance(time.Minute)
	if processed, err := workers.ProcessDueChannelPlans(t.Context(), 10); err != nil || processed != 0 {
		t.Fatalf("gateway failure processed=%d err=%v", processed, err)
	}
	assertIntegrationPlanState(t, host.database, planID, "queued", 2, "backend.notification.channel_plan_failed", "")
	host.clock.Advance(2 * time.Minute)
	if processed, err := workers.ProcessDueChannelPlans(t.Context(), 10); err != nil || processed != 1 {
		t.Fatalf("recovered delivery processed=%d err=%v", processed, err)
	}
	requests := host.gateway.snapshot()
	if len(requests) != 2 || requests[0].PlanID != planID || requests[1].PlanID != planID || requests[0].DedupeKey != planID || requests[1].DedupeKey != planID {
		t.Fatalf("gateway retries did not preserve plan identity: %+v", requests)
	}
	if requests[1].Rendered.TemplateKey != "invoice.due.email" || requests[1].Rendered.Subject != "Invoice INV-42" || len(requests[1].Rendered.Recipients) != 1 || requests[1].Rendered.Recipients[0] != "user-a@example.test" {
		t.Fatalf("gateway received an unexpected rendered snapshot: %+v", requests[1])
	}
	assertIntegrationPlanState(t, host.database, planID, "planned", 2, "", "outbox-2")
	if completed, err := workers.ProcessChannelPlan(t.Context(), notificationsdk.WorkLocator{Kind: "notification_channel", WorkspaceID: "workspace-a", TaskID: planID}); err != nil || completed {
		t.Fatalf("completed plan was dispatched again: completed=%v err=%v", completed, err)
	}
	if len(host.gateway.snapshot()) != 2 {
		t.Fatal("completed plan crossed the gateway more than once")
	}
	var reservations int
	if err := host.database.QueryRow(`SELECT COUNT(*) FROM _notification_deliveries WHERE workspace_id = ? AND row_kind = 'reservation'`, "workspace-a").Scan(&reservations); err != nil || reservations != 1 {
		t.Fatalf("delivery reservations=%d err=%v", reservations, err)
	}

	duplicate, created, err := binding.Publisher().PublishIntent(t.Context(), intent)
	if err != nil || created || duplicate.ID != event.ID {
		t.Fatalf("idempotent publish event=%+v created=%v err=%v", duplicate, created, err)
	}
	conflict := intent
	conflict.SubjectVersion = "2"
	if _, _, err := binding.Publisher().PublishIntent(t.Context(), conflict); !isIntegrationSDKError(err, 409, "notification.request_identity_conflict", false) {
		t.Fatalf("conflicting publish error=%v", err)
	}
	if !host.scope.allExact() {
		t.Fatalf("persistence crossed the host workspace scope: %+v", host.scope.seen)
	}
}

func TestPublicBindingEnforcesTenantWorkspaceAndExactActionAuthorization(t *testing.T) {
	host := newIntegrationHost(t)
	binding, err := NewFactory(Options{}).OpenModule(t.Context(), integrationApplication(), host)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"other-workspace"} {
		_, err := binding.Delivery().GetPolicy(t.Context(), notificationsdk.UserAuthority{AccessToken: token})
		if !isIntegrationSDKError(err, 403, "notification.application_scope_mismatch", false) {
			t.Fatalf("token %q scope error=%v", token, err)
		}
	}
	policy := contract.NotificationDeliveryPolicy{Revision: metadatasdk.DefinitionNoCurrentVersion, Enabled: true, QuietStart: "22:00", QuietEnd: "08:00", Timezone: "UTC", MaxPerRecipientPerHour: 20, DedupeWindowSeconds: 300}
	_, err = binding.Delivery().SavePolicy(t.Context(), notificationsdk.UserAuthority{AccessToken: "limited-token"}, policy)
	if !isIntegrationSDKError(err, 403, "notification.permission_denied", false) {
		t.Fatalf("limited token error=%v", err)
	}
	full := notificationsdk.UserAuthority{AccessToken: "full-token"}
	if _, err := binding.Delivery().SavePolicy(t.Context(), full, policy); err != nil {
		t.Fatal(err)
	}
	requests := host.identity.reauthorizationRequests()
	if len(requests) != 1 {
		t.Fatalf("reauthorization requests=%+v", requests)
	}
	request := requests[0]
	if request.Access.ObjectKey != "notification.delivery_policy" || request.Access.Action != "update" ||
		request.Facts["workspace_id"] != "workspace-a" || request.Facts["application_key"] != "runtime" {
		t.Fatalf("reauthorization was not exact and action-scoped: %+v", request)
	}
}

func TestModuleTransactionsShareHostDatabaseQueueAndMigrationLedger(t *testing.T) {
	host := newIntegrationHost(t)
	factory := NewFactory(Options{})
	binding, err := factory.OpenModule(t.Context(), integrationApplication(), host)
	if err != nil {
		t.Fatal(err)
	}
	transactional, ok := binding.(modulehost.TransactionalBinding)
	if !ok || transactional.ModuleTransactions() == nil {
		t.Fatal("Module binding did not expose shared transaction publication")
	}
	publisher := transactional.ModuleTransactions()
	compiled, err := publisher.CompileIntent(integrationIntent())
	if err != nil {
		t.Fatal(err)
	}

	tx, err := host.database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), `INSERT INTO host_business_records (id, state) VALUES (?, ?)`, "invoice-42", "due"); err != nil {
		t.Fatal(err)
	}
	if err := publisher.InsertEvent(t.Context(), tx, compiled); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if committed, err := publisher.EventCommitted(t.Context(), modulehost.EventIdentity{WorkspaceID: "workspace-a", Source: compiled.Source, SourceEventID: compiled.SourceEventID}); err != nil || committed {
		t.Fatalf("rolled back event committed=%v err=%v", committed, err)
	}
	assertIntegrationCount(t, host.database, `SELECT COUNT(*) FROM host_business_records`, 0)
	assertIntegrationCount(t, host.database, `SELECT COUNT(*) FROM host_notification_queue_scopes`, 0)

	tx, err = host.database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), `INSERT INTO host_business_records (id, state) VALUES (?, ?)`, "invoice-42", "due"); err != nil {
		t.Fatal(err)
	}
	if err := publisher.InsertEvent(t.Context(), tx, compiled); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if committed, err := publisher.EventCommitted(t.Context(), modulehost.EventIdentity{WorkspaceID: "workspace-a", Source: compiled.Source, SourceEventID: compiled.SourceEventID}); err != nil || !committed {
		t.Fatalf("committed event visible=%v err=%v", committed, err)
	}
	assertIntegrationCount(t, host.database, `SELECT COUNT(*) FROM host_business_records`, 1)
	assertIntegrationCount(t, host.database, `SELECT COUNT(*) FROM host_notification_queue_scopes WHERE kind = 'notification_inbox' AND workspace_id = 'workspace-a'`, 1)

	if _, err := factory.OpenModule(t.Context(), integrationApplication(), host); err != nil {
		t.Fatal(err)
	}
	assertIntegrationCount(t, host.database, `SELECT COUNT(*) FROM _schema_migrations WHERE namespace = 'notification'`, 1)
	assertIntegrationCount(t, host.database, `SELECT COUNT(*) FROM _schema_migrations WHERE namespace = 'shared/definitions'`, 1)
	assertIntegrationCount(t, host.database, `SELECT COUNT(*) FROM _schema_migrations WHERE namespace = 'shared/operations'`, 1)
	if host.migrations.applied != 3 {
		t.Fatalf("Module migrations replayed outside the host ledger: applied=%d", host.migrations.applied)
	}
}

func assertIntegrationPlanState(t *testing.T, database *sql.DB, planID, wantStatus string, wantAttempts int, wantCode, wantMessageID string) {
	t.Helper()
	var status, code, messageID string
	var attempts int
	if err := database.QueryRow(`SELECT status, attempt_count, last_error_code, outbox_message_id FROM _notification_deliveries WHERE workspace_id = ? AND row_kind = 'delivery' AND id = ?`, "workspace-a", planID).Scan(&status, &attempts, &code, &messageID); err != nil {
		t.Fatal(err)
	}
	if status != wantStatus || attempts != wantAttempts || code != wantCode || messageID != wantMessageID {
		t.Fatalf("plan state status=%q attempts=%d code=%q message=%q", status, attempts, code, messageID)
	}
}

func assertIntegrationCount(t *testing.T, database *sql.DB, query string, expected int) {
	t.Helper()
	var count int
	if err := database.QueryRow(query).Scan(&count); err != nil || count != expected {
		t.Fatalf("query %q count=%d expected=%d err=%v", query, count, expected, err)
	}
}

func isIntegrationSDKError(err error, status int, code string, retryable bool) bool {
	var sdkError *notificationsdk.Error
	return errors.As(err, &sdkError) && sdkError.StatusCode == status && sdkError.Code == code && sdkError.Retryable == retryable
}
