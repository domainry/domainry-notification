package sqlstore

func required(name string, kind schemaColumnKind) schemaColumn {
	return schemaColumn{name: name, kind: kind}
}

func optional(name string, kind schemaColumnKind) schemaColumn {
	return schemaColumn{name: name, kind: kind, nullable: true}
}

func defaulted(name string, kind schemaColumnKind, value string) schemaColumn {
	return schemaColumn{name: name, kind: kind, defaultSQL: value}
}

func primary(name string, kind schemaColumnKind) schemaColumn {
	return schemaColumn{name: name, kind: kind, primaryKey: true}
}

var baseSchemaTables = []schemaTable{
	{name: "notification_template_records", columns: []schemaColumn{
		primary("template_key", identifierColumn), optional("draft_json", documentColumn), optional("published_json", documentColumn),
		defaulted("published_version", integerColumn, "0"), defaulted("status", identifierColumn, "'active'"), required("updated_by", identifierColumn),
		required("created_at", identifierColumn), required("updated_at", identifierColumn),
	}},
	{name: "notification_template_versions", columns: []schemaColumn{
		required("id", identifierColumn), required("template_key", identifierColumn), required("version", integerColumn), required("payload_json", documentColumn),
		required("content_hash", identifierColumn), required("published_by", identifierColumn), required("published_at", identifierColumn),
	}},
	{name: "notification_template_publication_requests", columns: []schemaColumn{
		primary("id", identifierColumn), required("template_key", identifierColumn), required("snapshot_json", documentColumn), required("candidate_hash", identifierColumn),
		required("draft_updated_at", identifierColumn), required("status", identifierColumn), defaulted("scheduled_for", identifierColumn, "''"),
		required("requested_by", identifierColumn), required("requested_at", identifierColumn), defaulted("reviewed_by", identifierColumn, "''"),
		defaulted("reviewed_at", identifierColumn, "''"), defaulted("published_version", integerColumn, "0"), required("failure", plainTextColumn),
		defaulted("lease_owner", identifierColumn, "''"), defaulted("lease_expires_at", identifierColumn, "''"), defaulted("fencing_token", bigIntegerColumn, "0"),
		required("updated_at", identifierColumn),
	}},
	{name: "notification_template_publication_locks", columns: []schemaColumn{
		primary("template_key", identifierColumn), required("request_id", identifierColumn), required("created_at", identifierColumn),
	}},
	{name: "notification_delivery_policy", columns: []schemaColumn{
		primary("policy_key", identifierColumn), required("payload_json", documentColumn), required("updated_by", identifierColumn), required("updated_at", identifierColumn),
	}},
	{name: "notification_recipient_preferences", columns: []schemaColumn{
		required("workspace_id", identifierColumn), required("recipient_key", identifierColumn), required("payload_json", documentColumn),
		required("updated_by", identifierColumn), required("updated_at", identifierColumn),
	}},
	{name: "notification_delivery_reservations", columns: []schemaColumn{
		required("id", identifierColumn), required("workspace_id", identifierColumn), required("recipient_key", indexedTextColumn),
		required("template_key", indexedTextColumn), required("channel", indexedTextColumn), defaulted("dedupe_key", indexedTextColumn, "''"), required("created_at", indexedTextColumn),
	}},
	{name: "notification_events", columns: []schemaColumn{
		required("id", identifierColumn), required("workspace_id", identifierColumn), required("source", indexedTextColumn), required("source_event_id", indexedTextColumn),
		required("status", indexedTextColumn), required("payload_json", documentColumn), defaulted("attempt_count", integerColumn, "0"),
		defaulted("next_attempt_at", indexedTextColumn, "''"), defaulted("last_error_code", indexedTextColumn, "''"), defaulted("lease_owner", indexedTextColumn, "''"),
		defaulted("lease_expires_at", indexedTextColumn, "''"), defaulted("fencing_token", bigIntegerColumn, "0"), required("occurred_at", indexedTextColumn),
		required("created_at", indexedTextColumn), required("updated_at", indexedTextColumn),
	}},
	{name: "notification_event_failures", columns: []schemaColumn{
		required("id", identifierColumn), required("workspace_id", identifierColumn), required("event_id", indexedTextColumn), required("event_type", indexedTextColumn),
		required("source", indexedTextColumn), required("source_event_id", indexedTextColumn), required("stage", indexedTextColumn), required("error_code", indexedTextColumn),
		required("attempt", integerColumn), required("disposition", indexedTextColumn), defaulted("retryable", integerColumn, "0"),
		defaulted("next_attempt_at", indexedTextColumn, "''"), required("fencing_token", bigIntegerColumn), required("occurred_at", indexedTextColumn),
	}},
	{name: "notification_channel_plans", columns: []schemaColumn{
		required("id", identifierColumn), required("workspace_id", identifierColumn), required("event_id", indexedTextColumn), required("channel", indexedTextColumn),
		required("status", indexedTextColumn), required("payload_json", documentColumn), defaulted("attempt_count", integerColumn, "0"),
		defaulted("next_attempt_at", indexedTextColumn, "''"), defaulted("last_error_code", indexedTextColumn, "''"), defaulted("outbox_message_id", indexedTextColumn, "''"),
		defaulted("lease_owner", indexedTextColumn, "''"), defaulted("lease_expires_at", indexedTextColumn, "''"), defaulted("fencing_token", bigIntegerColumn, "0"),
		required("created_at", indexedTextColumn), required("updated_at", indexedTextColumn),
	}},
	{name: "notification_inbox_items", columns: []schemaColumn{
		required("id", identifierColumn), required("workspace_id", identifierColumn), required("recipient_user_id", indexedTextColumn), required("surface", indexedTextColumn),
		required("event_id", indexedTextColumn), required("event_type", indexedTextColumn), required("source", indexedTextColumn), required("category", indexedTextColumn),
		required("severity", indexedTextColumn), required("title", documentColumn), required("body", documentColumn), required("search_text", documentColumn),
		required("payload_json", documentColumn), defaulted("subject_type", indexedTextColumn, "''"), defaulted("subject_id", indexedTextColumn, "''"),
		required("action_state", indexedTextColumn), defaulted("alert_state", indexedTextColumn, "''"), defaulted("group_key", indexedTextColumn, "''"),
		defaulted("occurrence_count", integerColumn, "1"), required("first_occurred_at", indexedTextColumn), required("last_occurred_at", indexedTextColumn),
		defaulted("read_at", indexedTextColumn, "''"), defaulted("archived_at", indexedTextColumn, "''"), defaulted("expires_at", indexedTextColumn, "''"),
		required("created_at", indexedTextColumn), required("updated_at", indexedTextColumn),
	}},
	{name: "notification_inbox_delegations", columns: []schemaColumn{
		required("id", identifierColumn), required("workspace_id", identifierColumn), required("owner_user_id", indexedTextColumn), required("delegate_user_id", indexedTextColumn),
		required("surface", indexedTextColumn), defaulted("starts_at", indexedTextColumn, "''"), defaulted("ends_at", indexedTextColumn, "''"),
		defaulted("enabled", booleanColumn, "TRUE"), required("created_at", indexedTextColumn), required("updated_at", indexedTextColumn),
	}},
	{name: "notification_alert_groups", columns: []schemaColumn{
		required("workspace_id", identifierColumn), required("recipient_user_id", indexedTextColumn), required("surface", indexedTextColumn), required("group_key", indexedTextColumn),
		required("state", indexedTextColumn), defaulted("occurrence_count", integerColumn, "1"), required("first_occurred_at", indexedTextColumn),
		required("last_occurred_at", indexedTextColumn), defaulted("acknowledged_at", indexedTextColumn, "''"), defaulted("acknowledged_by", indexedTextColumn, "''"),
		defaulted("resolved_at", indexedTextColumn, "''"), required("last_event_id", indexedTextColumn), required("updated_at", indexedTextColumn),
	}},
	{name: "notification_inbox_saved_views", columns: []schemaColumn{
		required("workspace_id", identifierColumn), required("recipient_user_id", indexedTextColumn), required("surface", indexedTextColumn),
		required("view_key", indexedTextColumn), required("payload_json", documentColumn), required("created_at", indexedTextColumn), required("updated_at", indexedTextColumn),
	}},
}

var baseSchemaIndexes = []schemaIndex{
	{name: "uniq_notification_template_version", table: "notification_template_versions", unique: true, columns: []string{"template_key", "version"}},
	{name: "idx_notification_publication_template", table: "notification_template_publication_requests", columns: []string{"template_key", "requested_at"}},
	{name: "idx_notification_publication_due", table: "notification_template_publication_requests", columns: []string{"status", "scheduled_for"}},
	{name: "uniq_notification_recipient_preference", table: "notification_recipient_preferences", unique: true, columns: []string{"workspace_id", "recipient_key"}},
	{name: "idx_notification_delivery_frequency", table: "notification_delivery_reservations", columns: []string{"workspace_id", "recipient_key", "channel", "created_at"}},
	{name: "uniq_notification_delivery_reservation_workspace_identity", table: "notification_delivery_reservations", unique: true, columns: []string{"workspace_id", "id"}},
	{name: "idx_notification_delivery_dedupe", table: "notification_delivery_reservations", columns: []string{"workspace_id", "recipient_key", "template_key", "channel", "dedupe_key"}},
	{name: "uniq_notification_event_workspace_identity", table: "notification_events", unique: true, columns: []string{"workspace_id", "id"}},
	{name: "uniq_notification_event_source_identity", table: "notification_events", unique: true, columns: []string{"workspace_id", "source", "source_event_id"}},
	{name: "idx_notification_event_due", table: "notification_events", columns: []string{"status", "next_attempt_at", "lease_expires_at"}},
	{name: "uniq_notification_event_failure_identity", table: "notification_event_failures", unique: true, columns: []string{"workspace_id", "id"}},
	{name: "uniq_notification_event_failure_attempt", table: "notification_event_failures", unique: true, columns: []string{"workspace_id", "event_id", "fencing_token"}},
	{name: "idx_notification_event_failure_governance", table: "notification_event_failures", columns: []string{"workspace_id", "occurred_at", "stage", "error_code"}},
	{name: "uniq_notification_channel_plan_identity", table: "notification_channel_plans", unique: true, columns: []string{"workspace_id", "id"}},
	{name: "idx_notification_channel_plan_due", table: "notification_channel_plans", columns: []string{"status", "next_attempt_at", "lease_expires_at", "created_at"}},
	{name: "uniq_notification_inbox_workspace_identity", table: "notification_inbox_items", unique: true, columns: []string{"workspace_id", "id"}},
	{name: "idx_notification_inbox_mailbox", table: "notification_inbox_items", columns: []string{"workspace_id", "recipient_user_id", "surface", "archived_at", "updated_at", "id"}},
	{name: "idx_notification_inbox_unread", table: "notification_inbox_items", columns: []string{"workspace_id", "recipient_user_id", "surface", "read_at", "archived_at"}},
	{name: "idx_notification_inbox_facets", table: "notification_inbox_items", columns: []string{"workspace_id", "recipient_user_id", "surface", "category", "source", "severity", "archived_at"}},
	{name: "idx_notification_inbox_group", table: "notification_inbox_items", columns: []string{"workspace_id", "recipient_user_id", "surface", "group_key", "alert_state"}},
	{name: "uniq_notification_inbox_delegation_identity", table: "notification_inbox_delegations", unique: true, columns: []string{"workspace_id", "id"}},
	{name: "idx_notification_inbox_delegation_delegate", table: "notification_inbox_delegations", columns: []string{"workspace_id", "delegate_user_id", "surface", "enabled", "starts_at", "ends_at"}},
	{name: "uniq_notification_alert_group", table: "notification_alert_groups", unique: true, columns: []string{"workspace_id", "recipient_user_id", "surface", "group_key"}},
	{name: "idx_notification_alert_group_state", table: "notification_alert_groups", columns: []string{"workspace_id", "state", "updated_at"}},
	{name: "uniq_notification_inbox_saved_view", table: "notification_inbox_saved_views", unique: true, columns: []string{"workspace_id", "recipient_user_id", "surface", "view_key"}},
}

var retentionArchiveTables = []schemaTable{
	{name: "notification_retention_archive", columns: []schemaColumn{
		primary("id", identifierColumn), required("workspace_id", identifierColumn), required("policy_key", indexedTextColumn),
		required("policy_version", indexedTextColumn), required("job_id", indexedTextColumn), required("source_table", indexedTextColumn),
		required("resource_id", indexedTextColumn), required("payload_hash", indexedTextColumn), required("payload_json", documentColumn),
		required("archived_at", indexedTextColumn),
	}},
}

var retentionArchiveIndexes = []schemaIndex{
	{name: "uniq_notification_retention_archive", table: "notification_retention_archive", unique: true, columns: []string{"workspace_id", "policy_key", "source_table", "resource_id"}},
	{name: "idx_notification_retention_archive_job", table: "notification_retention_archive", columns: []string{"workspace_id", "job_id", "archived_at"}},
}

var migrationControlTables = []schemaTable{
	{name: "notification_migration_controls", columns: []schemaColumn{
		primary("workspace_id", identifierColumn), required("migration_id", identifierColumn), required("role", indexedTextColumn),
		required("state", indexedTextColumn), defaulted("bundle_fingerprint", identifierColumn, "''"), required("frozen_at", indexedTextColumn),
		defaulted("activated_at", indexedTextColumn, "''"), required("updated_at", indexedTextColumn),
	}},
}

var migrationControlIndexes = []schemaIndex{
	{name: "idx_notification_migration_state", table: "notification_migration_controls", columns: []string{"state", "updated_at"}},
}

func ownedSchemaTables() []schemaTable {
	tables := make([]schemaTable, 0, len(baseSchemaTables)+len(retentionArchiveTables)+len(migrationControlTables))
	tables = append(tables, baseSchemaTables...)
	tables = append(tables, retentionArchiveTables...)
	tables = append(tables, migrationControlTables...)
	return tables
}
