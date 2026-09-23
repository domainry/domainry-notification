package artifactkernel

import (
	"database/sql"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sharedartifact "github.com/domainry/domainry-foundation/artifact"
	ormdialect "github.com/domainry/domainry-orm/dialect"
	_ "modernc.org/sqlite"
)

func TestStandaloneArtifactKernelPersistsMetadataBindingsAndImmutableContent(t *testing.T) {
	database, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	renderer, err := ormdialect.ParseRenderer("sqlite", "", "")
	if err != nil {
		t.Fatal(err)
	}
	migrations, err := SchemaMigrations(renderer)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range migrations[0].Statements {
		if _, err = database.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	files, err := NewContentFiles(filepath.Join(t.TempDir(), "content"))
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	content := []byte(`{"id":"notification-one"}`)
	info, err := files.PutImmutable(t.Context(), "workspace-a", "archive-one", content)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(database, renderer)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	value, created, err := store.Register(t.Context(), sharedartifact.Artifact{
		ID: "lifecycle_archive_one", WorkspaceID: "workspace-a", Owner: sharedartifact.OwnerLifecycle, Kind: "archive",
		IdempotencyKey: "lifecycle_archive_one", CreatedBy: "notification_retention", Filename: "archive.json", MediaType: "application/json",
		ContentSHA256: info.SHA256, SizeBytes: info.Size, StorageReference: info.Reference,
		Status: sharedartifact.StatusAvailable, ScanStatus: sharedartifact.ScanNotRequired,
		DownloadTokenSHA256: strings.Repeat("a", 64), Metadata: []byte(`{"owner":"notification"}`), CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || !created {
		t.Fatalf("register created=%v err=%v", created, err)
	}
	if _, created, err = store.Register(t.Context(), value); err != nil || created {
		t.Fatalf("register replay created=%v err=%v", created, err)
	}
	binding := sharedartifact.Binding{
		ID: value.ID + ":source", WorkspaceID: value.WorkspaceID, ArtifactID: value.ID,
		Owner: sharedartifact.OwnerLifecycle, Kind: sharedartifact.BindingObjectField,
		ResourceType: "_notification_events", ResourceID: "notification-one", FieldKey: "notification.history",
		Metadata: []byte(`{}`), CreatedAt: now,
	}
	if _, created, err = store.Bind(t.Context(), binding); err != nil || !created {
		t.Fatalf("bind created=%v err=%v", created, err)
	}
	if _, created, err = store.Bind(t.Context(), binding); err != nil || created {
		t.Fatalf("bind replay created=%v err=%v", created, err)
	}
	bindings, err := store.Bindings(t.Context(), value.WorkspaceID, value.ID)
	if err != nil || len(bindings) != 1 || bindings[0].ResourceID != binding.ResourceID {
		t.Fatalf("bindings=%+v err=%v", bindings, err)
	}
	byToken, found, err := store.ByDownloadTokenHash(t.Context(), value.WorkspaceID, value.DownloadTokenSHA256)
	if err != nil || !found || byToken.ID != value.ID {
		t.Fatalf("by token=%+v found=%v err=%v", byToken, found, err)
	}
	reader, err := files.Open(t.Context(), value.WorkspaceID, value.StorageReference)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(raw) != string(content) {
		t.Fatalf("content=%q err=%v", raw, err)
	}
	if err = files.Delete(t.Context(), value.WorkspaceID, value.StorageReference); err != nil {
		t.Fatal(err)
	}
	if err = files.Delete(t.Context(), value.WorkspaceID, value.StorageReference); err != nil {
		t.Fatalf("idempotent delete: %v", err)
	}
}

func TestArtifactKernelSchemaIsPortableAndRetiresPrivateArchiveTable(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres", "mysql"} {
		t.Run(driver, func(t *testing.T) {
			renderer, err := ormdialect.ParseRenderer(driver, "", "")
			if err != nil {
				t.Fatal(err)
			}
			migrations, err := SchemaMigrations(renderer)
			if err != nil || len(migrations) != 1 {
				t.Fatalf("migrations=%+v err=%v", migrations, err)
			}
			joined := strings.Join(migrations[0].Statements, "\n")
			if !strings.Contains(joined, artifactTable) || !strings.Contains(joined, bindingTable) || strings.Contains(joined, "_lifecycle_archive_entries") {
				t.Fatalf("invalid shared Artifact schema:\n%s", joined)
			}
		})
	}
}
