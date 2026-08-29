package migration

import (
	"context"
	"fmt"

	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/base"
)

type Profile struct{}

func (Profile) Acquire(ctx context.Context, connection base.MigrationConnection, namespace string) (base.MigrationLockRelease, error) {
	key := base.MigrationLockKey(namespace)
	var ignored any
	if err := connection.QueryRowContext(ctx, "SELECT pg_advisory_lock(hashtextextended($1, 0))", key).Scan(&ignored); err != nil {
		return nil, fmt.Errorf("acquire notification PostgreSQL migration lock: %w", err)
	}
	return func(releaseContext context.Context) error {
		var released bool
		if err := connection.QueryRowContext(releaseContext, "SELECT pg_advisory_unlock(hashtextextended($1, 0))", key).Scan(&released); err != nil {
			return err
		}
		if !released {
			return fmt.Errorf("notification PostgreSQL migration lock was not held")
		}
		return nil
	}, nil
}
