package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-notification/internal/domain/template/service"
)

var templateRecordColumns = []string{"template_key", "draft_json", "published_json", "published_version", "status", "updated_by", "created_at", "updated_at"}
var templateVersionColumns = []string{"template_key", "version", "payload_json", "content_hash", "published_by", "published_at"}

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
	tx, err := s.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	lookup := "SELECT COUNT(*) FROM " + s.dialect.Table("notification_template_records") + " WHERE " + s.dialect.Identifier("template_key") + " = " + s.dialect.Placeholder(1)
	if err := tx.QueryRowContext(ctx, lookup, value.Key).Scan(&exists); err != nil {
		return fmt.Errorf("inspect notification template seed: %w", err)
	}
	if exists > 0 {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode notification template seed: %w", err)
	}
	now := notification.Timestamp(s.clock.Now())
	_, err = tx.ExecContext(ctx, s.dialect.Insert("notification_template_records", templateRecordColumns), value.Key, nil, string(raw), value.Version, "active", "manifest", now, now)
	if err != nil {
		return fmt.Errorf("insert notification template seed: %w", err)
	}
	if err := s.insertTemplateVersion(ctx, tx, value, "manifest", now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) List(ctx context.Context) ([]template.Record, error) {
	query := "SELECT " + s.columns(templateRecordColumns) + " FROM " + s.dialect.Table("notification_template_records") + " ORDER BY " + s.dialect.Identifier("template_key")
	rows, err := s.database.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list notification templates: %w", err)
	}
	defer rows.Close()
	values := []template.Record{}
	for rows.Next() {
		value, scanErr := scanTemplateRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) PublishedRevision(ctx context.Context) (string, error) {
	query := "SELECT COUNT(*), MAX(" + s.dialect.Identifier("updated_at") + ") FROM " + s.dialect.Table("notification_template_records") +
		" WHERE " + s.dialect.Identifier("status") + " = " + s.dialect.Placeholder(1) + " AND " + s.dialect.Identifier("published_json") + " IS NOT NULL"
	var count int64
	var updatedAt sql.NullString
	if err := s.database.QueryRowContext(ctx, query, "active").Scan(&count, &updatedAt); err != nil {
		return "", fmt.Errorf("read published notification revision: %w", err)
	}
	return fmt.Sprintf("%d:%s", count, updatedAt.String), nil
}

func (s *Store) Get(ctx context.Context, key string) (template.Record, bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return template.Record{}, false, fmt.Errorf("notification template key is required")
	}
	query := "SELECT " + s.columns(templateRecordColumns) + " FROM " + s.dialect.Table("notification_template_records") +
		" WHERE " + s.dialect.Identifier("template_key") + " = " + s.dialect.Placeholder(1)
	value, err := scanTemplateRecord(s.database.QueryRowContext(ctx, query, key))
	if errors.Is(err, sql.ErrNoRows) {
		return template.Record{}, false, nil
	}
	return value, err == nil, err
}

func (s *Store) ListVersions(ctx context.Context, key string) ([]template.Version, error) {
	key = strings.TrimSpace(key)
	query := "SELECT " + s.columns(templateVersionColumns) + " FROM " + s.dialect.Table("notification_template_versions") +
		" WHERE " + s.dialect.Identifier("template_key") + " = " + s.dialect.Placeholder(1) + " ORDER BY " + s.dialect.Identifier("version") + " DESC"
	rows, err := s.database.QueryContext(ctx, query, key)
	if err != nil {
		return nil, fmt.Errorf("list notification template versions: %w", err)
	}
	defer rows.Close()
	values := []template.Version{}
	for rows.Next() {
		value, scanErr := scanTemplateVersion(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) GetVersion(ctx context.Context, key string, version int) (template.Version, bool, error) {
	query := "SELECT " + s.columns(templateVersionColumns) + " FROM " + s.dialect.Table("notification_template_versions") +
		" WHERE " + s.dialect.Identifier("template_key") + " = " + s.dialect.Placeholder(1) + " AND " + s.dialect.Identifier("version") + " = " + s.dialect.Placeholder(2)
	value, err := scanTemplateVersion(s.database.QueryRowContext(ctx, query, strings.TrimSpace(key), version))
	if errors.Is(err, sql.ErrNoRows) {
		return template.Version{}, false, nil
	}
	return value, err == nil, err
}

func (s *Store) SaveDraft(ctx context.Context, value template.Template, expectedUpdatedAt, actor string) (template.Record, error) {
	value.Key, actor = strings.TrimSpace(value.Key), strings.TrimSpace(actor)
	if value.Key == "" || actor == "" {
		return template.Record{}, fmt.Errorf("notification template key and actor are required")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return template.Record{}, fmt.Errorf("encode notification template draft: %w", err)
	}
	now := notification.Timestamp(s.clock.Now())
	query := "UPDATE " + s.dialect.Table("notification_template_records") + " SET " + s.dialect.Identifier("draft_json") + " = " + s.dialect.Placeholder(1) +
		", " + s.dialect.Identifier("status") + " = 'active', " + s.dialect.Identifier("updated_by") + " = " + s.dialect.Placeholder(2) +
		", " + s.dialect.Identifier("updated_at") + " = " + s.dialect.Placeholder(3) + " WHERE " + s.dialect.Identifier("template_key") + " = " + s.dialect.Placeholder(4)
	args := []any{string(raw), actor, now, value.Key}
	if expectedUpdatedAt != "" {
		query += " AND " + s.dialect.Identifier("updated_at") + " = " + s.dialect.Placeholder(5)
		args = append(args, expectedUpdatedAt)
	}
	result, err := s.database.ExecContext(ctx, query, args...)
	if err != nil {
		return template.Record{}, fmt.Errorf("update notification template draft: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return template.Record{}, err
	}
	if count == 0 && expectedUpdatedAt != "" {
		return template.Record{}, template.ErrRecordConflict
	}
	if count == 0 {
		_, err = s.database.ExecContext(ctx, s.dialect.Insert("notification_template_records", templateRecordColumns), value.Key, string(raw), nil, 0, "active", actor, now, now)
		if err != nil {
			return template.Record{}, fmt.Errorf("insert notification template draft: %w", err)
		}
	}
	stored, found, err := s.Get(ctx, value.Key)
	if err != nil {
		return template.Record{}, err
	}
	if !found {
		return template.Record{}, template.ErrRecordNotFound
	}
	return stored, nil
}

func (s *Store) Publish(ctx context.Context, value template.Template, expectedUpdatedAt, actor string) (template.Record, error) {
	value.Key, actor = strings.TrimSpace(value.Key), strings.TrimSpace(actor)
	if value.Key == "" || value.Version < 1 || actor == "" {
		return template.Record{}, fmt.Errorf("notification published template identity is required")
	}
	value.Status = "published"
	value.ContentHash = template.ContentHash(value)
	raw, err := json.Marshal(value)
	if err != nil {
		return template.Record{}, fmt.Errorf("encode published notification template: %w", err)
	}
	tx, err := s.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return template.Record{}, err
	}
	defer tx.Rollback()
	now := notification.Timestamp(s.clock.Now())
	query := "UPDATE " + s.dialect.Table("notification_template_records") + " SET " + s.dialect.Identifier("draft_json") + " = NULL, " +
		s.dialect.Identifier("published_json") + " = " + s.dialect.Placeholder(1) + ", " + s.dialect.Identifier("published_version") + " = " + s.dialect.Placeholder(2) +
		", " + s.dialect.Identifier("status") + " = 'active', " + s.dialect.Identifier("updated_by") + " = " + s.dialect.Placeholder(3) +
		", " + s.dialect.Identifier("updated_at") + " = " + s.dialect.Placeholder(4) + " WHERE " + s.dialect.Identifier("template_key") + " = " + s.dialect.Placeholder(5)
	args := []any{string(raw), value.Version, actor, now, value.Key}
	if expectedUpdatedAt != "" {
		query += " AND " + s.dialect.Identifier("updated_at") + " = " + s.dialect.Placeholder(6)
		args = append(args, expectedUpdatedAt)
	}
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return template.Record{}, fmt.Errorf("publish notification template: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return template.Record{}, err
	}
	if count != 1 {
		return template.Record{}, template.ErrRecordConflict
	}
	if err := s.insertTemplateVersion(ctx, tx, value, actor, now); err != nil {
		return template.Record{}, err
	}
	if err := tx.Commit(); err != nil {
		return template.Record{}, err
	}
	stored, found, err := s.Get(ctx, value.Key)
	if err != nil {
		return template.Record{}, err
	}
	if !found {
		return template.Record{}, template.ErrRecordNotFound
	}
	return stored, nil
}

func (s *Store) Disable(ctx context.Context, key, expectedUpdatedAt, actor string) (template.Record, error) {
	key, actor = strings.TrimSpace(key), strings.TrimSpace(actor)
	if key == "" || actor == "" {
		return template.Record{}, fmt.Errorf("notification template key and actor are required")
	}
	now := notification.Timestamp(s.clock.Now())
	query := "UPDATE " + s.dialect.Table("notification_template_records") + " SET " + s.dialect.Identifier("status") + " = 'disabled', " +
		s.dialect.Identifier("updated_by") + " = " + s.dialect.Placeholder(1) + ", " + s.dialect.Identifier("updated_at") + " = " + s.dialect.Placeholder(2) +
		" WHERE " + s.dialect.Identifier("template_key") + " = " + s.dialect.Placeholder(3)
	args := []any{actor, now, key}
	if expectedUpdatedAt != "" {
		query += " AND " + s.dialect.Identifier("updated_at") + " = " + s.dialect.Placeholder(4)
		args = append(args, expectedUpdatedAt)
	}
	result, err := s.database.ExecContext(ctx, query, args...)
	if err != nil {
		return template.Record{}, fmt.Errorf("disable notification template: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return template.Record{}, err
	}
	if count != 1 {
		if _, found, getErr := s.Get(ctx, key); getErr != nil {
			return template.Record{}, getErr
		} else if !found {
			return template.Record{}, template.ErrRecordNotFound
		}
		return template.Record{}, template.ErrRecordConflict
	}
	stored, _, err := s.Get(ctx, key)
	return stored, err
}

func (s *Store) insertTemplateVersion(ctx context.Context, executor Executor, value template.Template, actor, publishedAt string) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode notification template version: %w", err)
	}
	columns := []string{"id", "template_key", "version", "payload_json", "content_hash", "published_by", "published_at"}
	_, err = executor.ExecContext(ctx, s.dialect.Insert("notification_template_versions", columns), fmt.Sprintf("%s:%d", value.Key, value.Version), value.Key,
		value.Version, string(raw), value.ContentHash, actor, publishedAt)
	if err != nil {
		return fmt.Errorf("insert notification template version: %w", err)
	}
	return nil
}

func scanTemplateRecord(row scanner) (template.Record, error) {
	var value template.Record
	var draftJSON, publishedJSON sql.NullString
	if err := row.Scan(&value.Key, &draftJSON, &publishedJSON, &value.PublishedVersion, &value.Status, &value.UpdatedBy, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return value, err
	}
	if draftJSON.Valid {
		var draft template.Template
		if err := json.Unmarshal([]byte(draftJSON.String), &draft); err != nil {
			return value, fmt.Errorf("decode notification template draft: %w", err)
		}
		value.Draft = &draft
	}
	if publishedJSON.Valid {
		var published template.Template
		if err := json.Unmarshal([]byte(publishedJSON.String), &published); err != nil {
			return value, fmt.Errorf("decode published notification template: %w", err)
		}
		value.Published = &published
	}
	return value, nil
}

func scanTemplateVersion(row scanner) (template.Version, error) {
	var value template.Version
	var raw string
	if err := row.Scan(&value.TemplateKey, &value.Version, &raw, &value.ContentHash, &value.PublishedBy, &value.PublishedAt); err != nil {
		return value, err
	}
	if err := json.Unmarshal([]byte(raw), &value.Template); err != nil {
		return value, fmt.Errorf("decode notification template version: %w", err)
	}
	return value, nil
}
