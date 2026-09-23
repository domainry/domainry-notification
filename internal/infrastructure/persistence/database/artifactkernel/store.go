// Package artifactkernel provides Notification's standalone adapter for the
// deployment-wide shared Artifact tables. Embedded Notification receives the
// same contract from its host and never installs a private archive table.
package artifactkernel

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	sharedartifact "github.com/domainry/domainry-foundation/artifact"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	"github.com/domainry/domainry-orm/query"
	ormschema "github.com/domainry/domainry-orm/schema"
)

const artifactTable = "_artifacts"
const bindingTable = "_artifact_bindings"

type Store struct {
	database modulehost.Database
	renderer modulehost.Dialect
}

func NewStore(database modulehost.Database, renderer modulehost.Dialect) (Store, error) {
	if database == nil || renderer == nil {
		return Store{}, fmt.Errorf("shared Artifact SQL store is incomplete")
	}
	return Store{database: database, renderer: renderer}, nil
}

func (s Store) executor(ctx context.Context) sharedartifact.Executor {
	return sharedartifact.ExecutorFromContext(ctx, s.database)
}

func (s Store) Register(ctx context.Context, value sharedartifact.Artifact) (sharedartifact.Artifact, bool, error) {
	value = normalizeArtifact(value)
	if err := validateArtifact(value); err != nil {
		return sharedartifact.Artifact{}, false, err
	}
	statement, arguments, err := query.NewWorkspaceInsertBuilder(s.renderer, artifactTable, value.WorkspaceID).
		Columns(artifactColumns()[1:]...).Values(artifactValues(value)[1:]...).Build()
	if err != nil {
		return sharedartifact.Artifact{}, false, err
	}
	if _, err = s.executor(ctx).ExecContext(ctx, statement, arguments...); err == nil {
		return value, true, nil
	}
	existing, found, readErr := s.find(ctx, value.WorkspaceID, query.And(query.Equal("owner", value.Owner), query.Equal("kind", value.Kind), query.Equal("idempotency_key", value.IdempotencyKey)))
	if readErr != nil {
		return sharedartifact.Artifact{}, false, readErr
	}
	if !found || !sameArtifact(existing, value) {
		return sharedartifact.Artifact{}, false, sharedartifact.ErrIdentityConflict
	}
	return existing, false, nil
}

func (s Store) ByID(ctx context.Context, workspaceID, id string) (sharedartifact.Artifact, bool, error) {
	return s.find(ctx, workspaceID, query.Equal("id", strings.TrimSpace(id)))
}

func (s Store) ByDownloadTokenHash(ctx context.Context, workspaceID, token string) (sharedartifact.Artifact, bool, error) {
	return s.find(ctx, workspaceID, query.Equal("download_token_sha256", strings.TrimSpace(token)))
}

func (s Store) Transition(ctx context.Context, workspaceID, id string, expected, next sharedartifact.Status, scan sharedartifact.ScanStatus, at time.Time) (bool, error) {
	workspaceID, err := normalizeWorkspace(workspaceID)
	if err != nil || strings.TrimSpace(id) == "" || !validStatus(expected) || !validStatus(next) || !validScan(scan) || at.IsZero() {
		return false, fmt.Errorf("artifact transition is invalid")
	}
	statement, arguments, err := query.NewWorkspaceUpdateBuilder(s.renderer, artifactTable, workspaceID).
		Set("status", string(next)).Set("scan_status", string(scan)).Set("updated_at", at.UTC().Format(time.RFC3339Nano)).
		Where(query.And(query.Equal("id", strings.TrimSpace(id)), query.Equal("status", string(expected)))).Build()
	if err != nil {
		return false, err
	}
	result, err := s.executor(ctx).ExecContext(ctx, statement, arguments...)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed == 1, err
}

func (s Store) Bind(ctx context.Context, value sharedartifact.Binding) (sharedartifact.Binding, bool, error) {
	value = normalizeBinding(value)
	if err := validateBinding(value); err != nil {
		return sharedartifact.Binding{}, false, err
	}
	if _, found, err := s.ByID(ctx, value.WorkspaceID, value.ArtifactID); err != nil {
		return sharedartifact.Binding{}, false, err
	} else if !found {
		return sharedartifact.Binding{}, false, fmt.Errorf("artifact binding references an unknown artifact")
	}
	statement, arguments, err := query.NewWorkspaceInsertBuilder(s.renderer, bindingTable, value.WorkspaceID).
		Columns(bindingColumns()[1:]...).Values(bindingValues(value)[1:]...).Build()
	if err != nil {
		return sharedartifact.Binding{}, false, err
	}
	if _, err = s.executor(ctx).ExecContext(ctx, statement, arguments...); err == nil {
		return value, true, nil
	}
	existing, found, readErr := s.bindingByIdentity(ctx, value)
	if readErr != nil {
		return sharedartifact.Binding{}, false, readErr
	}
	if !found || existing.ArtifactID != value.ArtifactID {
		return sharedartifact.Binding{}, false, sharedartifact.ErrBindingConflict
	}
	return existing, false, nil
}

func (s Store) Bindings(ctx context.Context, workspaceID, artifactID string) ([]sharedartifact.Binding, error) {
	workspaceID, err := normalizeWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	statement, arguments, err := query.NewWorkspaceSelectBuilder(s.renderer, bindingTable, workspaceID).
		Columns(bindingColumns()...).Where(query.Equal("artifact_id", strings.TrimSpace(artifactID))).
		OrderBy(query.Ascending("created_at"), query.Ascending("id")).Build()
	if err != nil {
		return nil, err
	}
	rows, err := s.executor(ctx).QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []sharedartifact.Binding{}
	for rows.Next() {
		value, scanErr := scanBinding(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (s Store) find(ctx context.Context, workspaceID string, predicate query.Predicate) (sharedartifact.Artifact, bool, error) {
	workspaceID, err := normalizeWorkspace(workspaceID)
	if err != nil {
		return sharedartifact.Artifact{}, false, err
	}
	statement, arguments, err := query.NewWorkspaceSelectBuilder(s.renderer, artifactTable, workspaceID).
		Columns(artifactColumns()...).Where(predicate).Limit(1).Build()
	if err != nil {
		return sharedartifact.Artifact{}, false, err
	}
	value, err := scanArtifact(s.executor(ctx).QueryRowContext(ctx, statement, arguments...))
	if errors.Is(err, sql.ErrNoRows) {
		return sharedartifact.Artifact{}, false, nil
	}
	return value, err == nil, err
}

func (s Store) bindingByIdentity(ctx context.Context, value sharedartifact.Binding) (sharedartifact.Binding, bool, error) {
	statement, arguments, err := query.NewWorkspaceSelectBuilder(s.renderer, bindingTable, value.WorkspaceID).
		Columns(bindingColumns()...).Where(query.And(query.Equal("artifact_id", value.ArtifactID), query.Equal("owner", value.Owner), query.Equal("kind", value.Kind), query.Equal("resource_type", value.ResourceType), query.Equal("resource_id", value.ResourceID), query.Equal("field_key", value.FieldKey))).Limit(1).Build()
	if err != nil {
		return sharedartifact.Binding{}, false, err
	}
	existing, err := scanBinding(s.executor(ctx).QueryRowContext(ctx, statement, arguments...))
	if errors.Is(err, sql.ErrNoRows) {
		return sharedartifact.Binding{}, false, nil
	}
	return existing, err == nil, err
}

func artifactColumns() []string {
	return []string{"workspace_id", "id", "owner", "kind", "idempotency_key", "created_by", "owner_org_id", "filename", "media_type", "content_sha256", "size_bytes", "storage_reference", "status", "expires_at", "scan_status", "download_token_sha256", "authorization_scope_sha256", "metadata_json", "created_at", "updated_at"}
}

func artifactValues(value sharedartifact.Artifact) []any {
	return []any{value.WorkspaceID, value.ID, value.Owner, value.Kind, value.IdempotencyKey, value.CreatedBy, value.OwnerOrgID, value.Filename, value.MediaType, value.ContentSHA256, value.SizeBytes, value.StorageReference, string(value.Status), optionalTime(value.ExpiresAt), string(value.ScanStatus), value.DownloadTokenSHA256, value.AuthorizationScopeSHA256, string(value.Metadata), value.CreatedAt.UTC().Format(time.RFC3339Nano), value.UpdatedAt.UTC().Format(time.RFC3339Nano)}
}

func bindingColumns() []string {
	return []string{"workspace_id", "id", "artifact_id", "owner", "kind", "resource_type", "resource_id", "field_key", "metadata_json", "created_at"}
}

func bindingValues(value sharedartifact.Binding) []any {
	return []any{value.WorkspaceID, value.ID, value.ArtifactID, value.Owner, value.Kind, value.ResourceType, value.ResourceID, value.FieldKey, string(value.Metadata), value.CreatedAt.UTC().Format(time.RFC3339Nano)}
}

type scanner interface{ Scan(...any) error }

func scanArtifact(row scanner) (sharedartifact.Artifact, error) {
	var value sharedartifact.Artifact
	var status, scanStatus, metadata, createdAt, updatedAt, expiresAt string
	err := row.Scan(&value.WorkspaceID, &value.ID, &value.Owner, &value.Kind, &value.IdempotencyKey, &value.CreatedBy, &value.OwnerOrgID, &value.Filename, &value.MediaType, &value.ContentSHA256, &value.SizeBytes, &value.StorageReference, &status, &expiresAt, &scanStatus, &value.DownloadTokenSHA256, &value.AuthorizationScopeSHA256, &metadata, &createdAt, &updatedAt)
	if err != nil {
		return value, err
	}
	value.Status, value.ScanStatus, value.Metadata = sharedartifact.Status(status), sharedartifact.ScanStatus(scanStatus), json.RawMessage(metadata)
	if value.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return value, err
	}
	if value.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		return value, err
	}
	if expiresAt != "" {
		value.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresAt)
	}
	return value, err
}

func scanBinding(row scanner) (sharedartifact.Binding, error) {
	var value sharedartifact.Binding
	var metadata, createdAt string
	err := row.Scan(&value.WorkspaceID, &value.ID, &value.ArtifactID, &value.Owner, &value.Kind, &value.ResourceType, &value.ResourceID, &value.FieldKey, &metadata, &createdAt)
	if err != nil {
		return value, err
	}
	value.Metadata = json.RawMessage(metadata)
	value.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	return value, err
}

func normalizeArtifact(value sharedartifact.Artifact) sharedartifact.Artifact {
	value.ID, value.WorkspaceID, value.Owner, value.Kind = strings.TrimSpace(value.ID), strings.TrimSpace(value.WorkspaceID), strings.TrimSpace(value.Owner), strings.TrimSpace(value.Kind)
	value.IdempotencyKey, value.CreatedBy, value.OwnerOrgID = strings.TrimSpace(value.IdempotencyKey), strings.TrimSpace(value.CreatedBy), strings.TrimSpace(value.OwnerOrgID)
	value.Filename, value.MediaType, value.ContentSHA256, value.StorageReference = strings.TrimSpace(value.Filename), strings.TrimSpace(value.MediaType), strings.TrimSpace(value.ContentSHA256), strings.TrimSpace(value.StorageReference)
	value.DownloadTokenSHA256, value.AuthorizationScopeSHA256 = strings.TrimSpace(value.DownloadTokenSHA256), strings.TrimSpace(value.AuthorizationScopeSHA256)
	if len(bytes.TrimSpace(value.Metadata)) == 0 {
		value.Metadata = json.RawMessage(`{}`)
	}
	return value
}

func validateArtifact(value sharedartifact.Artifact) error {
	if _, err := normalizeWorkspace(value.WorkspaceID); err != nil {
		return err
	}
	if _, found := sharedartifact.RegistrationFor(value.Owner, value.Kind); !found {
		return fmt.Errorf("artifact owner and kind are not registered")
	}
	if value.ID == "" || value.IdempotencyKey == "" || value.CreatedBy == "" || value.Filename == "" || value.MediaType == "" || value.ContentSHA256 == "" || value.StorageReference == "" || value.SizeBytes < 0 || !validStatus(value.Status) || !validScan(value.ScanStatus) || value.CreatedAt.IsZero() || !value.UpdatedAt.Equal(value.CreatedAt) || !json.Valid(value.Metadata) {
		return fmt.Errorf("artifact identity, content, or state is invalid")
	}
	return nil
}

func normalizeBinding(value sharedartifact.Binding) sharedartifact.Binding {
	value.ID, value.WorkspaceID, value.ArtifactID = strings.TrimSpace(value.ID), strings.TrimSpace(value.WorkspaceID), strings.TrimSpace(value.ArtifactID)
	value.Owner, value.Kind, value.ResourceType, value.ResourceID, value.FieldKey = strings.TrimSpace(value.Owner), strings.TrimSpace(value.Kind), strings.TrimSpace(value.ResourceType), strings.TrimSpace(value.ResourceID), strings.TrimSpace(value.FieldKey)
	if len(bytes.TrimSpace(value.Metadata)) == 0 {
		value.Metadata = json.RawMessage(`{}`)
	}
	return value
}

func validateBinding(value sharedartifact.Binding) error {
	if _, err := normalizeWorkspace(value.WorkspaceID); err != nil {
		return err
	}
	if value.ID == "" || value.ArtifactID == "" || value.Owner == "" || !sharedartifact.BindingKindRegistered(value.Kind) || value.ResourceType == "" || value.ResourceID == "" || value.CreatedAt.IsZero() || !json.Valid(value.Metadata) {
		return fmt.Errorf("artifact binding is invalid")
	}
	return nil
}

func sameArtifact(left, right sharedartifact.Artifact) bool {
	return left.Owner == right.Owner && left.Kind == right.Kind && left.IdempotencyKey == right.IdempotencyKey && left.CreatedBy == right.CreatedBy && left.OwnerOrgID == right.OwnerOrgID && left.Filename == right.Filename && left.MediaType == right.MediaType && left.ContentSHA256 == right.ContentSHA256 && left.SizeBytes == right.SizeBytes && left.StorageReference == right.StorageReference && left.DownloadTokenSHA256 == right.DownloadTokenSHA256 && left.AuthorizationScopeSHA256 == right.AuthorizationScopeSHA256 && left.ExpiresAt.Equal(right.ExpiresAt) && bytes.Equal(left.Metadata, right.Metadata)
}

func normalizeWorkspace(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 255 {
		return "", fmt.Errorf("artifact workspace is invalid")
	}
	return value, nil
}

func validStatus(value sharedartifact.Status) bool {
	switch value {
	case sharedartifact.StatusPending, sharedartifact.StatusAvailable, sharedartifact.StatusRejected, sharedartifact.StatusExpired, sharedartifact.StatusDeleted:
		return true
	}
	return false
}

func validScan(value sharedartifact.ScanStatus) bool {
	switch value {
	case sharedartifact.ScanNotRequired, sharedartifact.ScanPending, sharedartifact.ScanClean, sharedartifact.ScanRejected, sharedartifact.ScanFailed:
		return true
	}
	return false
}

func optionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func SchemaMigrations(renderer modulehost.Dialect) ([]modulehost.SchemaMigration, error) {
	migration := modulehost.SchemaMigration{Version: 1, Name: "shared_artifacts"}
	tables := []*ormschema.TableBuilder{
		ormschema.NewTable(renderer, artifactTable).IfNotExists().Columns(
			required("workspace_id", ormschema.TextKey(191)), required("id", ormschema.TextKey(255)), required("owner", ormschema.TextKey(191)), required("kind", ormschema.TextKey(191)), required("idempotency_key", ormschema.TextKey(191)), required("created_by", ormschema.TextKey(255)), ormschema.Column("owner_org_id", ormschema.TextKey(255)).NotNull().DefaultValue(""), required("filename", ormschema.Text()), required("media_type", ormschema.TextKey(255)), required("content_sha256", ormschema.TextKey(255)), required("size_bytes", ormschema.BigInt()), required("storage_reference", ormschema.Text()), required("status", ormschema.TextKey(64)), ormschema.Column("expires_at", ormschema.TextKey(255)).NotNull().DefaultValue(""), required("scan_status", ormschema.TextKey(64)), ormschema.Column("download_token_sha256", ormschema.TextKey(255)).NotNull().DefaultValue(""), ormschema.Column("authorization_scope_sha256", ormschema.TextKey(255)).NotNull().DefaultValue(""), required("metadata_json", ormschema.LongText()), required("created_at", ormschema.TextKey(255)), required("updated_at", ormschema.TextKey(255)),
		).PrimaryKey("id").Unique("workspace_id", "owner", "kind", "idempotency_key").Unique("workspace_id", "owner", "kind", "storage_reference"),
		ormschema.NewTable(renderer, bindingTable).IfNotExists().Columns(
			required("workspace_id", ormschema.TextKey(191)), required("id", ormschema.TextKey(255)), required("artifact_id", ormschema.TextKey(255)), required("owner", ormschema.TextKey(191)), required("kind", ormschema.TextKey(191)), required("resource_type", ormschema.TextKey(191)), required("resource_id", ormschema.TextKey(255)), ormschema.Column("field_key", ormschema.TextKey(255)).NotNull().DefaultValue(""), required("metadata_json", ormschema.LongText()), required("created_at", ormschema.TextKey(255)),
		).PrimaryKey("id").Unique("workspace_id", "artifact_id", "owner", "kind", "resource_type", "resource_id", "field_key"),
	}
	for _, table := range tables {
		statement, _, err := table.Build()
		if err != nil {
			return nil, err
		}
		migration.Statements = append(migration.Statements, statement)
	}
	indexes := []struct {
		name, table string
		columns     []string
	}{
		{"idx_artifact_download_token", artifactTable, []string{"workspace_id", "download_token_sha256"}},
		{"idx_artifact_expiry", artifactTable, []string{"workspace_id", "status", "expires_at"}},
		{"idx_artifact_storage_reference", artifactTable, []string{"workspace_id", "storage_reference"}},
		{"idx_artifact_owner_cursor", artifactTable, []string{"workspace_id", "owner", "kind", "status", "created_at", "id"}},
		{"idx_artifact_creator_cursor", artifactTable, []string{"workspace_id", "owner", "kind", "created_by", "created_at", "id"}},
		{"idx_artifact_owner_org_cursor", artifactTable, []string{"workspace_id", "owner", "kind", "owner_org_id", "created_at", "id"}},
		{"idx_artifact_scan_queue", artifactTable, []string{"owner", "kind", "scan_status", "created_at", "id"}},
		{"idx_artifact_binding_artifact", bindingTable, []string{"workspace_id", "artifact_id"}},
		{"idx_artifact_binding_resource", bindingTable, []string{"workspace_id", "owner", "kind", "resource_type", "resource_id", "artifact_id"}},
	}
	for _, index := range indexes {
		statement, _, err := ormschema.NewIndex(renderer, index.name, index.table).Columns(index.columns...).Build()
		if err != nil {
			return nil, err
		}
		migration.Statements = append(migration.Statements, statement)
	}
	return []modulehost.SchemaMigration{migration}, nil
}

func required(name string, dataType ormschema.ColumnType) ormschema.ColumnDefinition {
	return ormschema.Column(name, dataType).NotNull()
}

var _ sharedartifact.Store = Store{}
