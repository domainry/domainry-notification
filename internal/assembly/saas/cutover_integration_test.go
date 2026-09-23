package saas

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/contract"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	"github.com/domainry/domainry-notification/internal/assembly/module"
	sqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence"
	ormdialect "github.com/domainry/domainry-orm/dialect"
)

type cutoverMigrationRegistrar struct {
	database *sql.DB
	applied  map[string]bool
}

func (*cutoverMigrationRegistrar) Driver() string { return "sqlite" }
func (*cutoverMigrationRegistrar) Schema() string { return "" }
func (r *cutoverMigrationRegistrar) ApplyOwnedMigrations(ctx context.Context, owner string, migrations []modulehost.SchemaMigration) error {
	if r.applied == nil {
		r.applied = map[string]bool{}
	}
	if r.applied[owner] {
		return nil
	}
	for _, migration := range migrations {
		for _, statement := range migration.Statements {
			if _, err := r.database.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
	}
	r.applied[owner] = true
	return nil
}

type cutoverModuleHost struct {
	*saasApplicationHost
	migrations *cutoverMigrationRegistrar
}

func (h *cutoverModuleHost) Migrations() modulehost.MigrationRegistrar { return h.migrations }

func TestModuleToSaaSCutoverPreservesStateAndMovesTheOnlyWriter(t *testing.T) {
	application := notificationsdk.ApplicationRef{WorkspaceID: "workspace", ApplicationKey: "runtime"}
	catalog := modulehost.Catalog{
		DefaultLocale: "en", TemplateCapabilities: []contract.NotificationTemplateCapability{{Channel: "in_app"}},
		EventTypes: []contract.NotificationEventType{{Key: "report.completed", Source: "report", Category: "report", DefaultSeverity: "info", MandatoryInApp: true, TemplateKey: "report.completed", DefaultLocale: "en", Locales: map[string]contract.NotificationInboxEventTypeContent{"en": {Title: "Report ready", Body: "The report is ready."}}, Version: 1, Status: "published"}},
	}
	sourceDB, err := sql.Open("sqlite", "file:"+t.Name()+"-module?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	sourceDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sourceDB.Close() })
	sourceDialect, _ := ormdialect.ParseRenderer("sqlite", "", "")
	sourceOperations, sourceArchives := prepareCutoverSharedStores(t, sourceDB, sourceDialect)
	sourceHost := &cutoverModuleHost{
		saasApplicationHost: &saasApplicationHost{application: application, database: sourceDB, dialect: sourceDialect, identity: applicationIdentityStub{}, catalog: catalog, clock: wallClock{}, workerID: "module-worker", notifier: discardWorkNotifier{}, projection: identityRecipientResolver{application: application, projection: projectionStub{}}, audiences: snapshotOnlyAudienceResolver{}, gateway: applicationGatewayStub{}, operations: sourceOperations, controls: sourceOperations.(modulehost.OperationControlStore), archives: sourceArchives},
		migrations:          &cutoverMigrationRegistrar{database: sourceDB},
	}
	source, err := module.NewFactory(module.Options{}).OpenModule(t.Context(), application, sourceHost)
	if err != nil {
		t.Fatal(err)
	}
	intent := contract.NotificationIntent{ID: "event-one", WorkspaceID: "workspace", SourceEventID: "report:one", EventType: "report.completed", RecipientUserIDs: []string{"user"}, OccurredAt: "2026-08-29T03:00:00Z", SubjectType: "report", SubjectID: "one", SubjectVersion: "1"}
	sourceEvent, created, err := source.Publisher().PublishIntent(t.Context(), intent)
	if err != nil || !created {
		t.Fatalf("source event=%+v created=%v err=%v", sourceEvent, created, err)
	}
	sourceMigration := source.(notificationsdk.SystemMigrationBinding).SystemMigration()
	command := contract.NotificationMigrationCommand{MigrationID: "module-to-saas", At: time.Date(2026, 8, 29, 3, 1, 0, 0, time.UTC)}
	if _, err := sourceMigration.Freeze(t.Context(), command); err != nil {
		t.Fatal(err)
	}
	exported, err := sourceMigration.Export(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	targetDB, err := sql.Open("sqlite", "file:"+t.Name()+"-saas?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	targetDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = targetDB.Close() })
	persistence, _ := NewSQLPersistence(SQLPersistenceOptions{Database: targetDB, Driver: sqlstore.SQLite})
	targetFactory, err := NewSQLApplicationFactory(SQLApplicationFactoryOptions{Persistence: persistence, Catalog: catalog, WorkerID: "saas-worker", DeliveryGateway: applicationGatewayStub{}, ArtifactContent: newTestArtifactContent(t)})
	if err != nil {
		t.Fatal(err)
	}
	target, err := targetFactory.OpenSaaS(t.Context(), application, applicationIdentityStub{})
	if err != nil {
		t.Fatal(err)
	}
	targetMigration := target.(notificationsdk.SystemMigrationBinding).SystemMigration()
	receipt, err := targetMigration.Import(t.Context(), exported.Bundle)
	if err != nil || receipt.Fingerprint != exported.Bundle.Fingerprint {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	activate := contract.NotificationMigrationCommand{MigrationID: command.MigrationID, BundleFingerprint: exported.Bundle.Fingerprint, At: command.At.Add(time.Minute)}
	if status, err := targetMigration.Activate(t.Context(), activate); err != nil || status.State != contract.NotificationMigrationCutover {
		t.Fatalf("activation=%+v err=%v", status, err)
	}
	duplicate, created, err := target.Publisher().PublishIntent(t.Context(), intent)
	if err != nil || created || duplicate.ID != sourceEvent.ID {
		t.Fatalf("target duplicate=%+v created=%v err=%v", duplicate, created, err)
	}
	intent.ID, intent.SourceEventID, intent.SubjectID = "event-two", "report:two", "two"
	if _, created, err := target.Publisher().PublishIntent(t.Context(), intent); err != nil || !created {
		t.Fatalf("target new publication created=%v err=%v", created, err)
	}
	if _, _, err := source.Publisher().PublishIntent(t.Context(), intent); err == nil {
		t.Fatal("frozen Module source resumed writing after SaaS activation")
	} else {
		var sdkError *notificationsdk.Error
		if !errors.As(err, &sdkError) || sdkError.Code != "notification.migration_writes_frozen" {
			t.Fatalf("source write error=%v", err)
		}
	}
}

func prepareCutoverSharedStores(t *testing.T, database *sql.DB, dialect modulehost.Dialect) (modulehost.ManagedOperationStore, modulehost.RetentionArchiveStore) {
	t.Helper()
	persistence, err := NewSQLPersistence(SQLPersistenceOptions{Database: database, Driver: sqlstore.SQLite})
	if err != nil {
		t.Fatal(err)
	}
	operations, err := persistence.PrepareManagedOperationStore(t.Context(), dialect)
	if err != nil {
		t.Fatal(err)
	}
	archives, err := persistence.PrepareRetentionArchiveStore(t.Context(), dialect, newTestArtifactContent(t))
	if err != nil {
		t.Fatal(err)
	}
	return operations, archives
}
