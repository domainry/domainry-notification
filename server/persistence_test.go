package server

import (
	"database/sql"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification/sqlstore"

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
	application := notificationsdk.ApplicationRef{TenantID: "tenant-one", WorkspaceID: "workspace-one", ApplicationKey: "app-one"}
	first, err := persistence.PrepareApplication(t.Context(), application)
	if err != nil {
		t.Fatal(err)
	}
	second, err := persistence.PrepareApplication(t.Context(), application)
	if err != nil {
		t.Fatal(err)
	}
	if first.Table("notification_events") != second.Table("notification_events") {
		t.Fatalf("application namespace changed: %s != %s", first.Table("notification_events"), second.Table("notification_events"))
	}
	var migrations int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notification_saas_schema_migrations WHERE namespace = ?`, applicationKey(application)).Scan(&migrations); err != nil || migrations != 3 {
		t.Fatalf("migration count=%d err=%v", migrations, err)
	}
	var columns int
	table := strings.Trim(first.Table("notification_events"), `"`)
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name IN ('tenant_id', 'application_key', 'workspace_id')`, table).Scan(&columns); err != nil || columns != 3 {
		t.Fatalf("ownership columns=%d err=%v", columns, err)
	}
}

func TestSQLPersistenceSeparatesApplicationsWithSameWorkspace(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	persistence, err := NewSQLPersistence(SQLPersistenceOptions{Database: db, Driver: sqlstore.SQLite})
	if err != nil {
		t.Fatal(err)
	}
	left := notificationsdk.ApplicationRef{TenantID: "tenant", WorkspaceID: "workspace", ApplicationKey: "left"}
	right := notificationsdk.ApplicationRef{TenantID: "tenant", WorkspaceID: "workspace", ApplicationKey: "right"}
	leftDialect, err := persistence.PrepareApplication(t.Context(), left)
	if err != nil {
		t.Fatal(err)
	}
	rightDialect, err := persistence.PrepareApplication(t.Context(), right)
	if err != nil {
		t.Fatal(err)
	}
	if leftDialect.Table("notification_events") == rightDialect.Table("notification_events") {
		t.Fatal("two applications share one physical Notification table")
	}
	var migrations int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notification_saas_schema_migrations`).Scan(&migrations); err != nil || migrations != 6 {
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
	application := notificationsdk.ApplicationRef{TenantID: "tenant", WorkspaceID: "workspace", ApplicationKey: "application"}
	if _, err := persistence.PrepareApplication(t.Context(), application); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE notification_saas_schema_migrations SET checksum = 'different' WHERE namespace = ?`, applicationKey(application)); err != nil {
		t.Fatal(err)
	}
	if _, err := persistence.PrepareApplication(t.Context(), application); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("error=%v", err)
	}
}

func TestApplicationTablePrefixDoesNotExposeScopeValues(t *testing.T) {
	application := notificationsdk.ApplicationRef{TenantID: "tenant-secret", WorkspaceID: "workspace-secret", ApplicationKey: "application-secret"}
	prefix := applicationTablePrefix(application)
	if !strings.HasPrefix(prefix, "notification_") || !strings.HasSuffix(prefix, "_") || strings.Contains(prefix, "secret") {
		t.Fatalf("unsafe prefix %q", prefix)
	}
}

func TestMigrationLockKeyIsDeterministicBoundedAndOpaque(t *testing.T) {
	first := migrationLockKey("tenant-secret/workspace-secret/application-secret")
	second := migrationLockKey("tenant-secret/workspace-secret/application-secret")
	other := migrationLockKey("tenant-secret/workspace-secret/other")
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
			key := migrationLockKey("tenant/workspace/application")
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
			persistence := &SQLPersistence{driver: test.driver}
			release, err := persistence.acquireMigrationLock(t.Context(), connection, "tenant/workspace/application")
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
