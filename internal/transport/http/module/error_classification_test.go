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
		class notificationsdk.ErrorClass
		kind  string
		code  string
	}{
		{notificationsdk.ErrorClassUnavailable, "unavailable", "notification.database_unavailable"},
		{notificationsdk.ErrorClassUnavailable, "unavailable", "notification.provider_unavailable"},
		{notificationsdk.ErrorClassConflict, "conflict", "notification.publication_fence_rejected"},
		{notificationsdk.ErrorClassBadRequest, "bad_request", "notification.validation_failed"},
		{notificationsdk.ErrorClassForbidden, "forbidden", "notification.permission_denied"},
	}
	for _, test := range tests {
		capture := &classificationCapture{ResponseRecorder: httptest.NewRecorder()}
		writeError(capture, &notificationsdk.Error{Class: test.class, Code: test.code})
		wantCode := "backend." + test.code
		if capture.kind != test.kind || capture.code != wantCode {
			t.Fatalf("class=%q kind=%q code=%q", test.class, capture.kind, capture.code)
		}
	}
}
