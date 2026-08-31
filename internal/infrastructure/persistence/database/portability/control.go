package portabilitystore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

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
	queryValue, args, err := query.NewWorkspaceSelectBuilder(s.Renderer, "_notification_migration_controls", workspaceID).Columns("migration_id", "role", "state", "bundle_fingerprint", "frozen_at", "activated_at", "updated_at").Build()
	if err != nil {
		return MigrationControl{}, err
	}
	err = s.Database.QueryRowContext(s.workspaceScope.Context(ctx, notification.WorkspaceID(workspaceID)), queryValue, args...).Scan(&control.MigrationID, &control.Role, &control.State, &control.BundleFingerprint, &control.FrozenAt, &control.ActivatedAt, &control.UpdatedAt)
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
		_, err = s.WorkspaceInsert(s.workspaceScope.Context(ctx, notification.WorkspaceID(workspaceID)), s.Database, workspaceID, "_notification_migration_controls", []string{"workspace_id", "migration_id", "role", "state", "bundle_fingerprint", "frozen_at", "activated_at", "updated_at"}, workspaceID, migrationID, MigrationRoleSource, MigrationStateFrozen, "", atText, "", atText)
	} else {
		queryValue, args, buildErr := query.NewWorkspaceUpdateBuilder(s.Renderer, "_notification_migration_controls", workspaceID).Set("migration_id", migrationID).Set("role", MigrationRoleSource).Set("state", MigrationStateFrozen).Set("bundle_fingerprint", "").Set("frozen_at", atText).Set("activated_at", "").Set("updated_at", atText).Where(query.Equal("state", MigrationStateActive)).Build()
		if buildErr != nil {
			return MigrationControl{}, buildErr
		}
		_, err = s.Database.ExecContext(s.workspaceScope.Context(ctx, notification.WorkspaceID(workspaceID)), queryValue, args...)
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
	_, err = s.WorkspaceInsert(s.workspaceScope.Context(ctx, notification.WorkspaceID(workspaceID)), s.Database, workspaceID, "_notification_migration_controls", []string{"workspace_id", "migration_id", "role", "state", "bundle_fingerprint", "frozen_at", "activated_at", "updated_at"}, workspaceID, migrationID, MigrationRoleTarget, MigrationStateImported, fingerprint, atText, "", atText)
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
	predicate := query.And(query.Equal("migration_id", migrationID), query.Equal("role", fromRole), query.Equal("state", fromState), query.Or(query.Equal("bundle_fingerprint", ""), query.Equal("bundle_fingerprint", fingerprint)))
	queryValue, args, err := query.NewWorkspaceUpdateBuilder(s.Renderer, "_notification_migration_controls", workspaceID).Set("role", toRole).Set("state", toState).Set("bundle_fingerprint", fingerprint).Set("activated_at", activatedAt).Set("updated_at", atText).Where(predicate).Build()
	if err != nil {
		return MigrationControl{}, err
	}
	result, err := s.Database.ExecContext(s.workspaceScope.Context(ctx, notification.WorkspaceID(workspaceID)), queryValue, args...)
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
	for _, table := range []string{"_notification_events", "_notification_channel_plans"} {
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
	queryValue, args, err := query.NewSelectBuilder(s.Renderer, "_notification_template_publication_requests").Projections(query.Project(query.CountAll())).Where(query.NotEqual("lease_owner", "")).Build()
	if err != nil {
		return 0, err
	}
	var count int
	if err := s.Database.QueryRowContext(ctx, queryValue, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count notification publication migration leases: %w", err)
	}
	return total + count, nil
}
