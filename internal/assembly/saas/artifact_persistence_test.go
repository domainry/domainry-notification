package saas

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	sqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/artifactkernel"
	_ "modernc.org/sqlite"
)

type artifactTestGateway struct{}

func (artifactTestGateway) Dispatch(context.Context, modulehost.DeliveryRequest) (modulehost.DeliveryReceipt, error) {
	return modulehost.DeliveryReceipt{MessageID: "message"}, nil
}

func TestStandaloneRetentionArchiveUsesSharedArtifactPersistence(t *testing.T) {
	database, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	persistence, err := NewSQLPersistence(SQLPersistenceOptions{Database: database, Driver: sqlstore.SQLite})
	if err != nil {
		t.Fatal(err)
	}
	application := notificationsdk.ApplicationRef{WorkspaceID: "workspace-a", ApplicationKey: "application-a"}
	dialect, err := persistence.PrepareApplication(t.Context(), application)
	if err != nil {
		t.Fatal(err)
	}
	content, err := artifactkernel.NewContentFiles(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	defer content.Close()
	archives, err := persistence.PrepareRetentionArchiveStore(t.Context(), dialect, content)
	if err != nil {
		t.Fatal(err)
	}
	created, err := archives.ArchivePayload(t.Context(), modulehost.RetentionArchiveOwnerNotification,
		modulehost.RetentionArchiveJob{ID: "cleanup-one", WorkspaceID: application.WorkspaceID, ArchivedAt: time.Date(2026, 9, 23, 8, 0, 0, 0, time.UTC)},
		modulehost.RetentionArchivePolicy{Key: "notification.history", Version: "one"},
		"_notification_events", "event-one", []byte(`{"id":"event-one"}`))
	if err != nil || !created {
		t.Fatalf("archive created=%v err=%v", created, err)
	}
	created, err = archives.ArchivePayload(t.Context(), modulehost.RetentionArchiveOwnerNotification,
		modulehost.RetentionArchiveJob{ID: "cleanup-one", WorkspaceID: application.WorkspaceID, ArchivedAt: time.Date(2026, 9, 23, 8, 0, 0, 0, time.UTC)},
		modulehost.RetentionArchivePolicy{Key: "notification.history", Version: "one"},
		"_notification_events", "event-one", []byte(`{"id":"event-one"}`))
	if err != nil || created {
		t.Fatalf("archive replay created=%v err=%v", created, err)
	}
	for _, table := range []string{"_artifacts", "_artifact_bindings"} {
		var count int
		if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("shared table %q count=%d err=%v", table, count, err)
		}
	}
	var retired int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = '_lifecycle_archive_entries'`).Scan(&retired); err != nil || retired != 0 {
		t.Fatalf("retired archive table count=%d err=%v", retired, err)
	}
	var migrationRows int
	if err := database.QueryRow(`SELECT COUNT(*) FROM _schema_migrations WHERE namespace = 'shared/artifacts'`).Scan(&migrationRows); err != nil || migrationRows != 1 {
		t.Fatalf("shared Artifact migration rows=%d err=%v", migrationRows, err)
	}
}

func TestSQLApplicationFactoryRequiresDefaultArtifactContent(t *testing.T) {
	database, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	persistence, err := NewSQLPersistence(SQLPersistenceOptions{Database: database, Driver: sqlstore.SQLite})
	if err != nil {
		t.Fatal(err)
	}
	options := SQLApplicationFactoryOptions{
		Persistence: persistence, Catalog: modulehost.Catalog{DefaultLocale: "en"}, WorkerID: "worker", DeliveryGateway: artifactTestGateway{},
	}
	if _, err := NewSQLApplicationFactory(options); err == nil {
		t.Fatal("factory accepted a default archive store without Artifact content storage")
	}
	content, err := artifactkernel.NewContentFiles(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	defer content.Close()
	options.ArtifactContent = content
	if _, err := NewSQLApplicationFactory(options); err != nil {
		t.Fatal(err)
	}
}
