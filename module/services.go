package module

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/domainry/domainry-notification"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/contract"
	"github.com/domainry/domainry-notification/delivery"
	"github.com/domainry/domainry-notification/inbox"
	"github.com/domainry/domainry-notification/sqlstore"
	"github.com/domainry/domainry-notification/template"
)

type moduleTemplates struct{ b *binding }
type moduleSystemTemplates struct{ b *binding }
type moduleSystemSubjects struct{ b *binding }
type moduleSystemRetention struct{ b *binding }
type moduleSystemMigration struct{ b *binding }

func (s moduleSystemMigration) Status(ctx context.Context) (contract.NotificationMigrationStatus, error) {
	if s.b == nil || s.b.store == nil {
		return contract.NotificationMigrationStatus{}, &notificationsdk.Error{StatusCode: 503, Code: "notification.system_migration_unavailable"}
	}
	status, err := s.b.store.MigrationStatus(ctx, s.b.application.WorkspaceID)
	if err != nil {
		return contract.NotificationMigrationStatus{}, err
	}
	return migrationStatusContract(status), nil
}

func (s moduleSystemMigration) Freeze(ctx context.Context, command contract.NotificationMigrationCommand) (contract.NotificationMigrationStatus, error) {
	if err := command.Validate(false); err != nil {
		return contract.NotificationMigrationStatus{}, err
	}
	s.b.migrationMu.Lock()
	defer s.b.migrationMu.Unlock()
	status, err := s.b.store.FreezeMigration(ctx, s.b.application.WorkspaceID, command.MigrationID, command.At)
	if err != nil {
		return contract.NotificationMigrationStatus{}, err
	}
	return migrationStatusContract(status), nil
}

func (s moduleSystemMigration) Export(ctx context.Context) (contract.NotificationPortableExport, error) {
	if s.b == nil || s.b.store == nil {
		return contract.NotificationPortableExport{}, &notificationsdk.Error{StatusCode: 503, Code: "notification.system_migration_unavailable"}
	}
	scope := sqlstore.PortableScope{TenantID: s.b.application.TenantID, WorkspaceID: s.b.application.WorkspaceID, ApplicationKey: s.b.application.ApplicationKey}
	status, err := s.b.store.MigrationStatus(ctx, s.b.application.WorkspaceID)
	if err != nil {
		return contract.NotificationPortableExport{}, err
	}
	if status.Role != sqlstore.MigrationRoleSource || status.State != sqlstore.MigrationStateFrozen || status.ActiveLeases != 0 {
		return contract.NotificationPortableExport{}, &notificationsdk.Error{StatusCode: 409, Code: "notification.migration_not_quiescent", Retryable: status.ActiveLeases != 0}
	}
	bundle, inventory, err := s.b.store.ExportPortableMigration(ctx, scope, status.MigrationID)
	if err != nil {
		return contract.NotificationPortableExport{}, err
	}
	if _, err := s.b.store.RecordMigrationFingerprint(ctx, s.b.application.WorkspaceID, status.MigrationID, bundle.Fingerprint, s.b.clock.Now()); err != nil {
		return contract.NotificationPortableExport{}, err
	}
	convertedBundle, err := convert[contract.NotificationPortableBundle](bundle)
	if err != nil {
		return contract.NotificationPortableExport{}, err
	}
	convertedInventory, err := convert[contract.NotificationPortableInventory](inventory)
	return contract.NotificationPortableExport{Bundle: convertedBundle, Inventory: convertedInventory}, err
}

func (s moduleSystemMigration) Import(ctx context.Context, bundle contract.NotificationPortableBundle) (contract.NotificationPortableImportReceipt, error) {
	if s.b == nil || s.b.store == nil {
		return contract.NotificationPortableImportReceipt{}, &notificationsdk.Error{StatusCode: 503, Code: "notification.system_migration_unavailable"}
	}
	if err := bundle.ValidateEnvelope(); err != nil {
		return contract.NotificationPortableImportReceipt{}, err
	}
	s.b.migrationMu.Lock()
	defer s.b.migrationMu.Unlock()
	scope := sqlstore.PortableScope{TenantID: s.b.application.TenantID, WorkspaceID: s.b.application.WorkspaceID, ApplicationKey: s.b.application.ApplicationKey}
	status, err := s.b.store.MigrationStatus(ctx, s.b.application.WorkspaceID)
	if err != nil {
		return contract.NotificationPortableImportReceipt{}, err
	}
	if status.Role == sqlstore.MigrationRoleTarget && status.State == sqlstore.MigrationStateImported && status.MigrationID == bundle.MigrationID && status.BundleFingerprint == bundle.Fingerprint {
		return contract.NotificationPortableImportReceipt{FormatVersion: bundle.FormatVersion, Fingerprint: bundle.Fingerprint, Rows: portableRowCount(bundle), AlreadyPresent: true}, nil
	}
	portable, err := convert[sqlstore.PortableBundle](bundle)
	if err != nil {
		return contract.NotificationPortableImportReceipt{}, err
	}
	receipt, err := s.b.store.ImportPortable(ctx, scope, portable)
	if err != nil {
		return contract.NotificationPortableImportReceipt{}, err
	}
	if _, err := s.b.store.RecordImportedMigration(ctx, s.b.application.WorkspaceID, bundle.MigrationID, bundle.Fingerprint, s.b.clock.Now()); err != nil {
		return contract.NotificationPortableImportReceipt{}, err
	}
	return convert[contract.NotificationPortableImportReceipt](receipt)
}

func (s moduleSystemMigration) Activate(ctx context.Context, command contract.NotificationMigrationCommand) (contract.NotificationMigrationStatus, error) {
	if err := command.Validate(true); err != nil {
		return contract.NotificationMigrationStatus{}, err
	}
	s.b.migrationMu.Lock()
	defer s.b.migrationMu.Unlock()
	status, err := s.b.store.ActivateMigration(ctx, s.b.application.WorkspaceID, command.MigrationID, command.BundleFingerprint, command.At)
	if err != nil {
		return contract.NotificationMigrationStatus{}, err
	}
	return migrationStatusContract(status), nil
}

func (s moduleSystemMigration) Rollback(ctx context.Context, command contract.NotificationMigrationCommand) (contract.NotificationMigrationStatus, error) {
	if err := command.Validate(true); err != nil {
		return contract.NotificationMigrationStatus{}, err
	}
	s.b.migrationMu.Lock()
	defer s.b.migrationMu.Unlock()
	status, err := s.b.store.RollbackMigration(ctx, s.b.application.WorkspaceID, command.MigrationID, command.BundleFingerprint, command.At)
	if err != nil {
		return contract.NotificationMigrationStatus{}, err
	}
	return migrationStatusContract(status), nil
}

func migrationStatusContract(status sqlstore.MigrationControl) contract.NotificationMigrationStatus {
	result := contract.NotificationMigrationStatus{MigrationID: status.MigrationID, Role: status.Role, State: contract.NotificationMigrationState(status.State), BundleFingerprint: status.BundleFingerprint, ActiveLeases: status.ActiveLeases}
	result.FrozenAt, _ = time.Parse(time.RFC3339Nano, status.FrozenAt)
	result.ActivatedAt, _ = time.Parse(time.RFC3339Nano, status.ActivatedAt)
	return result
}

func portableRowCount(bundle contract.NotificationPortableBundle) int {
	rows := 0
	for _, table := range bundle.Tables {
		rows += len(table.Rows)
	}
	return rows
}

func (s moduleSystemRetention) Preview(ctx context.Context, request contract.NotificationRetentionPreviewRequest) (contract.NotificationRetentionPreview, error) {
	if s.b == nil || s.b.store == nil {
		return contract.NotificationRetentionPreview{}, &notificationsdk.Error{StatusCode: 503, Code: "notification.system_retention_unavailable"}
	}
	return s.b.store.PreviewRetention(ctx, request)
}

func (s moduleSystemRetention) ProcessBatch(ctx context.Context, request contract.NotificationRetentionBatchRequest) (contract.NotificationRetentionBatchResult, error) {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return contract.NotificationRetentionBatchResult{}, err
	}
	defer release()
	if s.b == nil || s.b.store == nil {
		return contract.NotificationRetentionBatchResult{}, &notificationsdk.Error{StatusCode: 503, Code: "notification.system_retention_unavailable"}
	}
	return s.b.store.ProcessRetentionBatch(ctx, request)
}

func (s moduleSystemSubjects) PreviewSubject(ctx context.Context, workspaceID, subjectID string) (json.RawMessage, error) {
	if s.b == nil || s.b.store == nil {
		return nil, &notificationsdk.Error{StatusCode: 503, Code: "notification.system_subjects_unavailable"}
	}
	return s.b.store.PreviewSubject(ctx, workspaceID, subjectID)
}
func (s moduleSystemSubjects) ExportSubject(ctx context.Context, workspaceID, subjectID string) (json.RawMessage, error) {
	if s.b == nil || s.b.store == nil {
		return nil, &notificationsdk.Error{StatusCode: 503, Code: "notification.system_subjects_unavailable"}
	}
	return s.b.store.ExportSubject(ctx, workspaceID, subjectID)
}
func (s moduleSystemSubjects) EraseSubject(ctx context.Context, workspaceID, subjectID string, holds json.RawMessage) (json.RawMessage, error) {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	if s.b == nil || s.b.store == nil {
		return nil, &notificationsdk.Error{StatusCode: 503, Code: "notification.system_subjects_unavailable"}
	}
	return s.b.store.EraseSubject(ctx, workspaceID, subjectID, holds)
}

func (s moduleSystemTemplates) SyncPublished(ctx context.Context, values []contract.NotificationTemplate) error {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return err
	}
	defer release()
	if s.b == nil || s.b.store == nil {
		return &notificationsdk.Error{StatusCode: 503, Code: "notification.system_templates_unavailable"}
	}
	templates, err := convertSlice[template.Template](values)
	if err != nil {
		return err
	}
	if err := s.b.store.SyncPublished(ctx, templates); err != nil {
		return moduleError(err)
	}
	return s.b.RefreshPublished(ctx)
}

func (s moduleSystemTemplates) ListPublished(ctx context.Context) ([]contract.NotificationTemplateRecord, error) {
	if s.b == nil || s.b.store == nil {
		return nil, &notificationsdk.Error{StatusCode: 503, Code: "notification.system_templates_unavailable"}
	}
	values, err := s.b.store.List(ctx)
	if err != nil {
		return nil, moduleError(err)
	}
	return convertSlice[contract.NotificationTemplateRecord](values)
}

func (s moduleTemplates) Capabilities(ctx context.Context, a notificationsdk.UserAuthority) ([]contract.NotificationTemplateCapability, error) {
	if _, err := s.actor(ctx, a, "notification_template", "read", false); err != nil {
		return nil, err
	}
	result := make([]contract.NotificationTemplateCapability, len(s.b.templateCapabilities))
	copy(result, s.b.templateCapabilities)
	return result, nil
}

func (s moduleTemplates) actor(ctx context.Context, a notificationsdk.UserAuthority, resource, action string, reauthorize bool) (string, error) {
	p, err := s.b.authorize(ctx, a, resource, action, reauthorize)
	return p.UserID, err
}
func (s moduleTemplates) List(ctx context.Context, a notificationsdk.UserAuthority) ([]contract.NotificationTemplateRecord, error) {
	if _, err := s.actor(ctx, a, "notification_template", "read", false); err != nil {
		return nil, err
	}
	values, err := s.b.templates.List(ctx)
	if err != nil {
		return nil, err
	}
	return convertSlice[contract.NotificationTemplateRecord](values)
}
func (s moduleTemplates) Get(ctx context.Context, a notificationsdk.UserAuthority, key string) (contract.NotificationTemplateRecord, bool, error) {
	if _, err := s.actor(ctx, a, "notification_template", "read", false); err != nil {
		return contract.NotificationTemplateRecord{}, false, err
	}
	value, found, err := s.b.templates.Get(ctx, key)
	if err != nil || !found {
		return contract.NotificationTemplateRecord{}, found, err
	}
	result, err := convert[contract.NotificationTemplateRecord](value)
	return result, found, err
}
func (s moduleTemplates) ListVersions(ctx context.Context, a notificationsdk.UserAuthority, key string) ([]contract.NotificationTemplateVersion, error) {
	if _, err := s.actor(ctx, a, "notification_template", "read", false); err != nil {
		return nil, err
	}
	values, err := s.b.templates.ListVersions(ctx, key)
	if err != nil {
		return nil, err
	}
	return convertSlice[contract.NotificationTemplateVersion](values)
}
func (s moduleTemplates) SaveDraft(ctx context.Context, a notificationsdk.UserAuthority, key string, v contract.NotificationTemplate, expected string) (contract.NotificationTemplateRecord, error) {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return contract.NotificationTemplateRecord{}, err
	}
	defer release()
	actor, err := s.actor(ctx, a, "notification_template", "draft", true)
	if err != nil {
		return contract.NotificationTemplateRecord{}, err
	}
	source, err := convert[template.Template](v)
	if err != nil {
		return contract.NotificationTemplateRecord{}, err
	}
	value, err := s.b.templates.SaveDraft(ctx, key, source, expected, actor)
	if err != nil {
		return contract.NotificationTemplateRecord{}, err
	}
	return convert[contract.NotificationTemplateRecord](value)
}
func (s moduleTemplates) RestoreVersionDraft(ctx context.Context, a notificationsdk.UserAuthority, key string, version int, expected string) (contract.NotificationTemplateRecord, error) {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return contract.NotificationTemplateRecord{}, err
	}
	defer release()
	actor, err := s.actor(ctx, a, "notification_template", "draft", true)
	if err != nil {
		return contract.NotificationTemplateRecord{}, err
	}
	value, err := s.b.templates.RestoreVersionDraft(ctx, key, version, expected, actor)
	if err != nil {
		return contract.NotificationTemplateRecord{}, err
	}
	return convert[contract.NotificationTemplateRecord](value)
}
func (s moduleTemplates) Disable(ctx context.Context, a notificationsdk.UserAuthority, key, expected string) (contract.NotificationTemplateRecord, error) {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return contract.NotificationTemplateRecord{}, err
	}
	defer release()
	actor, err := s.actor(ctx, a, "notification_template", "disable", true)
	if err != nil {
		return contract.NotificationTemplateRecord{}, err
	}
	value, err := s.b.templates.Disable(ctx, key, expected, actor)
	if err != nil {
		return contract.NotificationTemplateRecord{}, err
	}
	return convert[contract.NotificationTemplateRecord](value)
}
func (s moduleTemplates) Preview(ctx context.Context, a notificationsdk.UserAuthority, key, locale string, recipients []string, variables map[string]any) (contract.RenderedNotification, error) {
	p, err := s.b.authorize(ctx, a, "notification_template", "preview", false)
	if err != nil {
		return contract.RenderedNotification{}, err
	}
	ids := make([]notification.UserID, len(recipients))
	for i := range recipients {
		ids[i] = notification.UserID(recipients[i])
	}
	value, err := s.b.templates.Preview(ctx, notification.WorkspaceID(p.WorkspaceID), key, locale, ids, variables)
	if err != nil {
		return contract.RenderedNotification{}, err
	}
	return convert[contract.RenderedNotification](value)
}
func (s moduleTemplates) PreviewTemplate(ctx context.Context, a notificationsdk.UserAuthority, v contract.NotificationTemplate, locale string, recipients []string, variables map[string]any) (contract.RenderedNotification, error) {
	p, err := s.b.authorize(ctx, a, "notification_template", "preview", false)
	if err != nil {
		return contract.RenderedNotification{}, err
	}
	source, err := convert[template.Template](v)
	if err != nil {
		return contract.RenderedNotification{}, err
	}
	ids := make([]notification.UserID, len(recipients))
	for i := range recipients {
		ids[i] = notification.UserID(recipients[i])
	}
	value, err := s.b.templates.PreviewTemplate(ctx, notification.WorkspaceID(p.WorkspaceID), source, locale, ids, variables)
	if err != nil {
		return contract.RenderedNotification{}, err
	}
	return convert[contract.RenderedNotification](value)
}
func (s moduleTemplates) ListPublicationRequests(ctx context.Context, a notificationsdk.UserAuthority, key string) ([]contract.NotificationPublicationRequest, error) {
	if _, err := s.actor(ctx, a, "notification_publication", "read", false); err != nil {
		return nil, err
	}
	values, err := s.b.publications.List(ctx, key)
	if err != nil {
		return nil, err
	}
	return convertSlice[contract.NotificationPublicationRequest](values)
}
func (s moduleTemplates) RequestPublication(ctx context.Context, a notificationsdk.UserAuthority, key, scheduled, expected string) (contract.NotificationPublicationRequest, error) {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return contract.NotificationPublicationRequest{}, err
	}
	defer release()
	actor, err := s.actor(ctx, a, "notification_publication", "request", true)
	if err != nil {
		return contract.NotificationPublicationRequest{}, err
	}
	value, err := s.b.publications.Request(ctx, key, scheduled, expected, actor)
	if err != nil {
		return contract.NotificationPublicationRequest{}, err
	}
	return convert[contract.NotificationPublicationRequest](value)
}
func (s moduleTemplates) ApprovePublication(ctx context.Context, a notificationsdk.UserAuthority, id string) (contract.NotificationPublicationRequest, error) {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return contract.NotificationPublicationRequest{}, err
	}
	defer release()
	actor, err := s.actor(ctx, a, "notification_publication", "approve", true)
	if err != nil {
		return contract.NotificationPublicationRequest{}, err
	}
	value, err := s.b.publications.Approve(ctx, id, actor)
	if err != nil {
		return contract.NotificationPublicationRequest{}, err
	}
	return convert[contract.NotificationPublicationRequest](value)
}
func (s moduleTemplates) RejectPublication(ctx context.Context, a notificationsdk.UserAuthority, id, reason string) (contract.NotificationPublicationRequest, error) {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return contract.NotificationPublicationRequest{}, err
	}
	defer release()
	actor, err := s.actor(ctx, a, "notification_publication", "reject", true)
	if err != nil {
		return contract.NotificationPublicationRequest{}, err
	}
	value, err := s.b.publications.Reject(ctx, id, actor, reason)
	if err != nil {
		return contract.NotificationPublicationRequest{}, err
	}
	return convert[contract.NotificationPublicationRequest](value)
}
func (s moduleTemplates) CancelPublication(ctx context.Context, a notificationsdk.UserAuthority, id string) (contract.NotificationPublicationRequest, error) {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return contract.NotificationPublicationRequest{}, err
	}
	defer release()
	actor, err := s.actor(ctx, a, "notification_publication", "cancel", true)
	if err != nil {
		return contract.NotificationPublicationRequest{}, err
	}
	value, err := s.b.publications.Cancel(ctx, id, actor)
	if err != nil {
		return contract.NotificationPublicationRequest{}, err
	}
	return convert[contract.NotificationPublicationRequest](value)
}

type moduleDelivery struct{ b *binding }

func (s moduleDelivery) GetPolicy(ctx context.Context, a notificationsdk.UserAuthority) (contract.NotificationDeliveryPolicy, error) {
	if _, err := s.b.authorize(ctx, a, "notification_delivery_policy", "read", false); err != nil {
		return contract.NotificationDeliveryPolicy{}, err
	}
	value, err := s.b.policy.GetPolicy(ctx)
	if err != nil {
		return contract.NotificationDeliveryPolicy{}, err
	}
	return convert[contract.NotificationDeliveryPolicy](value)
}
func (s moduleDelivery) SavePolicy(ctx context.Context, a notificationsdk.UserAuthority, v contract.NotificationDeliveryPolicy) (contract.NotificationDeliveryPolicy, error) {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return contract.NotificationDeliveryPolicy{}, err
	}
	defer release()
	p, err := s.b.authorize(ctx, a, "notification_delivery_policy", "update", true)
	if err != nil {
		return contract.NotificationDeliveryPolicy{}, err
	}
	source, err := convert[delivery.Policy](v)
	if err != nil {
		return contract.NotificationDeliveryPolicy{}, err
	}
	value, err := s.b.policy.SavePolicy(ctx, source, p.UserID)
	if err != nil {
		return contract.NotificationDeliveryPolicy{}, err
	}
	return convert[contract.NotificationDeliveryPolicy](value)
}
func (s moduleDelivery) ListRecipientPreferences(ctx context.Context, a notificationsdk.UserAuthority) ([]contract.NotificationRecipientPreference, error) {
	p, err := s.b.authorize(ctx, a, "notification_preference", "read", false)
	if err != nil {
		return nil, err
	}
	values, err := s.b.policy.ListRecipientPreferences(ctx, notification.WorkspaceID(p.WorkspaceID))
	if err != nil {
		return nil, err
	}
	return convertSlice[contract.NotificationRecipientPreference](values)
}
func (s moduleDelivery) SaveRecipientPreference(ctx context.Context, a notificationsdk.UserAuthority, v contract.NotificationRecipientPreference) (contract.NotificationRecipientPreference, error) {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return contract.NotificationRecipientPreference{}, err
	}
	defer release()
	p, err := s.b.authorize(ctx, a, "notification_preference", "update", true)
	if err != nil {
		return contract.NotificationRecipientPreference{}, err
	}
	source, err := convert[delivery.RecipientPreference](v)
	if err != nil {
		return contract.NotificationRecipientPreference{}, err
	}
	if strings.TrimSpace(source.RecipientKey) == "" {
		return contract.NotificationRecipientPreference{}, &notificationsdk.Error{StatusCode: 400, Code: "notification.preference_identity_required"}
	}
	stored, err := s.b.policy.SaveRecipientPreference(ctx, notification.WorkspaceID(p.WorkspaceID), source, p.UserID)
	if err != nil {
		return contract.NotificationRecipientPreference{}, err
	}
	return convert[contract.NotificationRecipientPreference](stored)
}
func (s moduleDelivery) Metrics(ctx context.Context, a notificationsdk.UserAuthority, since string) (contract.NotificationDeliveryMetrics, error) {
	p, err := s.b.authorize(ctx, a, "notification_governance", "read", false)
	if err != nil {
		return contract.NotificationDeliveryMetrics{}, err
	}
	if s.b.metrics == nil {
		return contract.NotificationDeliveryMetrics{}, &notificationsdk.Error{StatusCode: 503, Code: "notification.delivery_metrics_unavailable"}
	}
	return s.b.metrics.Metrics(ctx, p.WorkspaceID, since)
}

type moduleAdministration struct{ b *binding }

func (s moduleAdministration) GovernanceCatalog(ctx context.Context, a notificationsdk.UserAuthority) (contract.NotificationGovernanceCatalog, error) {
	if _, err := s.b.authorize(ctx, a, "notification_governance", "read", false); err != nil {
		return contract.NotificationGovernanceCatalog{}, err
	}
	return convert[contract.NotificationGovernanceCatalog](inbox.GovernanceCatalog{EventTypes: s.b.eventTypes, Rules: s.b.rules})
}
func (s moduleAdministration) InboxGovernanceMetrics(ctx context.Context, a notificationsdk.UserAuthority, since string) (contract.NotificationInboxGovernanceMetrics, error) {
	p, err := s.b.authorize(ctx, a, "notification_governance", "read", false)
	if err != nil {
		return contract.NotificationInboxGovernanceMetrics{}, err
	}
	value, err := s.b.mailbox.GovernanceMetrics(ctx, notification.WorkspaceID(p.WorkspaceID), since)
	if err != nil {
		return contract.NotificationInboxGovernanceMetrics{}, err
	}
	return convert[contract.NotificationInboxGovernanceMetrics](value)
}

type moduleWorkers struct{ b *binding }

func (s moduleWorkers) ProcessDuePublications(ctx context.Context, limit int) (int, error) {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return 0, err
	}
	defer release()
	return s.b.publications.ProcessDue(ctx, limit)
}
func (s moduleWorkers) ProcessPublication(ctx context.Context, locator notificationsdk.WorkLocator) (bool, error) {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	return s.b.publications.Process(ctx, locator.TaskID)
}
func (s moduleWorkers) RefreshPublished(ctx context.Context) error {
	return s.b.templates.RefreshPublished(ctx)
}
func (s moduleWorkers) ProcessDueInboxEvents(ctx context.Context, limit int) (int, error) {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return 0, err
	}
	defer release()
	return s.b.inboxProcessor.ProcessDue(ctx, limit)
}
func (s moduleWorkers) ProcessInboxEvent(ctx context.Context, locator notificationsdk.WorkLocator) (bool, error) {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	return s.b.inboxProcessor.Process(ctx, notification.WorkspaceID(locator.WorkspaceID), locator.TaskID)
}
func (s moduleWorkers) ProcessDueChannelPlans(ctx context.Context, limit int) (int, error) {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return 0, err
	}
	defer release()
	return s.b.deliveryProcessor.ProcessDue(ctx, limit)
}
func (s moduleWorkers) ProcessChannelPlan(ctx context.Context, locator notificationsdk.WorkLocator) (bool, error) {
	release, err := s.b.beginMigrationSensitiveWrite(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	return s.b.deliveryProcessor.Process(ctx, notification.WorkspaceID(locator.WorkspaceID), locator.TaskID)
}
func (b *binding) RefreshPublished(ctx context.Context) error {
	return b.templates.RefreshPublished(ctx)
}

var _ notificationsdk.Templates = moduleTemplates{}
var _ notificationsdk.Delivery = moduleDelivery{}
var _ notificationsdk.Administration = moduleAdministration{}
var _ notificationsdk.LocalWorkers = moduleWorkers{}
var _ notificationsdk.SystemTemplates = moduleSystemTemplates{}
var _ notificationsdk.SystemSubjects = moduleSystemSubjects{}
var _ notificationsdk.SystemRetention = moduleSystemRetention{}
