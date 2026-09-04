package module

import (
	"errors"
	"testing"

	notificationsdk "github.com/domainry/domainry-notification-sdk"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

func TestModuleErrorPreservesDomainFailureSemantics(t *testing.T) {
	tests := []struct {
		name      string
		kind      notification.ErrorKind
		status    int
		retryable bool
	}{
		{name: "invalid", kind: notification.ErrorInvalid, status: 400},
		{name: "not found", kind: notification.ErrorNotFound, status: 404},
		{name: "conflict", kind: notification.ErrorConflict, status: 409},
		{name: "forbidden", kind: notification.ErrorForbidden, status: 403},
		{name: "unavailable", kind: notification.ErrorUnavailable, status: 503, retryable: true},
		{name: "internal", kind: notification.ErrorInternal, status: 500},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mapped := moduleError(notification.NewError(test.kind, "backend.notification.test", errors.New("cause"), nil))
			var sdkError *notificationsdk.Error
			if !errors.As(mapped, &sdkError) {
				t.Fatalf("mapped error type = %T", mapped)
			}
			if sdkError.StatusCode != test.status || sdkError.Code != "backend.notification.test" || sdkError.Retryable != test.retryable {
				t.Fatalf("mapped error = %#v", sdkError)
			}
		})
	}
}
