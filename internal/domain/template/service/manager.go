package template

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

type ManagerDependencies struct {
	Store     Store
	Engine    *Engine
	Validator *Validator
}

// Manager owns synchronous template administration and reconciliation of the
// process-local published catalog. Host authorization remains outside.
type Manager struct {
	store      Store
	engine     *Engine
	validator  *Validator
	mu         sync.Mutex
	revision   string
	reconciled bool
}

func NewManager(dependencies ManagerDependencies) (*Manager, error) {
	if dependencies.Store == nil || dependencies.Engine == nil || dependencies.Validator == nil {
		return nil, fmt.Errorf("notification template manager dependencies are required")
	}
	return &Manager{store: dependencies.Store, engine: dependencies.Engine, validator: dependencies.Validator}, nil
}

func (m *Manager) List(ctx context.Context) ([]Record, error) { return m.store.List(ctx) }

func (m *Manager) Get(ctx context.Context, key string) (Record, bool, error) {
	return m.store.Get(ctx, strings.TrimSpace(key))
}

func (m *Manager) ListVersions(ctx context.Context, key string) ([]Version, error) {
	return m.store.ListVersions(ctx, strings.TrimSpace(key))
}

// RefreshPublished reconciles this process with durable publication state.
// RevisionStore avoids full reloads but is never required for correctness.
func (m *Manager) RefreshPublished(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	revision := ""
	if revisions, ok := m.store.(RevisionStore); ok {
		var err error
		revision, err = revisions.PublishedRevision(ctx)
		if err != nil {
			return err
		}
		if m.reconciled && revision == m.revision {
			return nil
		}
	}
	records, err := m.store.List(ctx)
	if err != nil {
		return err
	}
	active := map[string]bool{}
	for _, record := range records {
		if record.Status == "active" && record.Published != nil {
			active[record.Key] = true
			if err := m.engine.Upsert(*record.Published); err != nil {
				return err
			}
		}
	}
	for _, value := range m.engine.Templates() {
		if !active[value.Key] {
			m.engine.Remove(value.Key)
		}
	}
	if _, ok := m.store.(RevisionStore); ok {
		m.revision, m.reconciled = revision, true
	}
	return nil
}

func (m *Manager) SaveDraft(ctx context.Context, key string, value Template, expectedUpdatedAt, actor string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value.Key, value.Status = strings.TrimSpace(key), "draft"
	if value.Version < 1 {
		value.Version = 1
	}
	if err := m.ensureEditable(ctx, value); err != nil {
		return Record{}, err
	}
	record, err := m.store.SaveDraft(ctx, value, strings.TrimSpace(expectedUpdatedAt), strings.TrimSpace(actor))
	return record, mapRecordConflict(err, value.Key)
}

func (m *Manager) RestoreVersionDraft(ctx context.Context, key string, version int, expectedUpdatedAt, actor string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key = strings.TrimSpace(key)
	if version < 1 {
		return Record{}, invalid("backend.notification.template_version_invalid", "template_key", key)
	}
	if err := m.ensureUnlocked(ctx, key); err != nil {
		return Record{}, err
	}
	stored, found, err := m.store.GetVersion(ctx, key, version)
	if err != nil {
		return Record{}, err
	}
	if !found {
		return Record{}, notification.NewError(notification.ErrorNotFound, "backend.notification.template_version_not_found", nil, map[string]any{"template_key": key})
	}
	value := stored.Template
	value.Key, value.Status = key, "draft"
	if err := m.validator.ValidateEditable(value); err != nil {
		return Record{}, err
	}
	if err := m.validateFallbackTargets(ctx, value); err != nil {
		return Record{}, err
	}
	record, err := m.store.SaveDraft(ctx, value, strings.TrimSpace(expectedUpdatedAt), strings.TrimSpace(actor))
	return record, mapRecordConflict(err, key)
}

func (m *Manager) Publish(ctx context.Context, key, expectedUpdatedAt, actor string) (Record, error) {
	return m.publish(ctx, key, expectedUpdatedAt, actor, false)
}

func (m *Manager) publish(ctx context.Context, key, expectedUpdatedAt, actor string, allowPublicationLock bool) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.publishLocked(ctx, strings.TrimSpace(key), strings.TrimSpace(expectedUpdatedAt), strings.TrimSpace(actor), allowPublicationLock)
}

func (m *Manager) publishLocked(ctx context.Context, key, expectedUpdatedAt, actor string, allowPublicationLock bool) (Record, error) {
	if !allowPublicationLock {
		if err := m.ensureUnlocked(ctx, key); err != nil {
			return Record{}, err
		}
	}
	record, found, err := m.store.Get(ctx, key)
	if err != nil {
		return Record{}, err
	}
	if !found || record.Draft == nil {
		return Record{}, notification.NewError(notification.ErrorNotFound, "backend.notification.template_draft_not_found", nil, map[string]any{"template_key": key})
	}
	value := cloneTemplate(*record.Draft)
	value.Status = "published"
	if value.Version <= record.PublishedVersion {
		value.Version = record.PublishedVersion + 1
	}
	value.ContentHash = ContentHash(value)
	if err := m.validator.Validate(value); err != nil {
		return Record{}, err
	}
	if err := m.validateFallbackTargets(ctx, value); err != nil {
		return Record{}, err
	}
	published, err := m.store.Publish(ctx, value, expectedUpdatedAt, actor)
	if err != nil {
		return Record{}, mapRecordConflict(err, key)
	}
	if err := m.engine.Upsert(value); err != nil {
		// Durable publication already committed. RefreshPublished repairs this
		// process, and other instances remain correct through revision polling.
		return published, notification.NewError(notification.ErrorUnavailable, "backend.notification.template_catalog_refresh_failed", err, map[string]any{"template_key": key})
	}
	return published, nil
}

func (m *Manager) Disable(ctx context.Context, key, expectedUpdatedAt, actor string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key = strings.TrimSpace(key)
	if err := m.ensureUnlocked(ctx, key); err != nil {
		return Record{}, err
	}
	record, err := m.store.Disable(ctx, key, strings.TrimSpace(expectedUpdatedAt), strings.TrimSpace(actor))
	if err != nil {
		if errors.Is(err, ErrRecordNotFound) {
			return Record{}, notification.NewError(notification.ErrorNotFound, "backend.notification.template_not_found", err, map[string]any{"template_key": key})
		}
		return Record{}, mapRecordConflict(err, key)
	}
	m.engine.Remove(key)
	return record, nil
}

func (m *Manager) Preview(ctx context.Context, workspaceID notification.WorkspaceID, key, locale string, recipients []notification.UserID, variables map[string]any) (Rendered, error) {
	record, found, err := m.store.Get(ctx, strings.TrimSpace(key))
	if err != nil {
		return Rendered{}, err
	}
	if !found {
		return Rendered{}, notification.NewError(notification.ErrorNotFound, "backend.notification.template_not_found", nil, map[string]any{"template_key": key})
	}
	value := record.Draft
	if value == nil {
		value = record.Published
	}
	if value == nil {
		return Rendered{}, notification.NewError(notification.ErrorNotFound, "backend.notification.template_content_not_found", nil, map[string]any{"template_key": key})
	}
	return m.engine.RenderTemplate(ctx, *value, RenderRequest{WorkspaceID: workspaceID, TemplateKey: key, Locale: locale, Recipients: recipients, Variables: variables})
}

func (m *Manager) PreviewTemplate(ctx context.Context, workspaceID notification.WorkspaceID, value Template, locale string, recipients []notification.UserID, variables map[string]any) (Rendered, error) {
	value.Status = "draft"
	if value.Version < 1 {
		value.Version = 1
	}
	return m.engine.RenderTemplate(ctx, value, RenderRequest{WorkspaceID: workspaceID, TemplateKey: value.Key, Locale: locale, Recipients: recipients, Variables: variables})
}

func (m *Manager) ensureEditable(ctx context.Context, value Template) error {
	if err := m.ensureUnlocked(ctx, value.Key); err != nil {
		return err
	}
	if err := m.validator.ValidateEditable(value); err != nil {
		return err
	}
	return m.validateFallbackTargets(ctx, value)
}

func (m *Manager) ensureUnlocked(ctx context.Context, key string) error {
	locked, err := m.store.HasOpenPublicationRequest(ctx, key)
	if err != nil {
		return err
	}
	if locked {
		return notification.NewError(notification.ErrorConflict, "backend.notification.template_publication_locked", nil, map[string]any{"template_key": key})
	}
	return nil
}

func (m *Manager) validateFallbackTargets(ctx context.Context, value Template) error {
	for _, fallback := range value.Fallbacks {
		record, found, err := m.store.Get(ctx, strings.TrimSpace(fallback.TemplateKey))
		if err != nil {
			return err
		}
		if !found || record.Published == nil || record.Status != "active" {
			return invalid("backend.notification.fallback_template_unavailable", "template_key", value.Key, "fallback_template_key", fallback.TemplateKey)
		}
	}
	return nil
}

func mapRecordConflict(err error, key string) error {
	if errors.Is(err, ErrRecordConflict) {
		return notification.NewError(notification.ErrorConflict, "backend.notification.template_conflict", err, map[string]any{"template_key": key})
	}
	return err
}
