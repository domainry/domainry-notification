package lifecyclestore_test

import (
	"encoding/json"
	"testing"

	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"
	lifecyclestore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/lifecycle"
	ormdialect "github.com/domainry/domainry-orm/dialect"
)

func TestSubjectLifecyclePreviewExportAndEraseOwnedRows(t *testing.T) {
	database, _ := migratedStore(t)
	if _, err := database.Exec(`INSERT INTO _notification_user_settings (workspace_id, recipient_user_id, setting_kind, setting_key, payload_json, updated_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, "workspace", "user", "delivery_preference", "default", `{}`, "user", int64(1), int64(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO _notification_inbox_delegations (delegation_id, workspace_id, owner_user_id, delegate_user_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, "delegation", "workspace", "user", "delegate", int64(1), int64(1)); err != nil {
		t.Fatal(err)
	}
	for _, recipient := range []string{"user", "another-user"} {
		if _, err := database.Exec(`INSERT INTO _notification_delivery_reservations (workspace_id, reservation_key, recipient_key, template_key, dedupe_key, channel, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "workspace", "reservation-"+recipient, recipient, "template", "dedupe-"+recipient, "email", int64(1)); err != nil {
			t.Fatal(err)
		}
	}
	dialect, _ := ormdialect.ParseRenderer("sqlite", "", "")
	store := lifecyclestore.New(lifecyclestore.Config{SQLStore: base.NewSQLStore(database, dialect)})
	preview, err := store.PreviewSubject(t.Context(), "workspace", "user")
	if err != nil {
		t.Fatal(err)
	}
	var counts map[string]int64
	if err := json.Unmarshal(preview, &counts); err != nil || counts["preferences"] != 1 || counts["delegations"] != 1 || counts["delivery_reservations"] != 1 {
		t.Fatalf("preview=%s counts=%+v err=%v", preview, counts, err)
	}
	exported, err := store.ExportSubject(t.Context(), "workspace", "user")
	if err != nil || !json.Valid(exported) {
		t.Fatalf("export=%s err=%v", exported, err)
	}
	receipt, err := store.EraseSubject(t.Context(), "workspace", "user", json.RawMessage(`[]`))
	if err != nil || !json.Valid(receipt) {
		t.Fatalf("receipt=%s err=%v", receipt, err)
	}
	preview, err = store.PreviewSubject(t.Context(), "workspace", "user")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(preview, &counts); err != nil || counts["preferences"] != 0 || counts["delegations"] != 0 || counts["delivery_reservations"] != 0 {
		t.Fatalf("post-erase preview=%s counts=%+v err=%v", preview, counts, err)
	}
}

func TestSubjectLifecycleRequiresExactScope(t *testing.T) {
	store := lifecyclestore.New(lifecyclestore.Config{})
	if _, err := store.PreviewSubject(t.Context(), "", "user"); err == nil {
		t.Fatal("blank workspace was accepted")
	}
	if _, err := store.EraseSubject(t.Context(), "workspace", "", nil); err == nil {
		t.Fatal("blank subject was accepted")
	}
}

func TestSubjectExportSeparatesPhysicalAndBusinessIDs(t *testing.T) {
	database, _ := migratedStore(t)
	for _, item := range []struct{ workspace, recipient, id string }{
		{"workspace", "user", "notice-owned"},
		{"workspace", "other", "notice-other"},
		{"other-workspace", "user", "notice-foreign"},
	} {
		if _, err := database.Exec(`INSERT INTO _notification_inbox_items (item_id,item_key,workspace_id,recipient_user_id,event_id,event_type,source,category,severity,title,body,search_text,payload_json,action_state,first_occurred_at,last_occurred_at,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, item.id, item.id, item.workspace, item.recipient, "event", "test", "test", "test", "info", "title", "body", "title body", `{}`, "none", int64(1), int64(2), int64(1), int64(2)); err != nil {
			t.Fatal(err)
		}
	}
	dialect, _ := ormdialect.ParseRenderer("sqlite", "", "")
	store := lifecyclestore.New(lifecyclestore.Config{SQLStore: base.NewSQLStore(database, dialect)})
	raw, err := store.ExportSubject(t.Context(), "workspace", "user")
	if err != nil {
		t.Fatal(err)
	}
	var exported struct {
		Items []struct {
			ID     int64  `json:"id"`
			ItemID string `json:"item_id"`
		} `json:"inbox_items"`
	}
	if err := json.Unmarshal(raw, &exported); err != nil {
		t.Fatal(err)
	}
	if len(exported.Items) != 1 || exported.Items[0].ID <= 0 || exported.Items[0].ItemID != "notice-owned" {
		t.Fatalf("export=%s", raw)
	}
}
