package template

import (
	"context"

	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

// RecipientResolver resolves the minimum recipient projection needed while
// rendering a notification. It is defined here because template rendering is
// its consumer; the host adapter enforces membership and visibility.
type RecipientResolver interface {
	FindRecipient(context.Context, notification.WorkspaceID, notification.UserID) (notification.Recipient, bool, error)
}

// NativePushRecipientResolver resolves the current delivery token at dispatch
// time so rotated credentials are never frozen into a notification plan.
type NativePushRecipientResolver interface {
	FindNativePushToken(context.Context, notification.WorkspaceID, notification.UserID, string) (string, bool, error)
}
