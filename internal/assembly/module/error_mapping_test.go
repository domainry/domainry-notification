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
		class     notificationsdk.ErrorClass
		retryable bool
	}{
		{name: "invalid", kind: notification.ErrorInvalid, class: notificationsdk.ErrorClassBadRequest},
		{name: "not found", kind: notification.ErrorNotFound, class: notificationsdk.ErrorClassNotFound},
		{name: "conflict", kind: notification.ErrorConflict, class: notificationsdk.ErrorClassConflict},
		{name: "forbidden", kind: notification.ErrorForbidden, class: notificationsdk.ErrorClassForbidden},
		{name: "unavailable", kind: notification.ErrorUnavailable, class: notificationsdk.ErrorClassUnavailable, retryable: true},
		{name: "internal", kind: notification.ErrorInternal, class: notificationsdk.ErrorClassInternal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mapped := moduleError(notification.NewError(test.kind, "backend.notification.test", errors.New("cause"), nil))
			var sdkError *notificationsdk.Error
			if !errors.As(mapped, &sdkError) {
				t.Fatalf("mapped error type = %T", mapped)
			}
			if sdkError.Class != test.class || sdkError.Code != "backend.notification.test" || sdkError.Retryable != test.retryable {
				t.Fatalf("mapped error = %#v", sdkError)
			}
		})
	}
}
