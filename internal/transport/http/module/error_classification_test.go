package module

import (
	"net/http/httptest"
	"testing"

	notificationsdk "github.com/domainry/domainry-notification-sdk"
)

type classificationCapture struct {
	*httptest.ResponseRecorder
	kind, code string
}

func (capture *classificationCapture) RecordError(kind, code string) {
	capture.kind, capture.code = kind, code
}

func TestNotificationErrorClassificationMatrix(t *testing.T) {
	tests := []struct {
		status int
		kind   string
		code   string
	}{
		{503, "unavailable", "notification.database_unavailable"},
		{502, "unavailable", "notification.provider_unavailable"},
		{409, "conflict", "notification.publication_fence_rejected"},
		{400, "bad_request", "notification.validation_failed"},
		{403, "forbidden", "notification.permission_denied"},
	}
	for _, test := range tests {
		capture := &classificationCapture{ResponseRecorder: httptest.NewRecorder()}
		writeError(capture, &notificationsdk.Error{StatusCode: test.status, Code: test.code})
		wantCode := "backend." + test.code
		if capture.kind != test.kind || capture.code != wantCode {
			t.Fatalf("status=%d kind=%q code=%q", test.status, capture.kind, capture.code)
		}
	}
}
