package inbox_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/domainry/domainry-notification/inbox"
)

func TestEventWireFormatRemainsCompatibleWithPlane(t *testing.T) {
	const legacy = `{
		"id":"event-1","workspace_id":"workspace-1","source":"workflow",
		"source_event_id":"task-1:opened","event_type":"workflow.task.opened",
		"category":"approval","severity":"info","surface":"business_workspace",
		"recipient_user_ids":["user-1"],"action_state":"open","occurred_at":"2026-08-24T00:00:00Z",
		"snapshot":{"title":"Approval required","body":"Review task"},
		"status":"queued","attempt_count":0,"fencing_token":0,
		"created_at":"2026-08-24T00:00:00Z","updated_at":"2026-08-24T00:00:00Z"
	}`
	var event inbox.Event
	if err := json.Unmarshal([]byte(legacy), &event); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var before, after map[string]any
	if err := json.Unmarshal([]byte(legacy), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("wire format changed\nbefore=%v\nafter=%v", before, after)
	}
}
