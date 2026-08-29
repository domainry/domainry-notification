package base

import ormmigration "github.com/domainry/domainry-orm/migration"

type MigrationConnection = ormmigration.Connection
type MigrationLockRelease = ormmigration.Release
type MigrationLocker = ormmigration.Locker

func MigrationLockKey(namespace string) string {
	return ormmigration.NamespacedLockKey("notification-migration", namespace)
}
