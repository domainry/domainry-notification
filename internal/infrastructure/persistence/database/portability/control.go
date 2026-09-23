package portabilitystore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/domainry/domainry-notification-sdk/modulehost"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-orm/query"
)

const (
	MigrationStateActive   = "active"
	MigrationStateFrozen   = "frozen"
	MigrationStateImported = "imported"
	MigrationStateCutover  = "cutover"
	MigrationRoleSource    = "source"
	MigrationRoleTarget    = "target"

	migrationControlPurpose = "notification_migration"
	migrationControlKind    = "workspace_cutover"
	migrationControlActor   = "notification"
	migrationControlReason  = "notification workspace migration"
)

type MigrationControl struct {
	WorkspaceID       string `json:"workspace_id"`
	MigrationID       string `json:"migration_id"`
	Role              string `json:"role"`
	State             string `json:"state"`
	BundleFingerprint string `json:"bundle_fingerprint"`
	FrozenAt          string `json:"frozen_at,omitempty"`
	ActivatedAt       string `json:"activated_at,omitempty"`
	UpdatedAt         string `json:"updated_at,omitempty"`
	ActiveLeases      int    `json:"active_leases"`
	Revision          int64  `json:"-"`
}

type migrationControlReference struct {
	MigrationID       string `json:"migration_id"`
	Role              string `json:"role"`
	BundleFingerprint string `json:"bundle_fingerprint,omitempty"`
	FrozenAt          string `json:"frozen_at,omitempty"`
	ActivatedAt       string `json:"activated_at,omitempty"`
}

func (s *Store) MigrationStatus(ctx context.Context, workspaceID string) (MigrationControl, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if s == nil || s.controls == nil || workspaceID == "" {
		return MigrationControl{}, fmt.Errorf("notification migration control store and workspace are required")
	}
	result := MigrationControl{WorkspaceID: workspaceID, State: MigrationStateActive}
	control, found, err := s.controls.GetOperationControl(ctx, migrationControlPurpose, migrationControlKind, workspaceID)
	if err != nil {
		return MigrationControl{}, fmt.Errorf("read notification migration control: %w", err)
	}
	if found {
		var reference migrationControlReference
		if err := json.Unmarshal([]byte(control.Reference), &reference); err != nil {
			return MigrationControl{}, fmt.Errorf("decode notification migration control reference: %w", err)
		}
		result.MigrationID = strings.TrimSpace(reference.MigrationID)
		result.Role = strings.TrimSpace(reference.Role)
		result.State = strings.TrimSpace(control.State)
		result.BundleFingerprint = strings.TrimSpace(reference.BundleFingerprint)
		result.FrozenAt = strings.TrimSpace(reference.FrozenAt)
		result.ActivatedAt = strings.TrimSpace(reference.ActivatedAt)
		result.UpdatedAt = control.UpdatedAt.UTC().Format(time.RFC3339Nano)
		result.Revision = control.Revision
	}
	result.ActiveLeases, err = s.activeMigrationLeases(ctx, workspaceID)
	return result, err
}

func (s *Store) FreezeMigration(ctx context.Context, workspaceID, migrationID string, at time.Time) (MigrationControl, error) {
	workspaceID, migrationID = strings.TrimSpace(workspaceID), strings.TrimSpace(migrationID)
	if s == nil || workspaceID == "" || migrationID == "" || at.IsZero() {
		return MigrationControl{}, fmt.Errorf("notification migration freeze command is invalid")
	}
	current, err := s.MigrationStatus(ctx, workspaceID)
	if err != nil {
		return MigrationControl{}, err
	}
	if current.State == MigrationStateFrozen && current.Role == MigrationRoleSource && current.MigrationID == migrationID {
		return current, nil
	}
	if current.State != MigrationStateActive {
		return MigrationControl{}, fmt.Errorf("notification migration cannot freeze from state %q", current.State)
	}
	atText := at.UTC().Format(time.RFC3339Nano)
	return s.putMigrationControl(ctx, current, MigrationControl{
		WorkspaceID: workspaceID, MigrationID: migrationID, Role: MigrationRoleSource, State: MigrationStateFrozen,
		FrozenAt: atText, UpdatedAt: atText,
	}, at)
}

func (s *Store) RecordMigrationFingerprint(ctx context.Context, workspaceID, migrationID, fingerprint string, at time.Time) (MigrationControl, error) {
	return s.transitionMigration(ctx, workspaceID, migrationID, MigrationRoleSource, MigrationStateFrozen, MigrationRoleSource, MigrationStateFrozen, fingerprint, at)
}

func (s *Store) RecordImportedMigration(ctx context.Context, workspaceID, migrationID, fingerprint string, at time.Time) (MigrationControl, error) {
	workspaceID, migrationID, fingerprint = strings.TrimSpace(workspaceID), strings.TrimSpace(migrationID), strings.TrimSpace(fingerprint)
	if workspaceID == "" || migrationID == "" || fingerprint == "" || at.IsZero() {
		return MigrationControl{}, fmt.Errorf("notification imported migration command is invalid")
	}
	current, err := s.MigrationStatus(ctx, workspaceID)
	if err != nil {
		return MigrationControl{}, err
	}
	if current.State != MigrationStateActive || current.Revision != 0 {
		return MigrationControl{}, fmt.Errorf("notification migration target is not active-empty")
	}
	atText := at.UTC().Format(time.RFC3339Nano)
	return s.putMigrationControl(ctx, current, MigrationControl{
		WorkspaceID: workspaceID, MigrationID: migrationID, Role: MigrationRoleTarget, State: MigrationStateImported,
		BundleFingerprint: fingerprint, FrozenAt: atText, UpdatedAt: atText,
	}, at)
}

func (s *Store) ActivateMigration(ctx context.Context, workspaceID, migrationID, fingerprint string, at time.Time) (MigrationControl, error) {
	return s.transitionMigration(ctx, workspaceID, migrationID, MigrationRoleTarget, MigrationStateImported, MigrationRoleTarget, MigrationStateCutover, fingerprint, at)
}

func (s *Store) RollbackMigration(ctx context.Context, workspaceID, migrationID, fingerprint string, at time.Time) (MigrationControl, error) {
	return s.transitionMigration(ctx, workspaceID, migrationID, MigrationRoleSource, MigrationStateFrozen, MigrationRoleSource, MigrationStateActive, fingerprint, at)
}

func (s *Store) transitionMigration(ctx context.Context, workspaceID, migrationID, fromRole, fromState, toRole, toState, fingerprint string, at time.Time) (MigrationControl, error) {
	workspaceID, migrationID, fingerprint = strings.TrimSpace(workspaceID), strings.TrimSpace(migrationID), strings.TrimSpace(fingerprint)
	if s == nil || workspaceID == "" || migrationID == "" || fingerprint == "" || at.IsZero() {
		return MigrationControl{}, fmt.Errorf("notification migration transition is invalid")
	}
	current, err := s.MigrationStatus(ctx, workspaceID)
	if err != nil {
		return MigrationControl{}, err
	}
	if current.MigrationID != migrationID || current.Role != fromRole || current.State != fromState ||
		(current.BundleFingerprint != "" && current.BundleFingerprint != fingerprint) {
		return MigrationControl{}, fmt.Errorf("notification migration transition conflict")
	}
	next := current
	next.Role, next.State, next.BundleFingerprint = toRole, toState, fingerprint
	next.UpdatedAt = at.UTC().Format(time.RFC3339Nano)
	if toState == MigrationStateCutover {
		next.ActivatedAt = next.UpdatedAt
	} else {
		next.ActivatedAt = ""
	}
	return s.putMigrationControl(ctx, current, next, at)
}

func (s *Store) putMigrationControl(ctx context.Context, current, next MigrationControl, at time.Time) (MigrationControl, error) {
	reference, err := json.Marshal(migrationControlReference{
		MigrationID: next.MigrationID, Role: next.Role, BundleFingerprint: next.BundleFingerprint, FrozenAt: next.FrozenAt, ActivatedAt: next.ActivatedAt,
	})
	if err != nil {
		return MigrationControl{}, err
	}
	changed, err := s.controls.PutOperationControl(ctx, modulehost.OperationControl{
		SystemPurpose: migrationControlPurpose,
		Kind:          migrationControlKind,
		Owner:         next.WorkspaceID,
		State:         next.State,
		Reason:        migrationControlReason,
		Reference:     string(reference),
		UpdatedBy:     migrationControlActor,
		Revision:      current.Revision + 1,
		UpdatedAt:     at.UTC(),
	}, current.Revision)
	if err != nil {
		return MigrationControl{}, fmt.Errorf("write notification migration control: %w", err)
	}
	if !changed {
		return MigrationControl{}, fmt.Errorf("notification migration transition conflict")
	}
	return s.MigrationStatus(ctx, next.WorkspaceID)
}

func (s *Store) activeMigrationLeases(ctx context.Context, workspaceID string) (int, error) {
	total := 0
	for _, table := range []string{"_notification_events", "_notification_deliveries"} {
		queryValue, args, err := query.NewWorkspaceSelectBuilder(s.Renderer, table, workspaceID).Projections(query.Project(query.CountAll())).Where(query.NotEqual("lease_owner", "")).Build()
		if err != nil {
			return 0, err
		}
		var count int
		if err := s.Database.QueryRowContext(s.workspaceScope.Context(ctx, notification.WorkspaceID(workspaceID)), queryValue, args...).Scan(&count); err != nil {
			return 0, fmt.Errorf("count notification migration leases in %s: %w", table, err)
		}
		total += count
	}
	return total, nil
}
