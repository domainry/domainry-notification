package timejson

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type fixture struct {
	CreatedAt string          `json:"created_at"`
	ExpiresAt string          `json:"expires_at,omitempty"`
	Metadata  map[string]any  `json:"metadata"`
	Opaque    json.RawMessage `json:"opaque"`
}

func TestOwnedTimesUseStrictUnixMilliseconds(t *testing.T) {
	instant := time.Date(2026, 9, 25, 4, 5, 6, 789000000, time.UTC)
	raw, err := Marshal(fixture{CreatedAt: instant.Format(time.RFC3339Nano), Metadata: map[string]any{"updated_at": instant}, Opaque: json.RawMessage(`{"occurred_at":"provider-owned"}`)})
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	millis := "1790309106789"
	if !strings.Contains(text, `"created_at":`+millis) || !strings.Contains(text, `"updated_at":`+millis) || !strings.Contains(text, `"occurred_at":"provider-owned"`) {
		t.Fatalf("unexpected durable JSON: %s", text)
	}
	var decoded fixture
	if err := Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	want := instant.Format(timestampLayout)
	if decoded.CreatedAt != want || decoded.Metadata["updated_at"] != want {
		t.Fatalf("unexpected decoded value: %#v", decoded)
	}
	if err := Unmarshal([]byte(`{"created_at":"2026-09-25T04:05:06Z","metadata":{},"opaque":{}}`), &decoded); err == nil {
		t.Fatal("accepted a string-encoded durable instant")
	}
}
