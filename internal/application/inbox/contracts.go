package inbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	inboxmodel "github.com/domainry/domainry-notification/internal/domain/inbox/model"
	inboxrepository "github.com/domainry/domainry-notification/internal/domain/inbox/repository"
	inboxvalidation "github.com/domainry/domainry-notification/internal/domain/inbox/validation"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

type Event = inboxmodel.Event
type Item = inboxmodel.Item
type Snapshot = inboxmodel.Snapshot
type ActionRef = inboxmodel.ActionRef
type EventStore = inboxrepository.EventStore

type WorkNotifier interface {
	Notify(context.Context, notification.Work)
}

const recipientLimit = inboxvalidation.RecipientLimit

func uniqueUsers(values []notification.UserID) []notification.UserID {
	return inboxvalidation.UniqueUsers(values)
}

func unavailable(code string, cause error) error {
	return notification.NewError(notification.ErrorUnavailable, code, cause, nil)
}

func normalizeLocale(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(value, "_", "-"))
}

func stableID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}
