package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

const (
	MigrationStateActive   = "active"
	MigrationStateFrozen   = "frozen"
	MigrationStateImported = "imported"
	MigrationStateCutover  = "cutover"
	MigrationRoleSource    = "source"
	MigrationRoleTarget    = "target"
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
}

func (s *Store) MigrationStatus(ctx context.Context, workspaceID string) (MigrationControl, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if s == nil || workspaceID == "" {
		return MigrationControl{}, fmt.Errorf("notification migration workspace is required")
	}
	control := MigrationControl{WorkspaceID: workspaceID, State: MigrationStateActive}
	query := "SELECT " + s.columns([]string{"migration_id", "role", "state", "bundle_fingerprint", "frozen_at", "activated_at", "updated_at"}) + " FROM " + s.dialect.Table("notification_migration_controls") + " WHERE " + s.dialect.Identifier("workspace_id") + " = " + s.dialect.Placeholder(1)
	err := s.database.QueryRowContext(s.workspaceScope.Context(ctx, notification.WorkspaceID(workspaceID)), query, workspaceID).Scan(&control.MigrationID, &control.Role, &control.State, &control.BundleFingerprint, &control.FrozenAt, &control.ActivatedAt, &control.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return control, nil
	}
	if err != nil {
		return MigrationControl{}, fmt.Errorf("read notification migration control: %w", err)
	}
	control.ActiveLeases, err = s.activeMigrationLeases(ctx, workspaceID)
	return control, err
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
	if current.MigrationID == "" {
		_, err = s.database.ExecContext(s.workspaceScope.Context(ctx, notification.WorkspaceID(workspaceID)), s.dialect.Insert("notification_migration_controls", []string{"workspace_id", "migration_id", "role", "state", "bundle_fingerprint", "frozen_at", "activated_at", "updated_at"}), workspaceID, migrationID, MigrationRoleSource, MigrationStateFrozen, "", atText, "", atText)
	} else {
		query := "UPDATE " + s.dialect.Table("notification_migration_controls") + " SET " + s.dialect.Identifier("migration_id") + "=" + s.dialect.Placeholder(1) + "," + s.dialect.Identifier("role") + "=" + s.dialect.Placeholder(2) + "," + s.dialect.Identifier("state") + "=" + s.dialect.Placeholder(3) + "," + s.dialect.Identifier("bundle_fingerprint") + "=''," + s.dialect.Identifier("frozen_at") + "=" + s.dialect.Placeholder(4) + "," + s.dialect.Identifier("activated_at") + "=''," + s.dialect.Identifier("updated_at") + "=" + s.dialect.Placeholder(5) + " WHERE " + s.dialect.Identifier("workspace_id") + "=" + s.dialect.Placeholder(6) + " AND " + s.dialect.Identifier("state") + "=" + s.dialect.Placeholder(7)
		_, err = s.database.ExecContext(s.workspaceScope.Context(ctx, notification.WorkspaceID(workspaceID)), query, migrationID, MigrationRoleSource, MigrationStateFrozen, atText, atText, workspaceID, MigrationStateActive)
	}
	if err != nil {
		return MigrationControl{}, fmt.Errorf("freeze notification migration: %w", err)
	}
	return s.MigrationStatus(ctx, workspaceID)
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
	if current.State != MigrationStateActive {
		return MigrationControl{}, fmt.Errorf("notification migration target is not active-empty")
	}
	atText := at.UTC().Format(time.RFC3339Nano)
	_, err = s.database.ExecContext(s.workspaceScope.Context(ctx, notification.WorkspaceID(workspaceID)), s.dialect.Insert("notification_migration_controls", []string{"workspace_id", "migration_id", "role", "state", "bundle_fingerprint", "frozen_at", "activated_at", "updated_at"}), workspaceID, migrationID, MigrationRoleTarget, MigrationStateImported, fingerprint, atText, "", atText)
	if err != nil {
		return MigrationControl{}, fmt.Errorf("record imported notification migration: %w", err)
	}
	return s.MigrationStatus(ctx, workspaceID)
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
	atText := at.UTC().Format(time.RFC3339Nano)
	activatedAt := ""
	if toState == MigrationStateCutover {
		activatedAt = atText
	}
	query := "UPDATE " + s.dialect.Table("notification_migration_controls") + " SET " + s.dialect.Identifier("role") + "=" + s.dialect.Placeholder(1) + "," + s.dialect.Identifier("state") + "=" + s.dialect.Placeholder(2) + "," + s.dialect.Identifier("bundle_fingerprint") + "=" + s.dialect.Placeholder(3) + "," + s.dialect.Identifier("activated_at") + "=" + s.dialect.Placeholder(4) + "," + s.dialect.Identifier("updated_at") + "=" + s.dialect.Placeholder(5) + " WHERE " + s.dialect.Identifier("workspace_id") + "=" + s.dialect.Placeholder(6) + " AND " + s.dialect.Identifier("migration_id") + "=" + s.dialect.Placeholder(7) + " AND " + s.dialect.Identifier("role") + "=" + s.dialect.Placeholder(8) + " AND " + s.dialect.Identifier("state") + "=" + s.dialect.Placeholder(9) + " AND (" + s.dialect.Identifier("bundle_fingerprint") + "='' OR " + s.dialect.Identifier("bundle_fingerprint") + "=" + s.dialect.Placeholder(10) + ")"
	result, err := s.database.ExecContext(s.workspaceScope.Context(ctx, notification.WorkspaceID(workspaceID)), query, toRole, toState, fingerprint, activatedAt, atText, workspaceID, migrationID, fromRole, fromState, fingerprint)
	if err != nil {
		return MigrationControl{}, fmt.Errorf("transition notification migration: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return MigrationControl{}, fmt.Errorf("notification migration transition conflict")
	}
	return s.MigrationStatus(ctx, workspaceID)
}

func (s *Store) activeMigrationLeases(ctx context.Context, workspaceID string) (int, error) {
	total := 0
	for _, table := range []string{"notification_events", "notification_channel_plans"} {
		query := "SELECT COUNT(*) FROM " + s.dialect.Table(table) + " WHERE " + s.dialect.Identifier("workspace_id") + "=" + s.dialect.Placeholder(1) + " AND " + s.dialect.Identifier("lease_owner") + "<>''"
		var count int
		if err := s.database.QueryRowContext(s.workspaceScope.Context(ctx, notification.WorkspaceID(workspaceID)), query, workspaceID).Scan(&count); err != nil {
			return 0, fmt.Errorf("count notification migration leases in %s: %w", table, err)
		}
		total += count
	}
	query := "SELECT COUNT(*) FROM " + s.dialect.Table("notification_template_publication_requests") + " WHERE " + s.dialect.Identifier("lease_owner") + "<>''"
	var count int
	if err := s.database.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("count notification publication migration leases: %w", err)
	}
	return total + count, nil
}
