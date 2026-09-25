package migration

import (
	"database/sql"
	"strings"
	"testing"

	ormdialect "github.com/domainry/domainry-orm/dialect"
	_ "modernc.org/sqlite"
)

func TestApplicationBindingIgnoresSharedMigrationNamespaces(t *testing.T) {
	database, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	renderer, err := ormdialect.ParseRenderer("sqlite", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ledger := NewLedger(renderer)
	if err := ledger.Ensure(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := ledger.RecordDirty(t.Context(), database, "shared/metadata", 1, "metadata", "metadata-checksum"); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Complete(t.Context(), database, "shared/metadata", 1, "metadata-checksum", 1); err != nil {
		t.Fatal(err)
	}
	if err := ledger.RecordDirty(t.Context(), database, "workspace/app", 1, "notification", "notification-checksum"); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Complete(t.Context(), database, "workspace/app", 1, "notification-checksum", 1); err != nil {
		t.Fatal(err)
	}
	if err := ledger.ValidateApplicationBinding(t.Context(), database, "workspace/app"); err != nil {
		t.Fatalf("shared namespace changed application binding: %v", err)
	}
	if err := ledger.ValidateApplicationBinding(t.Context(), database, "workspace/other"); err == nil || !strings.Contains(err.Error(), "already bound") {
		t.Fatalf("second application binding error=%v", err)
	}
}
