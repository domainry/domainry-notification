package persistence

import portabilitystore "github.com/domainry/domainry-notification/internal/infrastructure/persistence/repository/portability"

const (
	PortableFormatV1       = portabilitystore.PortableFormatV1
	MigrationStateActive   = portabilitystore.MigrationStateActive
	MigrationStateFrozen   = portabilitystore.MigrationStateFrozen
	MigrationStateImported = portabilitystore.MigrationStateImported
	MigrationStateCutover  = portabilitystore.MigrationStateCutover
	MigrationRoleSource    = portabilitystore.MigrationRoleSource
	MigrationRoleTarget    = portabilitystore.MigrationRoleTarget
)

type MigrationControl = portabilitystore.MigrationControl
type PortableScope = portabilitystore.PortableScope
type PortableBundle = portabilitystore.PortableBundle
type PortableTable = portabilitystore.PortableTable
type PortableInventory = portabilitystore.PortableInventory
type PortableImportReceipt = portabilitystore.PortableImportReceipt

var ExportPortable = portabilitystore.ExportPortable
var ImportPortable = portabilitystore.ImportPortable
var ValidatePortable = portabilitystore.ValidatePortable
