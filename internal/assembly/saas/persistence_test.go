package saas

import (
	"database/sql"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
	sqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence"
	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"

	_ "modernc.org/sqlite"
)

func TestSQLPersistencePreparesAndReopensExactApplication(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	persistence, err := NewSQLPersistence(SQLPersistenceOptions{Database: db, Driver: sqlstore.SQLite})
	if err != nil {
		t.Fatal(err)
	}
	application := notificationsdk.ApplicationRef{WorkspaceID: "workspace-one", ApplicationKey: "app-one"}
	first, err := persistence.PrepareApplication(t.Context(), application)
	if err != nil {
		t.Fatal(err)
	}
	second, err := persistence.PrepareApplication(t.Context(), application)
	if err != nil {
		t.Fatal(err)
	}
	if first.Table("_notification_events") != second.Table("_notification_events") {
		t.Fatalf("application namespace changed: %s != %s", first.Table("_notification_events"), second.Table("_notification_events"))
	}
	var migrations int
	if err := db.QueryRow(`SELECT COUNT(*) FROM _schema_migrations WHERE namespace = ?`, applicationKey(application)).Scan(&migrations); err != nil || migrations != 1 {
		t.Fatalf("migration count=%d err=%v", migrations, err)
	}
	var columns int
	table := strings.Trim(first.Table("_notification_events"), `"`)
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name IN ('application_key', 'workspace_id')`, table).Scan(&columns); err != nil || columns != 2 {
		t.Fatalf("ownership columns=%d err=%v", columns, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = 'tenant_id'`, table).Scan(&columns); err != nil || columns != 0 {
		t.Fatalf("unexpected tenant column=%d err=%v", columns, err)
	}
}

func TestSQLPersistenceRejectsASecondApplicationInStandaloneDatabase(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	persistence, err := NewSQLPersistence(SQLPersistenceOptions{Database: db, Driver: sqlstore.SQLite})
	if err != nil {
		t.Fatal(err)
	}
	left := notificationsdk.ApplicationRef{WorkspaceID: "workspace", ApplicationKey: "left"}
	right := notificationsdk.ApplicationRef{WorkspaceID: "workspace", ApplicationKey: "right"}
	leftDialect, err := persistence.PrepareApplication(t.Context(), left)
	if err != nil {
		t.Fatal(err)
	}
	if leftDialect.Table("_notification_events") != `"_notification_events"` {
		t.Fatalf("standalone table=%s", leftDialect.Table("_notification_events"))
	}
	if _, err := persistence.PrepareApplication(t.Context(), right); err == nil || !strings.Contains(err.Error(), "already bound") {
		t.Fatalf("second application error=%v", err)
	}
	var migrations int
	if err := db.QueryRow(`SELECT COUNT(*) FROM _schema_migrations`).Scan(&migrations); err != nil || migrations != 1 {
		t.Fatalf("migration count=%d err=%v", migrations, err)
	}
}

func TestSQLPersistenceRejectsMigrationChecksumDrift(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	persistence, err := NewSQLPersistence(SQLPersistenceOptions{Database: db, Driver: sqlstore.SQLite})
	if err != nil {
		t.Fatal(err)
	}
	application := notificationsdk.ApplicationRef{WorkspaceID: "workspace", ApplicationKey: "application"}
	if _, err := persistence.PrepareApplication(t.Context(), application); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE _schema_migrations SET checksum = 'different' WHERE namespace = ?`, applicationKey(application)); err != nil {
		t.Fatal(err)
	}
	if _, err := persistence.PrepareApplication(t.Context(), application); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("error=%v", err)
	}
}

func TestSQLPersistenceKeepsFailedMigrationDirtyAcrossRestart(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	persistence, err := NewSQLPersistence(SQLPersistenceOptions{Database: db, Driver: sqlstore.SQLite})
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.ensureLedger(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	broken := sqlstore.SchemaMigration{Version: 99, Name: "broken", Statements: []string{"CREATE TABLE"}}
	if err := persistence.applyMigration(t.Context(), db, "workspace/app", broken); err == nil {
		t.Fatal("broken migration unexpectedly succeeded")
	}
	var dirty bool
	if err := db.QueryRowContext(t.Context(), `SELECT dirty FROM _schema_migrations WHERE namespace=? AND version=?`, "workspace/app", 99).Scan(&dirty); err != nil || !dirty {
		t.Fatalf("dirty=%v err=%v", dirty, err)
	}
	repaired := sqlstore.SchemaMigration{Version: 99, Name: "broken", Statements: []string{"CREATE TABLE repaired_probe (id TEXT PRIMARY KEY)"}}
	if err := persistence.applyMigration(t.Context(), db, "workspace/app", repaired); err == nil || !strings.Contains(err.Error(), "is dirty") {
		t.Fatalf("dirty restart error=%v", err)
	}
}

func TestMigrationLockKeyIsDeterministicBoundedAndOpaque(t *testing.T) {
	first := base.MigrationLockKey("tenant-secret/workspace-secret/application-secret")
	second := base.MigrationLockKey("tenant-secret/workspace-secret/application-secret")
	other := base.MigrationLockKey("tenant-secret/workspace-secret/other")
	if first != second || first == other || len(first) > 64 || strings.Contains(first, "secret") {
		t.Fatalf("lock keys first=%q second=%q other=%q", first, second, other)
	}
}

func TestMigrationLocksUseOneDatabaseSession(t *testing.T) {
	for _, test := range []struct {
		name, acquire, release string
		driver                 sqlstore.Driver
		acquireRow, releaseRow any
	}{
		{name: "postgres", driver: sqlstore.Postgres, acquire: `SELECT pg_advisory_lock(hashtextextended($1, 0))`, release: `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, acquireRow: nil, releaseRow: true},
		{name: "mysql", driver: sqlstore.MySQL, acquire: `SELECT GET_LOCK(?, ?)`, release: `SELECT RELEASE_LOCK(?)`, acquireRow: int64(1), releaseRow: int64(1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			key := base.MigrationLockKey("tenant/workspace/application")
			var acquire *sqlmock.ExpectedQuery
			if test.driver == sqlstore.MySQL {
				acquire = mock.ExpectQuery(regexp.QuoteMeta(test.acquire)).WithArgs(key, 30)
			} else {
				acquire = mock.ExpectQuery(regexp.QuoteMeta(test.acquire)).WithArgs(key)
			}
			acquire.WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(test.acquireRow))
			mock.ExpectQuery(regexp.QuoteMeta(test.release)).WithArgs(key).WillReturnRows(sqlmock.NewRows([]string{"released"}).AddRow(test.releaseRow))
			connection, err := db.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			locker, err := sqlstore.NewEngine(test.driver)
			if err != nil {
				t.Fatal(err)
			}
			release, err := locker.Acquire(t.Context(), connection, "tenant/workspace/application")
			if err != nil {
				t.Fatal(err)
			}
			if err := release(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
