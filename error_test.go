package notification_test

import (
	"errors"
	"testing"

	"github.com/domainry/domainry-notification"
)

func TestModuleErrorPreservesSemanticsAndCause(t *testing.T) {
	cause := errors.New("database unavailable")
	err := notification.NewError(notification.ErrorUnavailable, "backend.notification.inbox_unavailable", cause, map[string]any{"queue": "inbox"})
	if notification.ErrorKindOf(err) != notification.ErrorUnavailable || notification.ErrorCode(err) != "backend.notification.inbox_unavailable" {
		t.Fatalf("kind=%q code=%q", notification.ErrorKindOf(err), notification.ErrorCode(err))
	}
	if !errors.Is(err, cause) {
		t.Fatal("module error did not preserve its cause")
	}
	var value *notification.Error
	if !errors.As(err, &value) || value.Params["queue"] != "inbox" {
		t.Fatalf("error=%+v", value)
	}
}
