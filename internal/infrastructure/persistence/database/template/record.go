package templatestore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	metadatasdk "github.com/domainry/domainry-metadata-sdk"
	metadatamodulehost "github.com/domainry/domainry-metadata-sdk/modulehost"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-notification/internal/domain/template/service"
)

const (
	templateDefinitionKind         = "notification_template"
	templateVersionDefinitionKind  = "notification_template_version"
	templateRecordSchema           = "domainry-notification-template-record-v1"
	templatePublishedVersionSchema = "domainry-notification-template-version-v1"
	templateDefinitionSourceKind   = "notification_template"
)

func (s *Store) SyncPublished(ctx context.Context, templates []template.Template) error {
	for _, value := range templates {
		value.Key = strings.TrimSpace(value.Key)
		if value.Key == "" || value.Version < 1 {
			return fmt.Errorf("notification published template identity is required")
		}
		value.Status = "published"
		value.ContentHash = template.ContentHash(value)
		if err := s.seedPublishedTemplate(ctx, value); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) seedPublishedTemplate(ctx context.Context, value template.Template) error {
	if _, found, err := s.templateRecordDefinition(ctx, value.Key); err != nil || found {
		return err
	}
	now := notification.Timestamp(s.clock.Now())
	record := template.Record{
		Key: value.Key, Published: cloneTemplatePointer(value), PublishedVersion: value.Version,
		Status: "active", UpdatedBy: "manifest", CreatedAt: now, UpdatedAt: now,
	}
	return s.publishTemplateAndVersion(ctx, record, value, metadatasdk.DefinitionNoCurrentVersion, "manifest", now)
}

func (s *Store) List(ctx context.Context) ([]template.Record, error) {
	if s.definitions == nil {
		return nil, fmt.Errorf("notification shared Definition store is unavailable")
	}
	definitions, err := s.definitions.List(ctx, metadatasdk.DefinitionQuery{
		Owner: metadatasdk.DefinitionOwnerNotification, ResourceType: templateDefinitionKind, SourceID: s.workspaceSourceID(),
	})
	if err != nil {
		return nil, fmt.Errorf("list notification template definitions: %w", err)
	}
	values := make([]template.Record, 0, len(definitions))
	for _, definition := range definitions {
		value, err := decodeTemplateRecord(definition.Payload)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Key < values[j].Key })
	return values, nil
}

func (s *Store) PublishedRevision(ctx context.Context) (string, error) {
	values, err := s.List(ctx)
	if err != nil {
		return "", err
	}
	count := 0
	latest := ""
	for _, value := range values {
		if value.Status != "active" || value.Published == nil {
			continue
		}
		count++
		if value.UpdatedAt > latest {
			latest = value.UpdatedAt
		}
	}
	return fmt.Sprintf("%d:%s", count, latest), nil
}

func (s *Store) Get(ctx context.Context, key string) (template.Record, bool, error) {
	definition, found, err := s.templateRecordDefinition(ctx, key)
	if err != nil || !found {
		return template.Record{}, found, err
	}
	value, err := decodeTemplateRecord(definition.Payload)
	return value, err == nil, err
}

func (s *Store) ListVersions(ctx context.Context, key string) ([]template.Version, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, fmt.Errorf("notification template key is required")
	}
	if s.definitions == nil {
		return nil, fmt.Errorf("notification shared Definition store is unavailable")
	}
	definitions, err := s.definitions.List(ctx, metadatasdk.DefinitionQuery{
		Owner: metadatasdk.DefinitionOwnerNotification, ResourceType: templateVersionDefinitionKind, SourceID: s.templateDefinitionKey(key),
	})
	if err != nil {
		return nil, fmt.Errorf("list notification template version definitions: %w", err)
	}
	values := make([]template.Version, 0, len(definitions))
	for _, definition := range definitions {
		value, err := decodeTemplateVersion(definition.Payload)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Version > values[j].Version })
	return values, nil
}

func (s *Store) GetVersion(ctx context.Context, key string, version int) (template.Version, bool, error) {
	key = strings.TrimSpace(key)
	if key == "" || version < 1 {
		return template.Version{}, false, fmt.Errorf("notification template version identity is required")
	}
	if s.definitions == nil {
		return template.Version{}, false, fmt.Errorf("notification shared Definition store is unavailable")
	}
	definition, found, err := s.definitions.Get(ctx, metadatasdk.DefinitionOwnerNotification, templateVersionDefinitionKind, s.templateVersionDefinitionKey(key, version))
	if err != nil || !found {
		return template.Version{}, found, err
	}
	value, err := decodeTemplateVersion(definition.Payload)
	return value, err == nil, err
}

func (s *Store) SaveDraft(ctx context.Context, value template.Template, expectedUpdatedAt, actor string) (template.Record, error) {
	value.Key, actor = strings.TrimSpace(value.Key), strings.TrimSpace(actor)
	if value.Key == "" || actor == "" {
		return template.Record{}, fmt.Errorf("notification template key and actor are required")
	}
	currentDefinition, found, err := s.templateRecordDefinition(ctx, value.Key)
	if err != nil {
		return template.Record{}, err
	}
	now := notification.Timestamp(s.clock.Now())
	record := template.Record{Key: value.Key, Status: "active", UpdatedBy: actor, CreatedAt: now, UpdatedAt: now}
	expectedRevision := metadatasdk.DefinitionNoCurrentVersion
	if found {
		record, err = decodeTemplateRecord(currentDefinition.Payload)
		if err != nil {
			return template.Record{}, err
		}
		if expectedUpdatedAt != "" && record.UpdatedAt != expectedUpdatedAt {
			return template.Record{}, template.ErrRecordConflict
		}
		expectedRevision = currentDefinition.CurrentVersionID
	} else if expectedUpdatedAt != "" {
		return template.Record{}, template.ErrRecordConflict
	}
	record.Draft = cloneTemplatePointer(value)
	record.Status, record.UpdatedBy, record.UpdatedAt = "active", actor, now
	if err := s.publishTemplateRecord(ctx, record, expectedRevision, actor); err != nil {
		return template.Record{}, err
	}
	return record, nil
}

func (s *Store) Publish(ctx context.Context, value template.Template, expectedUpdatedAt, actor string) (template.Record, error) {
	value.Key, actor = strings.TrimSpace(value.Key), strings.TrimSpace(actor)
	if value.Key == "" || value.Version < 1 || actor == "" {
		return template.Record{}, fmt.Errorf("notification published template identity is required")
	}
	value.Status = "published"
	value.ContentHash = template.ContentHash(value)
	currentDefinition, found, err := s.templateRecordDefinition(ctx, value.Key)
	if err != nil {
		return template.Record{}, err
	}
	if !found {
		return template.Record{}, template.ErrRecordNotFound
	}
	record, err := decodeTemplateRecord(currentDefinition.Payload)
	if err != nil {
		return template.Record{}, err
	}
	if expectedUpdatedAt != "" && record.UpdatedAt != expectedUpdatedAt {
		return template.Record{}, template.ErrRecordConflict
	}
	now := notification.Timestamp(s.clock.Now())
	record.Draft, record.Published = nil, cloneTemplatePointer(value)
	record.PublishedVersion, record.Status = value.Version, "active"
	record.UpdatedBy, record.UpdatedAt = actor, now
	if err := s.publishTemplateAndVersion(ctx, record, value, currentDefinition.CurrentVersionID, actor, now); err != nil {
		return template.Record{}, err
	}
	return record, nil
}

func (s *Store) Disable(ctx context.Context, key, expectedUpdatedAt, actor string) (template.Record, error) {
	key, actor = strings.TrimSpace(key), strings.TrimSpace(actor)
	if key == "" || actor == "" {
		return template.Record{}, fmt.Errorf("notification template key and actor are required")
	}
	currentDefinition, found, err := s.templateRecordDefinition(ctx, key)
	if err != nil {
		return template.Record{}, err
	}
	if !found {
		return template.Record{}, template.ErrRecordNotFound
	}
	record, err := decodeTemplateRecord(currentDefinition.Payload)
	if err != nil {
		return template.Record{}, err
	}
	if expectedUpdatedAt != "" && record.UpdatedAt != expectedUpdatedAt {
		return template.Record{}, template.ErrRecordConflict
	}
	record.Status, record.UpdatedBy, record.UpdatedAt = "disabled", actor, notification.Timestamp(s.clock.Now())
	if err := s.publishTemplateRecord(ctx, record, currentDefinition.CurrentVersionID, actor); err != nil {
		return template.Record{}, err
	}
	return record, nil
}

func (s *Store) publishTemplateAndVersion(ctx context.Context, record template.Record, published template.Template, expectedRevision, actor, publishedAt string) error {
	if s.Database == nil {
		return fmt.Errorf("notification template database is unavailable")
	}
	tx, err := s.Database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	txContext := metadatamodulehost.WithExecutor(ctx, tx)
	if err := s.publishTemplateVersion(txContext, published, actor, publishedAt); err != nil {
		return err
	}
	if err := s.publishTemplateRecord(txContext, record, expectedRevision, actor); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) publishTemplateRecord(ctx context.Context, record template.Record, expectedRevision, actor string) error {
	if s.definitions == nil {
		return fmt.Errorf("notification shared Definition store is unavailable")
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode notification template record: %w", err)
	}
	hash := payloadHash(raw)
	_, err = s.definitions.Publish(ctx, metadatasdk.DefinitionPublishCommand{
		Owner: metadatasdk.DefinitionOwnerNotification, ResourceType: templateDefinitionKind,
		ResourceKey: s.templateDefinitionKey(record.Key), ExpectedCurrentVersionID: expectedRevision,
		SchemaVersion: templateRecordSchema + ":" + hash, SchemaHash: hash, Name: record.Key, Payload: raw,
		SourceKind: templateDefinitionSourceKind, SourceID: s.workspaceSourceID(), PublishedBy: actor,
	})
	return mapDefinitionConflict(err)
}

func (s *Store) publishTemplateVersion(ctx context.Context, value template.Template, actor, publishedAt string) error {
	if s.definitions == nil {
		return fmt.Errorf("notification shared Definition store is unavailable")
	}
	version := template.Version{TemplateKey: value.Key, Version: value.Version, Template: value, ContentHash: value.ContentHash, PublishedBy: actor, PublishedAt: publishedAt}
	key := s.templateVersionDefinitionKey(value.Key, value.Version)
	if existing, found, err := s.definitions.Get(ctx, metadatasdk.DefinitionOwnerNotification, templateVersionDefinitionKind, key); err != nil {
		return err
	} else if found {
		stored, decodeErr := decodeTemplateVersion(existing.Payload)
		if decodeErr != nil {
			return decodeErr
		}
		if stored.ContentHash == version.ContentHash {
			return nil
		}
		return template.ErrRecordConflict
	}
	raw, err := json.Marshal(version)
	if err != nil {
		return fmt.Errorf("encode notification template version: %w", err)
	}
	hash := payloadHash(raw)
	_, err = s.definitions.Publish(ctx, metadatasdk.DefinitionPublishCommand{
		Owner: metadatasdk.DefinitionOwnerNotification, ResourceType: templateVersionDefinitionKind,
		ResourceKey: key, ExpectedCurrentVersionID: metadatasdk.DefinitionNoCurrentVersion,
		SchemaVersion: templatePublishedVersionSchema + ":" + hash, SchemaHash: hash, Name: value.Key + " v" + strconv.Itoa(value.Version), Payload: raw,
		SourceKind: templateDefinitionSourceKind, SourceID: s.templateDefinitionKey(value.Key), PublishedBy: actor,
	})
	return mapDefinitionConflict(err)
}

func (s *Store) templateRecordDefinition(ctx context.Context, key string) (metadatasdk.Definition, bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return metadatasdk.Definition{}, false, fmt.Errorf("notification template key is required")
	}
	if s.definitions == nil {
		return metadatasdk.Definition{}, false, fmt.Errorf("notification shared Definition store is unavailable")
	}
	value, found, err := s.definitions.Get(ctx, metadatasdk.DefinitionOwnerNotification, templateDefinitionKind, s.templateDefinitionKey(key))
	if err != nil {
		return metadatasdk.Definition{}, false, fmt.Errorf("get notification template definition: %w", err)
	}
	return value, found, nil
}

func (s *Store) workspaceSourceID() string {
	hash := workspaceIdentityHash(s.workspaceID.String())
	return "workspace:" + hex.EncodeToString(hash[:])
}

func (s *Store) templateDefinitionKey(key string) string {
	workspaceHash := workspaceIdentityHash(s.workspaceID.String())
	templateHash := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return "workspace:" + hex.EncodeToString(workspaceHash[:8]) + ":template:" + hex.EncodeToString(templateHash[:16])
}

func (s *Store) templateVersionDefinitionKey(key string, version int) string {
	return s.templateDefinitionKey(key) + ":version:" + strconv.Itoa(version)
}

func workspaceIdentityHash(value string) [32]byte {
	return sha256.Sum256([]byte(strings.TrimSpace(value)))
}

func payloadHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func decodeTemplateRecord(raw json.RawMessage) (template.Record, error) {
	var value template.Record
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, fmt.Errorf("decode notification template record definition: %w", err)
	}
	return value, nil
}

func decodeTemplateVersion(raw json.RawMessage) (template.Version, error) {
	var value template.Version
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, fmt.Errorf("decode notification template version definition: %w", err)
	}
	return value, nil
}

func cloneTemplatePointer(value template.Template) *template.Template {
	raw, _ := json.Marshal(value)
	var cloned template.Template
	_ = json.Unmarshal(raw, &cloned)
	return &cloned
}

func mapDefinitionConflict(err error) error {
	if err == nil {
		return nil
	}
	var metadataError *metadatasdk.Error
	if errors.As(err, &metadataError) && metadataError.StatusCode == 409 {
		return template.ErrRecordConflict
	}
	return err
}
