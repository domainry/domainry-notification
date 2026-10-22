package schema

func required(name string, kind schemaColumnKind) schemaColumn {
	return schemaColumn{name: name, kind: kind}
}

func optional(name string, kind schemaColumnKind) schemaColumn {
	return schemaColumn{name: name, kind: kind, nullable: true}
}

func defaulted(name string, kind schemaColumnKind, value any) schemaColumn {
	return schemaColumn{name: name, kind: kind, defaultValue: value, defaultSet: true}
}

func physicalID() schemaColumn {
	return schemaColumn{name: "id", kind: bigIntegerColumn, primaryKey: true, autoIncrement: true}
}

var baseSchemaTables = []schemaTable{
	{name: "_notification_user_settings", columns: []schemaColumn{
		physicalID(), required("workspace_id", identifierColumn), required("recipient_user_id", indexedTextColumn), required("setting_kind", indexedTextColumn),
		required("setting_key", indexedTextColumn), required("payload_json", documentColumn), required("updated_by", identifierColumn),
		required("created_at", bigIntegerColumn), required("updated_at", bigIntegerColumn),
	}},
	{name: "_notification_deliveries", columns: []schemaColumn{
		physicalID(), required("delivery_id", identifierColumn), required("workspace_id", identifierColumn), required("event_id", indexedTextColumn),
		required("channel", indexedTextColumn), required("status", indexedTextColumn),
		required("payload_json", documentColumn), defaulted("attempt_count", integerColumn, 0), defaulted("next_attempt_at", bigIntegerColumn, 0),
		defaulted("last_error_code", indexedTextColumn, ""), defaulted("outbox_message_id", indexedTextColumn, ""),
		defaulted("lease_owner", indexedTextColumn, ""), defaulted("lease_expires_at", bigIntegerColumn, 0), defaulted("fencing_token", bigIntegerColumn, 0),
		required("created_at", bigIntegerColumn), required("updated_at", bigIntegerColumn),
	}},
	{name: "_notification_delivery_reservations", columns: []schemaColumn{
		physicalID(), required("workspace_id", identifierColumn), required("reservation_key", identifierColumn), required("recipient_key", indexedTextColumn),
		required("template_key", indexedTextColumn), required("channel", indexedTextColumn), defaulted("dedupe_key", indexedTextColumn, ""),
		required("created_at", bigIntegerColumn),
	}},
	{name: "_notification_events", columns: []schemaColumn{
		physicalID(), required("event_id", identifierColumn), required("workspace_id", identifierColumn), required("source", indexedTextColumn), required("source_event_id", indexedTextColumn),
		required("status", indexedTextColumn), required("payload_json", documentColumn), defaulted("attempt_count", integerColumn, 0),
		defaulted("next_attempt_at", bigIntegerColumn, 0), defaulted("last_error_code", indexedTextColumn, ""), defaulted("lease_owner", indexedTextColumn, ""),
		defaulted("lease_expires_at", bigIntegerColumn, 0), defaulted("fencing_token", bigIntegerColumn, 0), required("occurred_at", bigIntegerColumn),
		required("created_at", bigIntegerColumn), required("updated_at", bigIntegerColumn),
	}},
	{name: "_notification_delivery_attempts", columns: []schemaColumn{
		physicalID(), required("workspace_id", identifierColumn), required("attempt_kind", indexedTextColumn), required("event_id", indexedTextColumn),
		required("delivery_id", indexedTextColumn), required("fencing_token", bigIntegerColumn), defaulted("event_type", indexedTextColumn, ""), defaulted("source", indexedTextColumn, ""),
		defaulted("source_event_id", indexedTextColumn, ""), defaulted("channel", indexedTextColumn, ""), required("stage", indexedTextColumn), required("error_code", indexedTextColumn),
		required("attempt", integerColumn), required("disposition", indexedTextColumn), defaulted("retryable", integerColumn, 0),
		defaulted("next_attempt_at", bigIntegerColumn, 0), required("occurred_at", bigIntegerColumn),
	}},
	{name: "_notification_inbox_items", columns: []schemaColumn{
		physicalID(), required("item_id", identifierColumn), required("item_key", identifierColumn), required("workspace_id", identifierColumn), required("recipient_user_id", indexedTextColumn),
		required("event_id", indexedTextColumn), required("event_type", indexedTextColumn), required("source", indexedTextColumn), required("category", indexedTextColumn),
		required("severity", indexedTextColumn), required("title", documentColumn), required("body", documentColumn), required("search_text", documentColumn),
		required("payload_json", documentColumn), defaulted("subject_type", indexedTextColumn, ""), defaulted("subject_id", indexedTextColumn, ""),
		required("action_state", indexedTextColumn), defaulted("alert_state", indexedTextColumn, ""), defaulted("group_key", indexedTextColumn, ""),
		defaulted("occurrence_count", integerColumn, 1), required("first_occurred_at", bigIntegerColumn), required("last_occurred_at", bigIntegerColumn),
		defaulted("read_at", bigIntegerColumn, 0), defaulted("archived_at", bigIntegerColumn, 0), defaulted("expires_at", bigIntegerColumn, 0),
		required("created_at", bigIntegerColumn), required("updated_at", bigIntegerColumn),
	}},
	{name: "_notification_inbox_delegations", columns: []schemaColumn{
		physicalID(), required("delegation_id", identifierColumn), required("workspace_id", identifierColumn), required("owner_user_id", indexedTextColumn), required("delegate_user_id", indexedTextColumn),
		defaulted("starts_at", bigIntegerColumn, 0), defaulted("ends_at", bigIntegerColumn, 0),
		defaulted("enabled", booleanColumn, true), required("created_at", bigIntegerColumn), required("updated_at", bigIntegerColumn),
	}},
	{name: "_notification_alert_groups", columns: []schemaColumn{
		physicalID(), required("workspace_id", identifierColumn), required("recipient_user_id", indexedTextColumn), required("group_key", indexedTextColumn),
		required("state", indexedTextColumn), defaulted("occurrence_count", integerColumn, 1), required("first_occurred_at", bigIntegerColumn),
		required("last_occurred_at", bigIntegerColumn), defaulted("acknowledged_at", bigIntegerColumn, 0), defaulted("acknowledged_by", indexedTextColumn, ""),
		defaulted("resolved_at", bigIntegerColumn, 0), required("last_event_id", indexedTextColumn), required("updated_at", bigIntegerColumn),
	}},
}

var baseSchemaIndexes = []schemaIndex{
	{name: "uniq_notification_user_setting", table: "_notification_user_settings", unique: true, columns: []string{"workspace_id", "recipient_user_id", "setting_kind", "setting_key"}},
	{name: "idx_notification_user_setting_list", table: "_notification_user_settings", columns: []string{"workspace_id", "setting_kind", "recipient_user_id", "setting_key"}},
	{name: "uniq_notification_delivery_id", table: "_notification_deliveries", unique: true, columns: []string{"delivery_id"}},
	{name: "idx_notification_delivery_frequency", table: "_notification_delivery_reservations", columns: []string{"workspace_id", "recipient_key", "channel", "created_at"}},
	{name: "idx_notification_delivery_dedupe", table: "_notification_delivery_reservations", columns: []string{"workspace_id", "recipient_key", "template_key", "channel", "dedupe_key", "created_at"}},
	{name: "uniq_notification_delivery_reservation", table: "_notification_delivery_reservations", unique: true, columns: []string{"workspace_id", "reservation_key"}},
	{name: "idx_notification_delivery_due", table: "_notification_deliveries", columns: []string{"status", "next_attempt_at", "lease_expires_at", "id"}},
	{name: "uniq_notification_event_id", table: "_notification_events", unique: true, columns: []string{"event_id"}},
	{name: "uniq_notification_event_source_identity", table: "_notification_events", unique: true, columns: []string{"workspace_id", "source", "source_event_id"}},
	{name: "idx_notification_event_due", table: "_notification_events", columns: []string{"status", "next_attempt_at", "lease_expires_at", "id"}},
	{name: "idx_notification_delivery_attempt_governance", table: "_notification_delivery_attempts", columns: []string{"workspace_id", "occurred_at", "attempt_kind", "stage", "error_code"}},
	{name: "uniq_notification_delivery_attempt", table: "_notification_delivery_attempts", unique: true, columns: []string{"workspace_id", "attempt_kind", "event_id", "delivery_id", "fencing_token"}},
	{name: "idx_notification_inbox_mailbox", table: "_notification_inbox_items", columns: []string{"workspace_id", "recipient_user_id", "archived_at", "updated_at", "id"}},
	{name: "uniq_notification_inbox_item_id", table: "_notification_inbox_items", unique: true, columns: []string{"item_id"}},
	{name: "uniq_notification_inbox_item_key", table: "_notification_inbox_items", unique: true, columns: []string{"workspace_id", "item_key"}},
	{name: "idx_notification_inbox_unread", table: "_notification_inbox_items", columns: []string{"workspace_id", "recipient_user_id", "read_at", "archived_at"}},
	{name: "idx_notification_inbox_facets", table: "_notification_inbox_items", columns: []string{"workspace_id", "recipient_user_id", "category", "source", "severity", "archived_at"}},
	{name: "idx_notification_inbox_group", table: "_notification_inbox_items", columns: []string{"workspace_id", "recipient_user_id", "group_key", "alert_state"}},
	{name: "idx_notification_inbox_delegation_delegate", table: "_notification_inbox_delegations", columns: []string{"workspace_id", "delegate_user_id", "enabled", "starts_at", "ends_at"}},
	{name: "uniq_notification_inbox_delegation_id", table: "_notification_inbox_delegations", unique: true, columns: []string{"delegation_id"}},
	{name: "idx_notification_alert_group_state", table: "_notification_alert_groups", columns: []string{"workspace_id", "state", "updated_at"}},
	{name: "uniq_notification_alert_group", table: "_notification_alert_groups", unique: true, columns: []string{"workspace_id", "recipient_user_id", "group_key"}},
}

func ownedSchemaTables() []schemaTable {
	tables := make([]schemaTable, 0, len(baseSchemaTables))
	tables = append(tables, baseSchemaTables...)
	return tables
}

func portableSchemaTables() []schemaTable {
	tables := make([]schemaTable, 0, len(baseSchemaTables))
	for _, source := range baseSchemaTables {
		table := schemaTable{name: source.name, columns: append([]schemaColumn(nil), source.columns...)}
		tables = append(tables, table)
	}
	return tables
}

type PortableColumnKind uint8

const (
	PortableText PortableColumnKind = iota
	PortableInteger
	PortableBoolean
)

type PortableTableDefinition struct {
	Name    string
	Columns []PortableColumnDefinition
}

type PortableColumnDefinition struct {
	Name string
	Kind PortableColumnKind
}

func PortableTables() []PortableTableDefinition {
	source := portableSchemaTables()
	result := make([]PortableTableDefinition, len(source))
	for tableIndex, table := range source {
		result[tableIndex].Name = table.name
		result[tableIndex].Columns = make([]PortableColumnDefinition, len(table.columns))
		for columnIndex, column := range table.columns {
			kind := PortableText
			switch column.kind {
			case integerColumn, bigIntegerColumn:
				kind = PortableInteger
			case booleanColumn:
				kind = PortableBoolean
			}
			result[tableIndex].Columns[columnIndex] = PortableColumnDefinition{Name: column.name, Kind: kind}
		}
	}
	return result
}
