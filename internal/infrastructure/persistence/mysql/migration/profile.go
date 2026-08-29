package migration

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/domainry/domainry-notification/internal/infrastructure/persistence/base"
)

type Profile struct{}

func (Profile) Acquire(ctx context.Context, connection base.MigrationConnection, namespace string) (base.MigrationLockRelease, error) {
	key := base.MigrationLockKey(namespace)
	var acquired sql.NullInt64
	if err := connection.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", key, 30).Scan(&acquired); err != nil {
		return nil, fmt.Errorf("acquire notification MySQL migration lock: %w", err)
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		return nil, fmt.Errorf("acquire notification MySQL migration lock timed out")
	}
	return func(releaseContext context.Context) error {
		var released sql.NullInt64
		if err := connection.QueryRowContext(releaseContext, "SELECT RELEASE_LOCK(?)", key).Scan(&released); err != nil {
			return err
		}
		if !released.Valid || released.Int64 != 1 {
			return fmt.Errorf("notification MySQL migration lock was not held")
		}
		return nil
	}, nil
}
