package sqlstore

import migrationstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/sqlstore/migration"

const (
	PortableFormatV1       = migrationstore.PortableFormatV1
	MigrationStateActive   = migrationstore.MigrationStateActive
	MigrationStateFrozen   = migrationstore.MigrationStateFrozen
	MigrationStateImported = migrationstore.MigrationStateImported
	MigrationStateCutover  = migrationstore.MigrationStateCutover
	MigrationRoleSource    = migrationstore.MigrationRoleSource
	MigrationRoleTarget    = migrationstore.MigrationRoleTarget
)

type MigrationControl = migrationstore.MigrationControl
type PortableScope = migrationstore.PortableScope
type PortableBundle = migrationstore.PortableBundle
type PortableTable = migrationstore.PortableTable
type PortableInventory = migrationstore.PortableInventory
type PortableImportReceipt = migrationstore.PortableImportReceipt

var ExportPortable = migrationstore.ExportPortable
var ImportPortable = migrationstore.ImportPortable
var ValidatePortable = migrationstore.ValidatePortable
