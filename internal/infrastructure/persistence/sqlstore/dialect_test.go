package sqlstore_test

import (
	"testing"

	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/sqlstore"
)

func TestStandaloneDialectsQuoteAndNumberSafely(t *testing.T) {
	postgres, err := sqlstore.NewDialect(sqlstore.Postgres, "runtime", "dr_")
	if err != nil {
		t.Fatal(err)
	}
	query := postgres.Insert("notification_events", []string{"id", "workspace_id"})
	want := `INSERT INTO "runtime"."dr_notification_events" ("id", "workspace_id") VALUES ($1, $2)`
	if query != want {
		t.Fatalf("query=%q want=%q", query, want)
	}
	mysql, err := sqlstore.NewDialect(sqlstore.MySQL, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if query := mysql.Insert("notification_events", []string{"id"}); query != "INSERT INTO `notification_events` (`id`) VALUES (?)" {
		t.Fatalf("mysql query=%q", query)
	}
}

func TestStandaloneDialectRejectsUnsafeConfiguration(t *testing.T) {
	if _, err := sqlstore.NewDialect(sqlstore.SQLite, "runtime;drop", ""); err == nil {
		t.Fatal("unsafe schema was accepted")
	}
	if _, err := sqlstore.NewDialect("oracle", "", ""); err == nil {
		t.Fatal("unsupported driver was accepted")
	}
}
