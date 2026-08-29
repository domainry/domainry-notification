package module

import (
	"github.com/domainry/domainry-notification-sdk/modulehost"
	sqlstore "github.com/domainry/domainry-notification/internal/infrastructure/persistence"
)

type DataScope string

const (
	SystemData    DataScope = "system"
	WorkspaceData DataScope = "workspace"
)

type TableOwnership struct {
	Name  string
	Scope DataScope
}

func SchemaOwnership() []TableOwnership {
	source := sqlstore.SchemaOwnership()
	result := make([]TableOwnership, len(source))
	for index, table := range source {
		result[index] = TableOwnership{Name: table.Name, Scope: DataScope(table.Scope)}
	}
	return result
}

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
