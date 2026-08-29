package sqlite

import (
	"context"

	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"
)

type MigrationLocker struct{}

func (MigrationLocker) Acquire(context.Context, base.MigrationConnection, string) (base.MigrationLockRelease, error) {
	return func(context.Context) error { return nil }, nil
}
