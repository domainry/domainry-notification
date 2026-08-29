package portabilitystore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/domainry/domainry-notification-sdk/modulehost"
	storeschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/schema"
	"github.com/domainry/domainry-orm/builder"
	"github.com/domainry/domainry-orm/sqlhost"
)

const PortableFormatV1 = "domainry-notification-portable-v1"

type PortableScope struct {
	TenantID       string `json:"tenant_id"`
	WorkspaceID    string `json:"workspace_id"`
	ApplicationKey string `json:"application_key"`
}

type PortableBundle struct {
	FormatVersion string          `json:"format_version"`
	MigrationID   string          `json:"migration_id,omitempty"`
	Source        PortableScope   `json:"source"`
	Tables        []PortableTable `json:"tables"`
	Fingerprint   string          `json:"fingerprint"`
}

type PortableTable struct {
	Name    string              `json:"name"`
	Columns []string            `json:"columns"`
	Rows    [][]json.RawMessage `json:"rows"`
}

type PortableInventory struct {
	Tables       map[string]int `json:"tables"`
	Rows         int            `json:"rows"`
	ActiveLeases int            `json:"active_leases"`
	Fingerprint  string         `json:"fingerprint"`
}

type PortableImportReceipt struct {
	FormatVersion  string `json:"format_version"`
	Fingerprint    string `json:"fingerprint"`
	Rows           int    `json:"rows"`
	AlreadyPresent bool   `json:"already_present"`
}

func (s *Store) ExportPortable(ctx context.Context, scope PortableScope) (PortableBundle, PortableInventory, error) {
	if s == nil {
		return PortableBundle{}, PortableInventory{}, fmt.Errorf("notification portable store is unavailable")
	}
	return ExportPortable(ctx, s.Database, s.Renderer, scope)
}

func (s *Store) ExportPortableMigration(ctx context.Context, scope PortableScope, migrationID string) (PortableBundle, PortableInventory, error) {
	bundle, inventory, err := s.ExportPortable(ctx, scope)
	if err != nil {
		return bundle, inventory, err
	}
	bundle.MigrationID = strings.TrimSpace(migrationID)
	if bundle.MigrationID == "" {
		return PortableBundle{}, PortableInventory{}, fmt.Errorf("notification portable migration id is required")
	}
	bundle.Fingerprint, err = portableFingerprint(bundle)
	inventory.Fingerprint = bundle.Fingerprint
	return bundle, inventory, err
}

func (s *Store) ImportPortable(ctx context.Context, scope PortableScope, bundle PortableBundle) (PortableImportReceipt, error) {
	if s == nil {
		return PortableImportReceipt{}, fmt.Errorf("notification portable store is unavailable")
	}
	return ImportPortable(ctx, s.Database, s.Renderer, scope, bundle)
}

func ExportPortable(ctx context.Context, database sqlhost.Queryer, dialect modulehost.Dialect, scope PortableScope) (PortableBundle, PortableInventory, error) {
	if database == nil || dialect == nil || strings.TrimSpace(scope.TenantID) == "" || strings.TrimSpace(scope.WorkspaceID) == "" || strings.TrimSpace(scope.ApplicationKey) == "" {
		return PortableBundle{}, PortableInventory{}, fmt.Errorf("notification portable export dependencies and scope are required")
	}
	ownership := ownershipByTable()
	definitions := storeschema.PortableTables()
	bundle := PortableBundle{FormatVersion: PortableFormatV1, Source: scope, Tables: make([]PortableTable, 0, len(definitions))}
	inventory := PortableInventory{Tables: map[string]int{}}
	for _, definition := range definitions {
		columns := make([]string, len(definition.Columns))
		for index, column := range definition.Columns {
			columns[index] = column.Name
		}
		selectBuilder := builder.NewSelectBuilder(dialect, definition.Name).Columns(columns...)
		if ownership[definition.Name] == storeschema.WorkspaceData {
			selectBuilder = builder.NewWorkspaceSelectBuilder(dialect, definition.Name, scope.WorkspaceID).Columns(columns...)
		}
		statement, args, err := selectBuilder.Build()
		if err != nil {
			return PortableBundle{}, PortableInventory{}, err
		}
		rows, err := database.QueryContext(ctx, statement, args...)
		if err != nil {
			return PortableBundle{}, PortableInventory{}, fmt.Errorf("export notification table %s: %w", definition.Name, err)
		}
		table := PortableTable{Name: definition.Name, Columns: columns, Rows: [][]json.RawMessage{}}
		for rows.Next() {
			values := make([]any, len(columns))
			destinations := make([]any, len(columns))
			for index := range values {
				destinations[index] = &values[index]
			}
			if err := rows.Scan(destinations...); err != nil {
				_ = rows.Close()
				return PortableBundle{}, PortableInventory{}, err
			}
			encoded := make([]json.RawMessage, len(values))
			for index, value := range values {
				if bytes, ok := value.([]byte); ok {
					value = string(bytes)
				}
				raw, marshalErr := json.Marshal(value)
				if marshalErr != nil {
					_ = rows.Close()
					return PortableBundle{}, PortableInventory{}, marshalErr
				}
				encoded[index] = raw
			}
			table.Rows = append(table.Rows, encoded)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return PortableBundle{}, PortableInventory{}, err
		}
		_ = rows.Close()
		sort.Slice(table.Rows, func(i, j int) bool { return portableRowKey(table.Rows[i]) < portableRowKey(table.Rows[j]) })
		inventory.Tables[definition.Name] = len(table.Rows)
		inventory.Rows += len(table.Rows)
		inventory.ActiveLeases += activePortableLeases(table)
		bundle.Tables = append(bundle.Tables, table)
	}
	fingerprint, err := portableFingerprint(bundle)
	if err != nil {
		return PortableBundle{}, PortableInventory{}, err
	}
	bundle.Fingerprint, inventory.Fingerprint = fingerprint, fingerprint
	return bundle, inventory, nil
}

func ImportPortable(ctx context.Context, database sqlhost.Database, dialect modulehost.Dialect, target PortableScope, bundle PortableBundle) (PortableImportReceipt, error) {
	if database == nil || dialect == nil {
		return PortableImportReceipt{}, fmt.Errorf("notification portable import dependencies are required")
	}
	if err := ValidatePortable(bundle, target); err != nil {
		return PortableImportReceipt{}, err
	}
	existing, inventory, err := ExportPortable(ctx, database, dialect, target)
	if err != nil {
		return PortableImportReceipt{}, err
	}
	if inventory.Rows > 0 {
		existing.MigrationID = bundle.MigrationID
		existing.Fingerprint, err = portableFingerprint(existing)
		if err != nil {
			return PortableImportReceipt{}, err
		}
		if existing.Fingerprint != bundle.Fingerprint {
			return PortableImportReceipt{}, fmt.Errorf("notification portable import target is not empty and does not match the bundle")
		}
		return PortableImportReceipt{FormatVersion: PortableFormatV1, Fingerprint: bundle.Fingerprint, Rows: inventory.Rows, AlreadyPresent: true}, nil
	}
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return PortableImportReceipt{}, err
	}
	defer tx.Rollback()
	rowCount := 0
	for _, table := range bundle.Tables {
		definition, found := tableDefinition(table.Name)
		if !found {
			return PortableImportReceipt{}, fmt.Errorf("notification portable table %s is not owned by this module", table.Name)
		}
		for _, row := range table.Rows {
			values := make([]any, len(row))
			for index, raw := range row {
				value, decodeErr := decodePortableCell(raw, definition.Columns[index].Kind)
				if decodeErr != nil {
					return PortableImportReceipt{}, fmt.Errorf("decode notification portable cell: %w", decodeErr)
				}
				values[index] = value
			}
			columns := append([]string(nil), table.Columns...)
			insertBuilder := builder.NewInsertBuilder(dialect, table.Name)
			if ownershipByTable()[table.Name] == storeschema.WorkspaceData {
				workspaceIndex := slices.Index(columns, builder.WorkspaceIDColumn)
				if workspaceIndex < 0 || strings.TrimSpace(fmt.Sprint(values[workspaceIndex])) != target.WorkspaceID {
					return PortableImportReceipt{}, fmt.Errorf("notification portable table %s contains an invalid workspace row", table.Name)
				}
				columns = slices.Delete(columns, workspaceIndex, workspaceIndex+1)
				values = slices.Delete(values, workspaceIndex, workspaceIndex+1)
				insertBuilder = builder.NewWorkspaceInsertBuilder(dialect, table.Name, target.WorkspaceID)
			}
			statement, args, buildErr := insertBuilder.Columns(columns...).Values(values...).Build()
			if buildErr != nil {
				return PortableImportReceipt{}, buildErr
			}
			if _, err := tx.ExecContext(ctx, statement, args...); err != nil {
				return PortableImportReceipt{}, fmt.Errorf("import notification table %s: %w", table.Name, err)
			}
			rowCount++
		}
	}
	if err := tx.Commit(); err != nil {
		return PortableImportReceipt{}, err
	}
	imported, importedInventory, err := ExportPortable(ctx, database, dialect, target)
	if err != nil {
		return PortableImportReceipt{}, err
	}
	imported.MigrationID = bundle.MigrationID
	imported.Fingerprint, err = portableFingerprint(imported)
	if err != nil {
		return PortableImportReceipt{}, err
	}
	if imported.Fingerprint != bundle.Fingerprint || importedInventory.Rows != rowCount {
		return PortableImportReceipt{}, fmt.Errorf("notification portable import reconciliation failed")
	}
	return PortableImportReceipt{FormatVersion: PortableFormatV1, Fingerprint: bundle.Fingerprint, Rows: rowCount}, nil
}

func ValidatePortable(bundle PortableBundle, target PortableScope) error {
	if bundle.FormatVersion != PortableFormatV1 || strings.TrimSpace(bundle.Source.TenantID) == "" || strings.TrimSpace(bundle.Source.WorkspaceID) == "" || strings.TrimSpace(bundle.Source.ApplicationKey) == "" {
		return fmt.Errorf("notification portable bundle format or source scope is invalid")
	}
	if bundle.Source.TenantID != target.TenantID || bundle.Source.WorkspaceID != target.WorkspaceID || bundle.Source.ApplicationKey != target.ApplicationKey {
		return fmt.Errorf("notification portable bundle target scope mismatch")
	}
	definitions := storeschema.PortableTables()
	if len(bundle.Tables) != len(definitions) {
		return fmt.Errorf("notification portable bundle table inventory is incomplete")
	}
	for index, definition := range definitions {
		table := bundle.Tables[index]
		if table.Name != definition.Name || len(table.Columns) != len(definition.Columns) {
			return fmt.Errorf("notification portable table %d schema mismatch", index)
		}
		for columnIndex, column := range definition.Columns {
			if table.Columns[columnIndex] != column.Name {
				return fmt.Errorf("notification portable table %s columns mismatch", table.Name)
			}
		}
		for _, row := range table.Rows {
			if len(row) != len(table.Columns) {
				return fmt.Errorf("notification portable table %s row width mismatch", table.Name)
			}
			if ownershipByTable()[table.Name] == storeschema.WorkspaceData {
				workspaceIndex := slices.Index(table.Columns, "workspace_id")
				var workspaceID string
				if workspaceIndex < 0 || json.Unmarshal(row[workspaceIndex], &workspaceID) != nil || workspaceID != target.WorkspaceID {
					return fmt.Errorf("notification portable table %s contains a foreign workspace row", table.Name)
				}
			}
		}
	}
	fingerprint, err := portableFingerprint(PortableBundle{FormatVersion: bundle.FormatVersion, MigrationID: bundle.MigrationID, Source: bundle.Source, Tables: bundle.Tables})
	if err != nil || fingerprint != bundle.Fingerprint {
		return fmt.Errorf("notification portable bundle fingerprint mismatch")
	}
	return nil
}

func tableDefinition(name string) (storeschema.PortableTableDefinition, bool) {
	for _, table := range storeschema.PortableTables() {
		if table.Name == name {
			return table, true
		}
	}
	return storeschema.PortableTableDefinition{}, false
}

func decodePortableCell(raw json.RawMessage, kind storeschema.PortableColumnKind) (any, error) {
	if string(raw) == "null" {
		return nil, nil
	}
	switch kind {
	case storeschema.PortableInteger:
		var value int64
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		return value, nil
	case storeschema.PortableBoolean:
		var value bool
		if err := json.Unmarshal(raw, &value); err == nil {
			return value, nil
		}
		var numeric int64
		if err := json.Unmarshal(raw, &numeric); err != nil {
			return nil, err
		}
		return numeric != 0, nil
	default:
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		return value, nil
	}
}

func ownershipByTable() map[string]storeschema.DataScope {
	ownership := storeschema.SchemaOwnership()
	result := make(map[string]storeschema.DataScope, len(ownership))
	for _, table := range ownership {
		result[table.Name] = table.Scope
	}
	return result
}

func portableFingerprint(bundle PortableBundle) (string, error) {
	bundle.Fingerprint = ""
	encoded, err := json.Marshal(bundle)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func portableRowKey(row []json.RawMessage) string {
	encoded, _ := json.Marshal(row)
	return string(encoded)
}

func activePortableLeases(table PortableTable) int {
	leaseIndex := -1
	for index, column := range table.Columns {
		if column == "lease_owner" {
			leaseIndex = index
			break
		}
	}
	if leaseIndex < 0 {
		return 0
	}
	active := 0
	for _, row := range table.Rows {
		var owner string
		if leaseIndex < len(row) && json.Unmarshal(row[leaseIndex], &owner) == nil && strings.TrimSpace(owner) != "" {
			active++
		}
	}
	return active
}
