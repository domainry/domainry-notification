package retentionarchivestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sharedartifact "github.com/domainry/domainry-foundation/artifact"
	"github.com/domainry/domainry-notification-sdk/modulehost"
)

type Content interface {
	sharedartifact.ContentStore
	sharedartifact.ContentWriter
}

type Store struct {
	artifacts sharedartifact.Store
	content   Content
}

type archiveArtifactMetadata struct {
	Owner         string `json:"owner"`
	SourceTable   string `json:"source_table"`
	ResourceID    string `json:"resource_id"`
	PolicyKey     string `json:"policy_key"`
	PolicyVersion string `json:"policy_version"`
	JobID         string `json:"job_id"`
}

func New(artifacts sharedartifact.Store, content Content) (*Store, error) {
	if artifacts == nil || content == nil {
		return nil, fmt.Errorf("shared Lifecycle archive Artifact store is incomplete")
	}
	return &Store{artifacts: artifacts, content: content}, nil
}

func archiveArtifactID(workspaceID, sourceTable, resourceID, policyKey string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(workspaceID) + "\x00" + strings.TrimSpace(policyKey) + "\x00" + strings.TrimSpace(sourceTable) + "\x00" + strings.TrimSpace(resourceID)))
	return "lifecycle_archive_" + hex.EncodeToString(digest[:])
}

func (s *Store) Archived(ctx context.Context, workspaceID, sourceTable, resourceID, policyKey string) (bool, error) {
	workspaceID, sourceTable = strings.TrimSpace(workspaceID), strings.TrimSpace(sourceTable)
	resourceID, policyKey = strings.TrimSpace(resourceID), strings.TrimSpace(policyKey)
	if s == nil || s.artifacts == nil || workspaceID == "" || sourceTable == "" || resourceID == "" || policyKey == "" {
		return false, fmt.Errorf("shared Lifecycle archive identity is required")
	}
	value, found, err := s.artifacts.ByID(ctx, workspaceID, archiveArtifactID(workspaceID, sourceTable, resourceID, policyKey))
	if err != nil || !found {
		return false, err
	}
	if value.Owner != sharedartifact.OwnerLifecycle || value.Kind != "archive" || value.Status != sharedartifact.StatusAvailable {
		return false, nil
	}
	bindings, err := s.artifacts.Bindings(ctx, workspaceID, value.ID)
	if err != nil {
		return false, err
	}
	for _, binding := range bindings {
		if binding.Owner == sharedartifact.OwnerLifecycle &&
			binding.Kind == sharedartifact.BindingObjectField &&
			binding.ResourceType == sourceTable &&
			binding.ResourceID == resourceID &&
			binding.FieldKey == policyKey {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) ArchivePayload(ctx context.Context, owner string, job modulehost.RetentionArchiveJob, policy modulehost.RetentionArchivePolicy, sourceTable, resourceID string, payload []byte) (created bool, err error) {
	owner, job.ID, job.WorkspaceID = strings.TrimSpace(owner), strings.TrimSpace(job.ID), strings.TrimSpace(job.WorkspaceID)
	policy.Key, policy.Version = strings.TrimSpace(policy.Key), strings.TrimSpace(policy.Version)
	sourceTable, resourceID = strings.TrimSpace(sourceTable), strings.TrimSpace(resourceID)
	if s == nil || s.artifacts == nil || s.content == nil || owner == "" || job.ID == "" || job.WorkspaceID == "" || policy.Key == "" || policy.Version == "" || sourceTable == "" || resourceID == "" || len(payload) == 0 {
		return false, fmt.Errorf("shared Lifecycle archive payload command is invalid")
	}
	if exists, readErr := s.Archived(ctx, job.WorkspaceID, sourceTable, resourceID, policy.Key); readErr != nil || exists {
		return false, readErr
	}

	artifactID := archiveArtifactID(job.WorkspaceID, sourceTable, resourceID, policy.Key)
	content, err := s.content.PutImmutable(ctx, job.WorkspaceID, artifactID, payload)
	if err != nil {
		return false, err
	}
	// The blob write precedes SQL registration. On failure, remove only an
	// unreferenced blob; a concurrent or partially completed retry may already
	// have committed the same immutable reference.
	defer func() {
		if err == nil {
			return
		}
		value, found, readErr := s.artifacts.ByID(context.WithoutCancel(ctx), job.WorkspaceID, artifactID)
		if readErr == nil && found && value.StorageReference == content.Reference {
			return
		}
		_ = s.content.Delete(context.WithoutCancel(ctx), job.WorkspaceID, content.Reference)
	}()

	digest := sha256.Sum256(payload)
	if content.Reference == "" || !strings.EqualFold(content.SHA256, hex.EncodeToString(digest[:])) || content.Size != int64(len(payload)) {
		return false, fmt.Errorf("shared Lifecycle archive content evidence mismatch")
	}
	metadata, err := json.Marshal(archiveArtifactMetadata{
		Owner: owner, SourceTable: sourceTable, ResourceID: resourceID,
		PolicyKey: policy.Key, PolicyVersion: policy.Version, JobID: job.ID,
	})
	if err != nil {
		return false, err
	}
	archivedAt := job.ArchivedAt.UTC()
	if archivedAt.IsZero() {
		archivedAt = time.Now().UTC()
	}
	value := sharedartifact.Artifact{
		ID: artifactID, WorkspaceID: job.WorkspaceID, Owner: sharedartifact.OwnerLifecycle, Kind: "archive", IdempotencyKey: artifactID,
		CreatedBy: "notification_retention", Filename: artifactID + ".json", MediaType: "application/json",
		ContentSHA256: content.SHA256, SizeBytes: content.Size, StorageReference: content.Reference,
		Status: sharedartifact.StatusAvailable, ScanStatus: sharedartifact.ScanNotRequired, Metadata: metadata,
		CreatedAt: archivedAt, UpdatedAt: archivedAt,
	}
	_, artifactCreated, err := s.artifacts.Register(ctx, value)
	if err != nil {
		return false, fmt.Errorf("register shared Lifecycle archive Artifact: %w", err)
	}
	jobBinding := sharedartifact.Binding{
		ID: artifactID + ":job", WorkspaceID: job.WorkspaceID, ArtifactID: artifactID, Owner: sharedartifact.OwnerLifecycle,
		Kind: sharedartifact.BindingJob, ResourceType: "lifecycle.cleanup_job", ResourceID: job.ID,
		Metadata: json.RawMessage(`{}`), CreatedAt: archivedAt,
	}
	_, jobBindingCreated, err := s.artifacts.Bind(ctx, jobBinding)
	if err != nil {
		return false, fmt.Errorf("bind shared Lifecycle archive job Artifact: %w", err)
	}
	sourceBinding := sharedartifact.Binding{
		ID: artifactID + ":source", WorkspaceID: job.WorkspaceID, ArtifactID: artifactID, Owner: sharedartifact.OwnerLifecycle,
		Kind: sharedartifact.BindingObjectField, ResourceType: sourceTable, ResourceID: resourceID, FieldKey: policy.Key,
		Metadata: json.RawMessage(`{}`), CreatedAt: archivedAt,
	}
	_, sourceBindingCreated, err := s.artifacts.Bind(ctx, sourceBinding)
	if err != nil {
		return false, fmt.Errorf("bind shared Lifecycle archive source Artifact: %w", err)
	}
	return artifactCreated || jobBindingCreated || sourceBindingCreated, nil
}

var _ modulehost.RetentionArchiveStore = (*Store)(nil)
