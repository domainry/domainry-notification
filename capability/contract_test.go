package capability

import (
	"testing"
)

func TestPublicationRequestOpenAPIResponseUsesPublicationContract(t *testing.T) {
	schema := notificationResponseSchema("post", "/notification/templates/{templateKey}/publication-requests")
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("publication response schema properties = %#v", schema["properties"])
	}
	if _, ok := properties["snapshot"]; !ok {
		t.Fatalf("publication response schema must expose snapshot: %#v", properties)
	}
	if _, ok := properties["draft"]; ok {
		t.Fatalf("publication response schema must not use template-record draft: %#v", properties)
	}
}
