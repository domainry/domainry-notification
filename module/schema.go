package module

import (
	"github.com/domainry/domainry-foundation/schemaownership"
	"github.com/domainry/domainry-notification-sdk/modulehost"
	sqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence"
)

func SchemaOwnership() []schemaownership.Table { return sqlstore.SchemaOwnership() }

func OwnedTables() []string { return schemaownership.Names(SchemaOwnership()) }

func SchemaMigrations(driver, schema, tablePrefix string) ([]modulehost.SchemaMigration, error) {
	source, err := sqlstore.SchemaMigrations(sqlstore.Driver(driver), schema, tablePrefix)
	if err != nil {
		return nil, err
	}
	result := make([]modulehost.SchemaMigration, len(source))
	for index, migration := range source {
		result[index] = modulehost.SchemaMigration{Version: migration.Version, Name: migration.Name, Statements: append([]string(nil), migration.Statements...)}
	}
	return result, nil
}
