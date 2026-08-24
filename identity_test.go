package notification_test

import (
	"errors"
	"testing"

	"github.com/domainry/domainry-notification"
)

func TestModuleOwnedIdentityValues(t *testing.T) {
	workspace, err := notification.NewWorkspaceID(" workspace-1 ")
	if err != nil || workspace.String() != "workspace-1" {
		t.Fatalf("workspace=%q err=%v", workspace, err)
	}
	user, err := notification.NewUserID(" user-1 ")
	if err != nil || user.String() != "user-1" {
		t.Fatalf("user=%q err=%v", user, err)
	}
	if _, err := notification.NewWorkspaceID(" "); !errors.Is(err, notification.ErrWorkspaceIDRequired) {
		t.Fatalf("workspace error=%v", err)
	}
	if _, err := notification.NewUserID(""); !errors.Is(err, notification.ErrUserIDRequired) {
		t.Fatalf("user error=%v", err)
	}
}
