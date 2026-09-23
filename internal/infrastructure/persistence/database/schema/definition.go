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

func primary(name string, kind schemaColumnKind) schemaColumn {
	return schemaColumn{name: name, kind: kind, primaryKey: true}
}

var baseSchemaTables = []schemaTable{
	{name: "_notification_user_settings", columns: []schemaColumn{
		required("workspace_id", identifierColumn), required("recipient_user_id", indexedTextColumn), required("setting_kind", indexedTextColumn),
		required("setting_key", indexedTextColumn), required("payload_json", documentColumn), required("updated_by", identifierColumn),
		required("created_at", indexedTextColumn), required("updated_at", indexedTextColumn),
	}},
	{name: "_notification_deliveries", columns: []schemaColumn{
		required("id", identifierColumn), required("workspace_id", identifierColumn), required("row_kind", indexedTextColumn),
		defaulted("event_id", indexedTextColumn, ""), defaulted("recipient_key", indexedTextColumn, ""), defaulted("template_key", indexedTextColumn, ""),
		required("channel", indexedTextColumn), defaulted("dedupe_key", indexedTextColumn, ""), required("status", indexedTextColumn),
		required("payload_json", documentColumn), defaulted("attempt_count", integerColumn, 0), defaulted("next_attempt_at", indexedTextColumn, ""),
		defaulted("last_error_code", indexedTextColumn, ""), defaulted("outbox_message_id", indexedTextColumn, ""),
		defaulted("lease_owner", indexedTextColumn, ""), defaulted("lease_expires_at", indexedTextColumn, ""), defaulted("fencing_token", bigIntegerColumn, 0),
		required("created_at", indexedTextColumn), required("updated_at", indexedTextColumn),
	}},
	{name: "_notification_events", columns: []schemaColumn{
		required("id", identifierColumn), required("workspace_id", identifierColumn), required("source", indexedTextColumn), required("source_event_id", indexedTextColumn),
		required("status", indexedTextColumn), required("payload_json", documentColumn), defaulted("attempt_count", integerColumn, 0),
		defaulted("next_attempt_at", indexedTextColumn, ""), defaulted("last_error_code", indexedTextColumn, ""), defaulted("lease_owner", indexedTextColumn, ""),
		defaulted("lease_expires_at", indexedTextColumn, ""), defaulted("fencing_token", bigIntegerColumn, 0), required("occurred_at", indexedTextColumn),
		required("created_at", indexedTextColumn), required("updated_at", indexedTextColumn),
	}},
	{name: "_notification_delivery_attempts", columns: []schemaColumn{
		required("id", identifierColumn), required("workspace_id", identifierColumn), required("attempt_kind", indexedTextColumn), required("event_id", indexedTextColumn),
		defaulted("delivery_id", indexedTextColumn, ""), defaulted("event_type", indexedTextColumn, ""), defaulted("source", indexedTextColumn, ""),
		defaulted("source_event_id", indexedTextColumn, ""), defaulted("channel", indexedTextColumn, ""), required("stage", indexedTextColumn), required("error_code", indexedTextColumn),
		required("attempt", integerColumn), required("disposition", indexedTextColumn), defaulted("retryable", integerColumn, 0),
		defaulted("next_attempt_at", indexedTextColumn, ""), required("fencing_token", bigIntegerColumn), required("occurred_at", indexedTextColumn),
	}},
	{name: "_notification_inbox_items", columns: []schemaColumn{
		required("id", identifierColumn), required("workspace_id", identifierColumn), required("recipient_user_id", indexedTextColumn),
		required("event_id", indexedTextColumn), required("event_type", indexedTextColumn), required("source", indexedTextColumn), required("category", indexedTextColumn),
		required("severity", indexedTextColumn), required("title", documentColumn), required("body", documentColumn), required("search_text", documentColumn),
		required("payload_json", documentColumn), defaulted("subject_type", indexedTextColumn, ""), defaulted("subject_id", indexedTextColumn, ""),
		required("action_state", indexedTextColumn), defaulted("alert_state", indexedTextColumn, ""), defaulted("group_key", indexedTextColumn, ""),
		defaulted("occurrence_count", integerColumn, 1), required("first_occurred_at", indexedTextColumn), required("last_occurred_at", indexedTextColumn),
		defaulted("read_at", indexedTextColumn, ""), defaulted("archived_at", indexedTextColumn, ""), defaulted("expires_at", indexedTextColumn, ""),
		required("created_at", indexedTextColumn), required("updated_at", indexedTextColumn),
	}},
	{name: "_notification_inbox_delegations", columns: []schemaColumn{
		required("id", identifierColumn), required("workspace_id", identifierColumn), required("owner_user_id", indexedTextColumn), required("delegate_user_id", indexedTextColumn),
		defaulted("starts_at", indexedTextColumn, ""), defaulted("ends_at", indexedTextColumn, ""),
		defaulted("enabled", booleanColumn, true), required("created_at", indexedTextColumn), required("updated_at", indexedTextColumn),
	}},
	{name: "_notification_alert_groups", columns: []schemaColumn{
		required("workspace_id", identifierColumn), required("recipient_user_id", indexedTextColumn), required("group_key", indexedTextColumn),
		required("state", indexedTextColumn), defaulted("occurrence_count", integerColumn, 1), required("first_occurred_at", indexedTextColumn),
		required("last_occurred_at", indexedTextColumn), defaulted("acknowledged_at", indexedTextColumn, ""), defaulted("acknowledged_by", indexedTextColumn, ""),
		defaulted("resolved_at", indexedTextColumn, ""), required("last_event_id", indexedTextColumn), required("updated_at", indexedTextColumn),
	}},
}

var baseSchemaIndexes = []schemaIndex{
	{name: "uniq_notification_user_setting", table: "_notification_user_settings", unique: true, columns: []string{"workspace_id", "recipient_user_id", "setting_kind", "setting_key"}},
	{name: "idx_notification_user_setting_list", table: "_notification_user_settings", columns: []string{"workspace_id", "setting_kind", "recipient_user_id", "setting_key"}},
	{name: "uniq_notification_delivery_workspace_identity", table: "_notification_deliveries", unique: true, columns: []string{"workspace_id", "id"}},
	{name: "idx_notification_delivery_frequency", table: "_notification_deliveries", columns: []string{"workspace_id", "row_kind", "recipient_key", "channel", "created_at"}},
	{name: "idx_notification_delivery_dedupe", table: "_notification_deliveries", columns: []string{"workspace_id", "row_kind", "recipient_key", "template_key", "channel", "dedupe_key"}},
	{name: "idx_notification_delivery_due", table: "_notification_deliveries", columns: []string{"row_kind", "status", "next_attempt_at", "lease_expires_at", "created_at"}},
	{name: "uniq_notification_event_workspace_identity", table: "_notification_events", unique: true, columns: []string{"workspace_id", "id"}},
	{name: "uniq_notification_event_source_identity", table: "_notification_events", unique: true, columns: []string{"workspace_id", "source", "source_event_id"}},
	{name: "idx_notification_event_due", table: "_notification_events", columns: []string{"status", "next_attempt_at", "lease_expires_at"}},
	{name: "uniq_notification_delivery_attempt_identity", table: "_notification_delivery_attempts", unique: true, columns: []string{"workspace_id", "id"}},
	{name: "uniq_notification_delivery_attempt_fence", table: "_notification_delivery_attempts", unique: true, columns: []string{"workspace_id", "attempt_kind", "event_id", "delivery_id", "fencing_token"}},
	{name: "idx_notification_delivery_attempt_governance", table: "_notification_delivery_attempts", columns: []string{"workspace_id", "occurred_at", "attempt_kind", "stage", "error_code"}},
	{name: "uniq_notification_inbox_workspace_identity", table: "_notification_inbox_items", unique: true, columns: []string{"workspace_id", "id"}},
	{name: "idx_notification_inbox_mailbox", table: "_notification_inbox_items", columns: []string{"workspace_id", "recipient_user_id", "archived_at", "updated_at", "id"}},
	{name: "idx_notification_inbox_unread", table: "_notification_inbox_items", columns: []string{"workspace_id", "recipient_user_id", "read_at", "archived_at"}},
	{name: "idx_notification_inbox_facets", table: "_notification_inbox_items", columns: []string{"workspace_id", "recipient_user_id", "category", "source", "severity", "archived_at"}},
	{name: "idx_notification_inbox_group", table: "_notification_inbox_items", columns: []string{"workspace_id", "recipient_user_id", "group_key", "alert_state"}},
	{name: "uniq_notification_inbox_delegation_identity", table: "_notification_inbox_delegations", unique: true, columns: []string{"workspace_id", "id"}},
	{name: "idx_notification_inbox_delegation_delegate", table: "_notification_inbox_delegations", columns: []string{"workspace_id", "delegate_user_id", "enabled", "starts_at", "ends_at"}},
	{name: "uniq_notification_alert_group", table: "_notification_alert_groups", unique: true, columns: []string{"workspace_id", "recipient_user_id", "group_key"}},
	{name: "idx_notification_alert_group_state", table: "_notification_alert_groups", columns: []string{"workspace_id", "state", "updated_at"}},
}

// sharedOperationTables is installed only by standalone SaaS composition.
// Embedded Module mode receives the same canonical table through its host.
var sharedOperationTables = []schemaTable{
	{name: "_operations", columns: []schemaColumn{
		primary("id", identifierColumn), required("workspace_id", indexedTextColumn), defaulted("system_purpose", indexedTextColumn, ""),
		required("owner", indexedTextColumn), required("kind", indexedTextColumn), required("action_key", identifierColumn), defaulted("parent_id", identifierColumn, ""),
		required("resource_type", identifierColumn), defaulted("resource_id", identifierColumn, ""), required("idempotency_key", indexedTextColumn),
		required("request_fingerprint", identifierColumn), required("requested_by", identifierColumn), required("reason", documentColumn), defaulted("reference", identifierColumn, ""),
		required("status", indexedTextColumn), required("status_url", documentColumn), required("result_json", documentColumn), required("metadata_json", documentColumn),
		defaulted("error_code", identifierColumn, ""), defaulted("failure_class", identifierColumn, ""), defaulted("next_action", documentColumn, ""),
		required("related_ids_json", documentColumn), defaulted("correlation", identifierColumn, ""), required("evidence_json", documentColumn),
		defaulted("lease_owner", identifierColumn, ""), defaulted("lease_expires_at", indexedTextColumn, ""), defaulted("fencing_token", bigIntegerColumn, 0),
		defaulted("expires_at", indexedTextColumn, ""), required("created_at", indexedTextColumn), defaulted("started_at", identifierColumn, ""),
		defaulted("finished_at", identifierColumn, ""), required("updated_at", indexedTextColumn),
	}},
	{name: "_operation_controls", columns: []schemaColumn{
		required("system_purpose", identifierColumn), required("control_kind", identifierColumn), required("owner", identifierColumn),
		required("state", indexedTextColumn), required("reason", plainTextColumn), defaulted("reference", plainTextColumn, ""),
		required("updated_by", plainTextColumn), required("revision", bigIntegerColumn), required("updated_at", plainTextColumn),
	}},
}

var sharedOperationIndexes = []schemaIndex{
	{name: "uniq_runtime_operation_key", table: "_operations", unique: true, columns: []string{"workspace_id", "system_purpose", "owner", "kind", "idempotency_key"}},
	{name: "idx_runtime_operation_status", table: "_operations", columns: []string{"workspace_id", "owner", "status", "created_at"}},
	{name: "idx_runtime_operation_parent", table: "_operations", columns: []string{"workspace_id", "parent_id", "created_at"}},
	{name: "idx_runtime_operation_lease", table: "_operations", columns: []string{"owner", "status", "lease_expires_at"}},
	{name: "uniq_runtime_operation_control", table: "_operation_controls", unique: true, columns: []string{"system_purpose", "control_kind", "owner"}},
	{name: "idx_runtime_operation_control_state", table: "_operation_controls", columns: []string{"system_purpose", "control_kind", "state"}},
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
