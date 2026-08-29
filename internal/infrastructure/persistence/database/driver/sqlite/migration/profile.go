package migration

import (
	"context"

	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/base"
)

type Profile struct{}

func (Profile) Acquire(context.Context, base.MigrationConnection, string) (base.MigrationLockRelease, error) {
	return func(context.Context) error { return nil }, nil
}
