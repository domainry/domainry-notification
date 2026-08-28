package template

import (
	"context"

	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

// RecipientDirectory resolves the minimum recipient projection needed while
// rendering a notification. It is defined here because template rendering is
// its consumer; the host adapter enforces membership and visibility.
type RecipientDirectory interface {
	FindRecipient(context.Context, notification.WorkspaceID, notification.UserID) (notification.Recipient, bool, error)
}
