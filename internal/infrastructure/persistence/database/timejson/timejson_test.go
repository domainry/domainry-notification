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
	raw, err := Marshal(fixture{CreatedAt: instant.Format(time.RFC3339Nano), Metadata: map[string]any{"updated_at": "provider-owned"}, Opaque: json.RawMessage(`{"occurred_at":"provider-owned"}`)})
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	millis := "1790309106789"
	if !strings.Contains(text, `"created_at":`+millis) || !strings.Contains(text, `"updated_at":"provider-owned"`) || !strings.Contains(text, `"occurred_at":"provider-owned"`) {
		t.Fatalf("unexpected durable JSON: %s", text)
	}
	var decoded fixture
	if err := Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	want := instant.Format(timestampLayout)
	if decoded.CreatedAt != want || decoded.Metadata["updated_at"] != "provider-owned" {
		t.Fatalf("unexpected decoded value: %#v", decoded)
	}
	if err := Unmarshal([]byte(`{"created_at":"2026-09-25T04:05:06Z","metadata":{},"opaque":{}}`), &decoded); err == nil {
		t.Fatal("accepted a string-encoded durable instant")
	}
}

func TestTypedNestedInstantIsProjectedBeforeSerialization(t *testing.T) {
	type nested struct {
		ScheduledFor time.Time      `json:"scheduled_for"`
		Metadata     map[string]any `json:"metadata"`
	}
	instant := time.Date(2026, 9, 25, 12, 0, 0, 123_000_000, time.FixedZone("local", 8*3600))
	raw, err := Marshal(nested{ScheduledFor: instant, Metadata: map[string]any{"scheduled_for": "external-label"}})
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		ScheduledFor int64          `json:"scheduled_for"`
		Metadata     map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &stored); err != nil || stored.ScheduledFor != instant.UnixMilli() || stored.Metadata["scheduled_for"] != "external-label" {
		t.Fatalf("stored=%+v err=%v payload=%s", stored, err, raw)
	}
	var restored nested
	if err := Unmarshal(raw, &restored); err != nil || !restored.ScheduledFor.Equal(instant) || restored.Metadata["scheduled_for"] != "external-label" {
		t.Fatalf("restored=%+v err=%v", restored, err)
	}
}
