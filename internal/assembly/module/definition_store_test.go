package module

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	metadatasdk "github.com/domainry/domainry-metadata-sdk"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	sqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/artifactkernel"
	operationstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/operation"
	retentionarchivestore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/retentionarchive"
)

type testDefinitionStore struct {
	mu      sync.Mutex
	current map[string]metadatasdk.Definition
	version int
}

func newTestDefinitionStore() *testDefinitionStore {
	return &testDefinitionStore{current: map[string]metadatasdk.Definition{}}
}

func definitionIdentity(owner, kind, key string) string {
	return strings.Join([]string{owner, kind, key}, "\x00")
}

func (s *testDefinitionStore) List(_ context.Context, query metadatasdk.DefinitionQuery) ([]metadatasdk.Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := []metadatasdk.Definition{}
	for _, value := range s.current {
		if query.Owner == value.Owner && (query.ResourceType == "" || query.ResourceType == value.ResourceType) {
			result = append(result, value)
		}
	}
	return result, nil
}

func (s *testDefinitionStore) Get(_ context.Context, owner, kind, key string) (metadatasdk.Definition, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, found := s.current[definitionIdentity(owner, kind, key)]
	return value, found, nil
}

func (s *testDefinitionStore) Snapshot(ctx context.Context, query metadatasdk.DefinitionQuery) (metadatasdk.DefinitionSnapshot, error) {
	values, err := s.List(ctx, query)
	return metadatasdk.DefinitionSnapshot{Definitions: values}, err
}

func (*testDefinitionStore) ReplaceSourceSnapshot(context.Context, metadatasdk.ProjectionSnapshot) error {
	return nil
}

func (s *testDefinitionStore) Publish(_ context.Context, command metadatasdk.DefinitionPublishCommand) (metadatasdk.DefinitionPublishResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	identity := definitionIdentity(command.Owner, command.ResourceType, command.ResourceKey)
	current, found := s.current[identity]
	expected := metadatasdk.DefinitionNoCurrentVersion
	if found {
		expected = current.CurrentVersionID
	}
	if command.ExpectedCurrentVersionID != expected {
		return metadatasdk.DefinitionPublishResult{}, &metadatasdk.Error{StatusCode: 409, Code: "metadata.definition_revision_conflict"}
	}
	s.version++
	versionID := fmt.Sprintf("definition-version-%d", s.version)
	value := metadatasdk.Definition{
		Owner: command.Owner, ResourceType: command.ResourceType, ResourceKey: command.ResourceKey,
		CurrentVersionID: versionID, Status: "active", Name: command.Name,
		Payload: append(json.RawMessage(nil), command.Payload...), SchemaVersion: command.SchemaVersion,
		SourceKind: command.SourceKind, SourceID: command.SourceID, PublishedBy: command.PublishedBy,
	}
	s.current[identity] = value
	return metadatasdk.DefinitionPublishResult{Definition: value, CurrentVersionID: versionID}, nil
}

func (*testDefinitionStore) Disable(context.Context, metadatasdk.DefinitionDisableCommand) error {
	return nil
}

func (*testDefinitionStore) GetVersion(context.Context, metadatasdk.DefinitionVersionQuery) (metadatasdk.DefinitionVersion, bool, error) {
	return metadatasdk.DefinitionVersion{}, false, nil
}

func newTestManagedOperationStore(t *testing.T, database *sql.DB, dialect modulehost.Dialect) modulehost.ManagedOperationStore {
	t.Helper()
	migrations, err := sqlstore.SharedOperationSchemaMigrations(sqlstore.SQLite, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		for _, statement := range migration.Statements {
			if _, err := database.Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	store, err := operationstore.New(base.NewSQLStore(database, dialect))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func newTestRetentionArchiveStore(t *testing.T, database *sql.DB, dialect modulehost.Dialect) modulehost.RetentionArchiveStore {
	t.Helper()
	migrations, err := sqlstore.SharedArtifactSchemaMigrations(sqlstore.SQLite, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		for _, statement := range migration.Statements {
			if _, err := database.Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	artifacts, err := artifactkernel.NewStore(database, dialect)
	if err != nil {
		t.Fatal(err)
	}
	content, err := artifactkernel.NewContentFiles(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = content.Close() })
	store, err := retentionarchivestore.New(artifacts, content)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
