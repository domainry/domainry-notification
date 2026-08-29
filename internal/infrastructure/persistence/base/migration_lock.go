package base

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strings"
)

// MigrationConnection is the session-bound SQL surface required by database
// advisory locks. Acquire and release must use the same connection.
type MigrationConnection interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type MigrationLockRelease func(context.Context) error

type MigrationLocker interface {
	Acquire(context.Context, MigrationConnection, string) (MigrationLockRelease, error)
}

func MigrationLockKey(namespace string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(namespace)))
	return "notification-migration-" + hex.EncodeToString(sum[:16])
}
