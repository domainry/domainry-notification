package module

import (
	"context"
	"fmt"
	"strings"
	"sync"

	actioncontract "github.com/domainry/domainry-foundation/action"
	"github.com/domainry/domainry-foundation/modulecapability"
	"github.com/domainry/domainry-foundation/modulehttp"
	"github.com/domainry/domainry-foundation/mutation"
	identitysdk "github.com/domainry/domainry-identity-sdk"
	identityprincipal "github.com/domainry/domainry-identity-sdk/authorization/principal"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/contract"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	notificationapplication "github.com/domainry/domainry-notification/internal/application"
	appdelivery "github.com/domainry/domainry-notification/internal/application/delivery"
	appinbox "github.com/domainry/domainry-notification/internal/application/inbox"
	apptemplate "github.com/domainry/domainry-notification/internal/application/template"
	"github.com/domainry/domainry-notification/internal/domain/delivery/service"
	"github.com/domainry/domainry-notification/internal/domain/inbox/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-notification/internal/domain/template/service"
	sqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence"
)

type binding struct {
	application          notificationsdk.ApplicationRef
	mode                 notificationsdk.DeploymentMode
	identity             identitysdk.Binding
	principals           principalAuthenticator
	templates            *template.Manager
	publications         *apptemplate.PublicationProcessor
	engine               *template.Engine
	publisher            *inbox.Publisher
	compiler             *inbox.Compiler
	store                *sqlstore.Store
	inboxProcessor       *appinbox.Processor
	policy               *delivery.PolicyManager
	deliveryProcessor    *appdelivery.Processor
	mailbox              *inbox.MailboxManager
	actions              *inbox.ActionResolver
	catalog              *inbox.Catalog
	eventTypes           []inbox.EventType
	rules                []inbox.Rule
	metrics              modulehost.DeliveryMetrics
	clock                modulehost.Clock
	templateCapabilities []contract.NotificationTemplateCapability
	capabilities         modulecapability.Binding
	migrationMu          sync.RWMutex
	surfaces             []modulehttp.Surface
}

type principalAuthenticator interface {
	Authenticate(context.Context, string) (identitysdk.Principal, error)
}

var _ principalAuthenticator = (*identityprincipal.Resolver)(nil)

func (b *binding) Descriptor() notificationsdk.Descriptor {
	capabilities := []string{"publication", "inbox", "templates", "delivery", "administration"}
	if b.mode == notificationsdk.DeploymentModeModule {
		capabilities = append(capabilities, "local_workers")
	}
	return notificationsdk.Descriptor{ProtocolVersion: notificationsdk.CurrentProtocolVersion, Mode: b.mode, Audience: b.application.ApplicationKey, Capabilities: capabilities}
}
func (b *binding) CapabilitySummary(ctx context.Context) (modulecapability.ModuleSummary, error) {
	if b.capabilities == nil {
		return modulecapability.ModuleSummary{}, fmt.Errorf("Notification capability binding is unavailable")
	}
	return b.capabilities.CapabilitySummary(ctx)
}
func (b *binding) CapabilityCategory(ctx context.Context, key string) (modulecapability.CategoryDocument, error) {
	if b.capabilities == nil {
		return modulecapability.CategoryDocument{}, fmt.Errorf("Notification capability binding is unavailable")
	}
	return b.capabilities.CapabilityCategory(ctx, key)
}
func (b *binding) ValidateCapabilityCandidate(ctx context.Context, request modulecapability.ValidationRequest) (modulecapability.ValidationResult, error) {
	if b.capabilities == nil {
		return modulecapability.ValidationResult{}, fmt.Errorf("Notification capability binding is unavailable")
	}
	return b.capabilities.ValidateCapabilityCandidate(ctx, request)
}
func (b *binding) Publisher() notificationsdk.Publisher               { return modulePublisher{b} }
func (b *binding) Inbox() notificationsdk.Inbox                       { return moduleInbox{b} }
func (b *binding) Templates() notificationsdk.Templates               { return moduleTemplates{b} }
func (b *binding) Delivery() notificationsdk.Delivery                 { return moduleDelivery{b} }
func (b *binding) Administration() notificationsdk.Administration     { return moduleAdministration{b} }
func (b *binding) SystemTemplates() notificationsdk.SystemTemplates   { return moduleSystemTemplates{b} }
func (b *binding) SystemSubjects() notificationsdk.SystemSubjects     { return moduleSystemSubjects{b} }
func (b *binding) SystemRetention() notificationsdk.SystemRetention   { return moduleSystemRetention{b} }
func (b *binding) SystemMigration() notificationsdk.SystemMigration   { return moduleSystemMigration{b} }
func (b *binding) LocalWorkers() (notificationsdk.LocalWorkers, bool) { return moduleWorkers{b}, true }
func (b *binding) Close(context.Context) error                        { return nil }
func (b *binding) SetHTTPSurfaces(surfaces []modulehttp.Surface) {
	b.surfaces = append([]modulehttp.Surface(nil), surfaces...)
}
func (b *binding) HTTPSurfaces() []modulehttp.Surface {
	return append([]modulehttp.Surface(nil), b.surfaces...)
}

func (b *binding) AuthorizationActions() ([]actioncontract.ActionDefinition, error) {
	return notificationapplication.AuthorizationActions()
}

var _ modulehttp.Provider = (*binding)(nil)
var _ actioncontract.Provider = (*binding)(nil)

func (b *binding) authenticate(ctx context.Context, authority notificationsdk.UserAuthority) (identitysdk.Principal, error) {
	if err := authority.Validate(); err != nil {
		return identitysdk.Principal{}, err
	}
	principal, err := b.principals.Authenticate(ctx, authority.AccessToken)
	if err != nil {
		return identitysdk.Principal{}, err
	}
	if !principal.Known || strings.TrimSpace(principal.WorkspaceID) != b.application.WorkspaceID || strings.TrimSpace(principal.UserID) == "" {
		return identitysdk.Principal{}, &notificationsdk.Error{StatusCode: 403, Code: "notification.workspace_scope_mismatch"}
	}
	return principal, nil
}

func (b *binding) authorizeAction(ctx context.Context, authority notificationsdk.UserAuthority, actionKey string, dataAction identitysdk.DataAction, reauthorize bool) (identitysdk.Principal, error) {
	principal, err := b.authenticate(ctx, authority)
	if err != nil {
		return principal, err
	}
	actionKey = strings.TrimSpace(actionKey)
	separator := strings.LastIndex(actionKey, ".")
	if separator <= 0 || separator == len(actionKey)-1 || !principal.HasPermission(actionKey) {
		return identitysdk.Principal{}, &notificationsdk.Error{StatusCode: 403, Code: "notification.permission_denied"}
	}
	if !reauthorize {
		return principal, nil
	}
	facts := identitysdk.ResourceFacts{
		"tenant_id": b.application.TenantID, "workspace_id": b.application.WorkspaceID, "application_key": b.application.ApplicationKey,
	}
	decision, err := b.identity.Authorization().Reauthorize(ctx, identitysdk.DecisionRequest{
		Identity: identitysdk.RequestIdentity{Principal: principal, AccessToken: authority.AccessToken},
		Access:   identitysdk.AccessRequest{ObjectKey: actionKey[:separator], Action: actionKey[separator+1:], DataAction: dataAction},
		Facts:    facts,
	})
	if err != nil {
		return identitysdk.Principal{}, &notificationsdk.Error{StatusCode: 503, Code: "notification.identity_reauthorization_failed", Retryable: true, Cause: err}
	}
	if !decision.Allowed {
		return identitysdk.Principal{}, &notificationsdk.Error{StatusCode: 403, Code: "notification.permission_denied"}
	}
	principal.AuthorizationRevision = decision.AuthorizationRevision
	return principal, nil
}
func inboxQuery(value contract.NotificationInboxQuery, principal identitysdk.Principal, surface string) (inbox.Query, error) {
	query, err := convert[inbox.Query](value)
	if err != nil {
		return query, err
	}
	query.WorkspaceID = notification.WorkspaceID(principal.WorkspaceID)
	query.ViewerUserID = notification.UserID(principal.UserID)
	query.Surface = notification.Surface(strings.TrimSpace(surface))
	query.ReportingUserIDs = make([]notification.UserID, len(principal.ReportingUserIDs))
	for i := range principal.ReportingUserIDs {
		query.ReportingUserIDs[i] = notification.UserID(principal.ReportingUserIDs[i])
	}
	if value.TeamMemberID != "" {
		query.RecipientUserID = notification.UserID(value.TeamMemberID)
	}
	return query, nil
}
func requireSurface(authority notificationsdk.UserAuthority) error {
	if strings.TrimSpace(authority.Surface) == "" {
		return &notificationsdk.Error{StatusCode: 400, Code: "notification.surface_required"}
	}
	return nil
}

type modulePublisher struct{ b *binding }

func (b *binding) beginMigrationSensitiveWrite(ctx context.Context) (func(), error) {
	if b == nil || b.store == nil {
		return nil, &notificationsdk.Error{StatusCode: 503, Code: "notification.binding_unavailable", Retryable: true}
	}
	b.migrationMu.RLock()
	status, err := b.store.MigrationStatus(ctx, b.application.WorkspaceID)
	if err != nil {
		b.migrationMu.RUnlock()
		return nil, err
	}
	if status.State == sqlstore.MigrationStateFrozen || status.State == sqlstore.MigrationStateImported {
		b.migrationMu.RUnlock()
		return nil, &notificationsdk.Error{StatusCode: 503, Code: "notification.migration_writes_frozen", Retryable: true}
	}
	return b.migrationMu.RUnlock, nil
}

func (s modulePublisher) PublishIntent(ctx context.Context, value contract.NotificationIntent) (contract.NotificationEvent, bool, error) {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return contract.NotificationEvent{}, false, err
	}
	defer release()
	if err := value.Validate(); err != nil {
		return contract.NotificationEvent{}, false, err
	}
	if value.WorkspaceID != s.b.application.WorkspaceID {
		return contract.NotificationEvent{}, false, &notificationsdk.Error{StatusCode: 403, Code: "notification.workspace_scope_mismatch"}
	}
	source, err := convert[inbox.Intent](value)
	if err != nil {
		return contract.NotificationEvent{}, false, err
	}
	stored, created, err := s.b.publisher.PublishIntent(ctx, source)
	if err != nil {
		if mutation.IsMutationConflict(err, mutation.MutationConflictIdempotency) {
			return contract.NotificationEvent{}, false, &notificationsdk.Error{StatusCode: 409, Code: "notification.request_identity_conflict"}
		}
		return contract.NotificationEvent{}, false, err
	}
	result, err := convert[contract.NotificationEvent](stored)
	return result, created, err
}

type moduleInbox struct{ b *binding }

func (s moduleInbox) scope(ctx context.Context, a notificationsdk.UserAuthority, q contract.NotificationInboxQuery) (identitysdk.Principal, inbox.Query, error) {
	if err := requireSurface(a); err != nil {
		return identitysdk.Principal{}, inbox.Query{}, err
	}
	principal, err := s.b.authenticate(ctx, a)
	if err != nil {
		return principal, inbox.Query{}, err
	}
	query, err := inboxQuery(q, principal, a.Surface)
	if err != nil {
		return principal, query, err
	}
	if q.Scope == contract.NotificationInboxScopeDelegated {
		owners, ownerErr := s.b.mailbox.ActiveDelegatedOwnerIDs(ctx, notification.WorkspaceID(principal.WorkspaceID), notification.UserID(principal.UserID), notification.Surface(a.Surface))
		if ownerErr != nil {
			return principal, query, ownerErr
		}
		query.DelegatedUserIDs = owners
	}
	return principal, query, nil
}
func (s moduleInbox) List(ctx context.Context, a notificationsdk.UserAuthority, q contract.NotificationInboxQuery, cursor string) (contract.NotificationInboxPage, error) {
	_, query, err := s.scope(ctx, a, q)
	if err != nil {
		return contract.NotificationInboxPage{}, err
	}
	value, err := s.b.mailbox.List(ctx, query, cursor)
	if err != nil {
		return contract.NotificationInboxPage{}, err
	}
	return convert[contract.NotificationInboxPage](value)
}
func (s moduleInbox) Get(ctx context.Context, a notificationsdk.UserAuthority, id string, q contract.NotificationInboxQuery) (contract.NotificationInboxItem, error) {
	_, query, err := s.scope(ctx, a, q)
	if err != nil {
		return contract.NotificationInboxItem{}, err
	}
	value, err := s.b.mailbox.Get(ctx, query, id)
	if err != nil {
		return contract.NotificationInboxItem{}, err
	}
	return convert[contract.NotificationInboxItem](value)
}
func (s moduleInbox) Facets(ctx context.Context, a notificationsdk.UserAuthority, q contract.NotificationInboxQuery) (contract.NotificationInboxFacets, error) {
	_, query, err := s.scope(ctx, a, q)
	if err != nil {
		return contract.NotificationInboxFacets{}, err
	}
	value, err := s.b.mailbox.Facets(ctx, query)
	if err != nil {
		return contract.NotificationInboxFacets{}, err
	}
	return convert[contract.NotificationInboxFacets](value)
}
func (s moduleInbox) mine(ctx context.Context, a notificationsdk.UserAuthority) (identitysdk.Principal, inbox.Query, error) {
	return s.scope(ctx, a, contract.NotificationInboxQuery{Scope: contract.NotificationInboxScopeMine})
}
func (s moduleInbox) SetRead(ctx context.Context, a notificationsdk.UserAuthority, id string, v bool) (contract.NotificationInboxItem, error) {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return contract.NotificationInboxItem{}, err
	}
	defer release()
	_, q, err := s.mine(ctx, a)
	if err != nil {
		return contract.NotificationInboxItem{}, err
	}
	value, err := s.b.mailbox.SetRead(ctx, q, id, v)
	if err != nil {
		return contract.NotificationInboxItem{}, err
	}
	return convert[contract.NotificationInboxItem](value)
}
func (s moduleInbox) SetArchived(ctx context.Context, a notificationsdk.UserAuthority, id string, v bool) (contract.NotificationInboxItem, error) {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return contract.NotificationInboxItem{}, err
	}
	defer release()
	_, q, err := s.mine(ctx, a)
	if err != nil {
		return contract.NotificationInboxItem{}, err
	}
	value, err := s.b.mailbox.SetArchived(ctx, q, id, v)
	if err != nil {
		return contract.NotificationInboxItem{}, err
	}
	return convert[contract.NotificationInboxItem](value)
}
func (s moduleInbox) AcknowledgeAlert(ctx context.Context, a notificationsdk.UserAuthority, id string) (contract.NotificationInboxItem, error) {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return contract.NotificationInboxItem{}, err
	}
	defer release()
	p, q, err := s.mine(ctx, a)
	if err != nil {
		return contract.NotificationInboxItem{}, err
	}
	value, err := s.b.mailbox.AcknowledgeAlert(ctx, q, id, notification.UserID(p.UserID))
	if err != nil {
		return contract.NotificationInboxItem{}, err
	}
	return convert[contract.NotificationInboxItem](value)
}
func (s moduleInbox) MarkAllRead(ctx context.Context, a notificationsdk.UserAuthority, qv contract.NotificationInboxQuery) (int, error) {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return 0, err
	}
	defer release()
	_, q, err := s.scope(ctx, a, qv)
	if err != nil {
		return 0, err
	}
	return s.b.mailbox.MarkAllRead(ctx, q)
}
func (s moduleInbox) ResolveAction(ctx context.Context, a notificationsdk.UserAuthority, id, key string, qv contract.NotificationInboxQuery) (contract.NotificationInboxResolvedAction, error) {
	_, q, err := s.scope(ctx, a, qv)
	if err != nil {
		return contract.NotificationInboxResolvedAction{}, err
	}
	value, err := s.b.actions.Resolve(ctx, q, id, key)
	if err != nil {
		return contract.NotificationInboxResolvedAction{}, err
	}
	return convert[contract.NotificationInboxResolvedAction](value)
}
func (s moduleInbox) ListDelegations(ctx context.Context, a notificationsdk.UserAuthority, surface string) ([]contract.NotificationInboxDelegation, error) {
	p, err := s.b.authenticate(ctx, a)
	if err != nil {
		return nil, err
	}
	values, err := s.b.mailbox.ListDelegations(ctx, notification.WorkspaceID(p.WorkspaceID), notification.UserID(p.UserID), notification.Surface(surface))
	if err != nil {
		return nil, err
	}
	return convertSlice[contract.NotificationInboxDelegation](values)
}
func (s moduleInbox) SaveDelegation(ctx context.Context, a notificationsdk.UserAuthority, v contract.NotificationInboxDelegation) (contract.NotificationInboxDelegation, error) {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return contract.NotificationInboxDelegation{}, err
	}
	defer release()
	p, err := s.b.authenticate(ctx, a)
	if err != nil {
		return contract.NotificationInboxDelegation{}, err
	}
	v.WorkspaceID = p.WorkspaceID
	v.OwnerUserID = p.UserID
	source, err := convert[inbox.Delegation](v)
	if err != nil {
		return contract.NotificationInboxDelegation{}, err
	}
	value, err := s.b.mailbox.SaveDelegation(ctx, source)
	if err != nil {
		return contract.NotificationInboxDelegation{}, err
	}
	return convert[contract.NotificationInboxDelegation](value)
}
func (s moduleInbox) DeleteDelegation(ctx context.Context, a notificationsdk.UserAuthority, id string) error {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return err
	}
	defer release()
	p, err := s.b.authenticate(ctx, a)
	if err != nil {
		return err
	}
	return s.b.mailbox.DeleteDelegation(ctx, notification.WorkspaceID(p.WorkspaceID), notification.UserID(p.UserID), id)
}
func (s moduleInbox) ListDelegatedOwnerIDs(ctx context.Context, a notificationsdk.UserAuthority, surface string) ([]string, error) {
	p, err := s.b.authenticate(ctx, a)
	if err != nil {
		return nil, err
	}
	values, err := s.b.mailbox.ActiveDelegatedOwnerIDs(ctx, notification.WorkspaceID(p.WorkspaceID), notification.UserID(p.UserID), notification.Surface(surface))
	if err != nil {
		return nil, err
	}
	result := make([]string, len(values))
	for i := range values {
		result[i] = values[i].String()
	}
	return result, nil
}
func (s moduleInbox) ListSavedViews(ctx context.Context, a notificationsdk.UserAuthority, surface string) ([]contract.NotificationInboxSavedView, error) {
	p, err := s.b.authenticate(ctx, a)
	if err != nil {
		return nil, err
	}
	values, err := s.b.mailbox.ListSavedViews(ctx, notification.WorkspaceID(p.WorkspaceID), notification.UserID(p.UserID), notification.Surface(surface))
	if err != nil {
		return nil, err
	}
	return convertSlice[contract.NotificationInboxSavedView](values)
}
func (s moduleInbox) SaveSavedView(ctx context.Context, a notificationsdk.UserAuthority, v contract.NotificationInboxSavedView) (contract.NotificationInboxSavedView, error) {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return contract.NotificationInboxSavedView{}, err
	}
	defer release()
	p, err := s.b.authenticate(ctx, a)
	if err != nil {
		return contract.NotificationInboxSavedView{}, err
	}
	source, err := convert[inbox.SavedView](v)
	if err != nil {
		return contract.NotificationInboxSavedView{}, err
	}
	value, err := s.b.mailbox.SaveSavedView(ctx, notification.WorkspaceID(p.WorkspaceID), notification.UserID(p.UserID), notification.Surface(a.Surface), source)
	if err != nil {
		return contract.NotificationInboxSavedView{}, err
	}
	return convert[contract.NotificationInboxSavedView](value)
}
func (s moduleInbox) DeleteSavedView(ctx context.Context, a notificationsdk.UserAuthority, key string) error {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return err
	}
	defer release()
	p, err := s.b.authenticate(ctx, a)
	if err != nil {
		return err
	}
	return s.b.mailbox.DeleteSavedView(ctx, notification.WorkspaceID(p.WorkspaceID), notification.UserID(p.UserID), notification.Surface(a.Surface), key)
}
func (s moduleInbox) GetPreference(ctx context.Context, a notificationsdk.UserAuthority, surface string) (contract.NotificationRecipientPreference, error) {
	p, err := s.b.authenticate(ctx, a)
	if err != nil {
		return contract.NotificationRecipientPreference{}, err
	}
	value, found, err := s.b.policy.GetRecipientPreference(ctx, notification.WorkspaceID(p.WorkspaceID), notification.UserID(p.UserID))
	if err != nil {
		return contract.NotificationRecipientPreference{}, err
	}
	if !found {
		return contract.NotificationRecipientPreference{RecipientKey: p.UserID, EnabledChannels: map[string]bool{}}, nil
	}
	return convert[contract.NotificationRecipientPreference](value)
}
func (s moduleInbox) SavePreference(ctx context.Context, a notificationsdk.UserAuthority, surface string, v contract.NotificationRecipientPreference) (contract.NotificationRecipientPreference, error) {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return contract.NotificationRecipientPreference{}, err
	}
	defer release()
	p, err := s.b.authenticate(ctx, a)
	if err != nil {
		return contract.NotificationRecipientPreference{}, err
	}
	v.RecipientKey = p.UserID
	source, err := convert[delivery.RecipientPreference](v)
	if err != nil {
		return contract.NotificationRecipientPreference{}, err
	}
	value, err := s.b.policy.SaveRecipientPreference(ctx, notification.WorkspaceID(p.WorkspaceID), source, p.UserID)
	if err != nil {
		return contract.NotificationRecipientPreference{}, err
	}
	return convert[contract.NotificationRecipientPreference](value)
}

var _ notificationsdk.Binding = (*binding)(nil)
var _ notificationsdk.Publisher = modulePublisher{}
var _ notificationsdk.Inbox = moduleInbox{}
var _ = fmt.Sprintf
