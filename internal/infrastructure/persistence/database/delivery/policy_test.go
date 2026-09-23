package deliverystore_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	metadatasdk "github.com/domainry/domainry-metadata-sdk"
	"github.com/domainry/domainry-notification/internal/domain/delivery/service"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	sqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence"
	ormdialect "github.com/domainry/domainry-orm/dialect"
	_ "modernc.org/sqlite"
)

func TestDeliveryPolicyAndRecipientPreferencePersistence(t *testing.T) {
	_, store := migratedStore(t)
	policy, err := store.GetPolicy(t.Context())
	if err != nil || !policy.Enabled || policy.MaxPerRecipientPerHour != 20 || policy.Revision != metadatasdk.DefinitionNoCurrentVersion {
		t.Fatalf("default policy=%+v err=%v", policy, err)
	}
	initialRevision := policy.Revision
	policy.MaxPerRecipientPerHour, policy.UpdatedBy, policy.UpdatedAt = 40, "admin-1", "2026-08-24T01:00:00.000000000Z"
	saved, err := store.SavePolicy(t.Context(), policy)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.GetPolicy(t.Context())
	if err != nil || stored.MaxPerRecipientPerHour != 40 || stored.UpdatedBy != "admin-1" || stored.Revision == "" || stored.Revision != saved.Revision {
		t.Fatalf("stored policy=%+v err=%v", stored, err)
	}
	stale := stored
	stale.Revision = initialRevision
	if _, err := store.SavePolicy(t.Context(), stale); notification.ErrorCode(err) != "backend.notification.policy_revision_conflict" {
		t.Fatalf("stale policy error=%v", err)
	}
	preference := delivery.RecipientPreference{RecipientKey: "user-1", EnabledChannels: map[string]bool{"email": true}, UpdatedBy: "user-1", UpdatedAt: policy.UpdatedAt}
	if _, err := store.SaveRecipientPreference(t.Context(), "workspace-1", preference); err != nil {
		t.Fatal(err)
	}
	got, found, err := store.GetRecipientPreference(t.Context(), "workspace-1", "user-1")
	if err != nil || !found || !got.EnabledChannels["email"] {
		t.Fatalf("preference=%+v found=%v err=%v", got, found, err)
	}
	listed, err := store.ListRecipientPreferences(t.Context(), "workspace-1")
	if err != nil || len(listed) != 1 || listed[0].RecipientKey != "user-1" {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
}

func TestDeliveryReservationEnforcesFrequencyAndDedupe(t *testing.T) {
	_, store := migratedStore(t)
	reservation := delivery.Reservation{ID: "reservation-1", RecipientKey: "user-1", TemplateKey: "workflow.failed", Channel: "email", DedupeKey: "run-1", CreatedAt: "2026-08-24T01:00:00.000000000Z"}
	if err := store.ReserveBatch(t.Context(), "workspace-1", []delivery.Reservation{reservation}, 2, 300); err != nil {
		t.Fatal(err)
	}
	duplicate := reservation
	duplicate.ID, duplicate.CreatedAt = "reservation-2", "2026-08-24T01:01:00.000000000Z"
	if err := store.ReserveBatch(t.Context(), "workspace-1", []delivery.Reservation{duplicate}, 2, 300); !errors.Is(err, delivery.ErrDuplicate) {
		t.Fatalf("duplicate err=%v", err)
	}
	second := reservation
	second.ID, second.DedupeKey, second.CreatedAt = "reservation-3", "run-2", "2026-08-24T01:02:00.000000000Z"
	if err := store.ReserveBatch(t.Context(), "workspace-1", []delivery.Reservation{second}, 2, 300); err != nil {
		t.Fatal(err)
	}
	third := reservation
	third.ID, third.DedupeKey, third.CreatedAt = "reservation-4", "run-3", "2026-08-24T01:03:00.000000000Z"
	if err := store.ReserveBatch(t.Context(), "workspace-1", []delivery.Reservation{third}, 2, 300); !errors.Is(err, delivery.ErrFrequencyExceeded) {
		t.Fatalf("frequency err=%v", err)
	}
}

func TestDeliveryReservationBatchRollsBackPartialRecipients(t *testing.T) {
	db, store := migratedStore(t)
	first := delivery.Reservation{ID: "reservation-1", RecipientKey: "user-1", TemplateKey: "workflow.failed", Channel: "email", DedupeKey: "run-1", CreatedAt: "2026-08-24T01:00:00.000000000Z"}
	second := first
	second.ID, second.CreatedAt = "reservation-2", "2026-08-24T01:01:00.000000000Z"
	if err := store.ReserveBatch(t.Context(), "workspace-1", []delivery.Reservation{first, second}, 20, 300); !errors.Is(err, delivery.ErrDuplicate) {
		t.Fatalf("err=%v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM _notification_deliveries WHERE row_kind = 'reservation'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}

func migratedStore(t *testing.T) (*sql.DB, *sqlstore.Store) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	migrations, err := sqlstore.SchemaMigrations(sqlstore.SQLite, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		for _, statement := range migration.Statements {
			if _, err := db.Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	dialect, _ := ormdialect.ParseRenderer("sqlite", "", "")
	store, err := sqlstore.New(sqlstore.Config{Database: db, Dialect: dialect, WorkspaceScope: passthroughScope{}, QueueScopes: &queueScopes{}, Clock: storeClock{value: time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC)}, WorkspaceID: "workspace-1", DefinitionStore: &policyDefinitionStore{}})
	if err != nil {
		t.Fatal(err)
	}
	return db, store
}

type policyDefinitionStore struct {
	current metadatasdk.Definition
	version int
}

func (s *policyDefinitionStore) List(context.Context, metadatasdk.DefinitionQuery) ([]metadatasdk.Definition, error) {
	if s.current.CurrentVersionID == "" {
		return nil, nil
	}
	return []metadatasdk.Definition{s.current}, nil
}
func (s *policyDefinitionStore) Get(_ context.Context, owner, kind, key string) (metadatasdk.Definition, bool, error) {
	if s.current.CurrentVersionID == "" || s.current.Owner != owner || s.current.ResourceType != kind || s.current.ResourceKey != key {
		return metadatasdk.Definition{}, false, nil
	}
	return s.current, true, nil
}
func (s *policyDefinitionStore) Snapshot(ctx context.Context, query metadatasdk.DefinitionQuery) (metadatasdk.DefinitionSnapshot, error) {
	values, err := s.List(ctx, query)
	return metadatasdk.DefinitionSnapshot{Definitions: values}, err
}
func (s *policyDefinitionStore) ReplaceSourceSnapshot(context.Context, metadatasdk.ProjectionSnapshot) error {
	return nil
}
func (s *policyDefinitionStore) Publish(_ context.Context, command metadatasdk.DefinitionPublishCommand) (metadatasdk.DefinitionPublishResult, error) {
	expected := metadatasdk.DefinitionNoCurrentVersion
	if s.current.CurrentVersionID != "" {
		expected = s.current.CurrentVersionID
	}
	if command.ExpectedCurrentVersionID != expected {
		return metadatasdk.DefinitionPublishResult{}, &metadatasdk.Error{StatusCode: 409, Code: "metadata.definition_revision_conflict"}
	}
	s.version++
	versionID := fmt.Sprintf("policy-version-%d", s.version)
	s.current = metadatasdk.Definition{
		Owner: command.Owner, ResourceType: command.ResourceType, ResourceKey: command.ResourceKey,
		CurrentVersionID: versionID, Status: "active", Payload: append(json.RawMessage(nil), command.Payload...),
		SchemaVersion: command.SchemaVersion, SourceKind: command.SourceKind, SourceID: command.SourceID,
		PublishedBy: command.PublishedBy,
	}
	return metadatasdk.DefinitionPublishResult{Definition: s.current, CurrentVersionID: versionID}, nil
}
func (s *policyDefinitionStore) Disable(context.Context, metadatasdk.DefinitionDisableCommand) error {
	return nil
}
func (s *policyDefinitionStore) GetVersion(context.Context, metadatasdk.DefinitionVersionQuery) (metadatasdk.DefinitionVersion, bool, error) {
	return metadatasdk.DefinitionVersion{}, false, nil
}
