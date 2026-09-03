package schema

// DataScope describes the tenancy boundary of a notification-owned table. It
// is schema metadata, not an authorization mechanism: callers still provide
// explicit system or workspace authority at capability boundaries.
type DataScope string

const (
	SystemData    DataScope = "system"
	WorkspaceData DataScope = "workspace"
)

// TableOwnership is the durable schema ownership contract shared with hosts,
// migration tooling, and architecture gates.
type TableOwnership struct {
	Name  string
	Scope DataScope
}

var tableOwnership = [...]TableOwnership{
	{Name: "_notification_alert_groups", Scope: WorkspaceData},
	{Name: "_notification_channel_plans", Scope: WorkspaceData},
	{Name: "_notification_delivery_policies", Scope: WorkspaceData},
	{Name: "_notification_delivery_reservations", Scope: WorkspaceData},
	{Name: "_notification_event_failures", Scope: WorkspaceData},
	{Name: "_notification_events", Scope: WorkspaceData},
	{Name: "_notification_inbox_delegations", Scope: WorkspaceData},
	{Name: "_notification_inbox_items", Scope: WorkspaceData},
	{Name: "_notification_inbox_saved_views", Scope: WorkspaceData},
	{Name: "_notification_migration_controls", Scope: WorkspaceData},
	{Name: "_notification_recipient_preferences", Scope: WorkspaceData},
	{Name: "_notification_retention_archive_entries", Scope: WorkspaceData},
	{Name: "_notification_template_publication_locks", Scope: WorkspaceData},
	{Name: "_notification_template_publication_requests", Scope: WorkspaceData},
	{Name: "_notification_template_versions", Scope: WorkspaceData},
	{Name: "_notification_templates", Scope: WorkspaceData},
}

// SchemaOwnership returns a defensive copy of the module's durable table
// ownership, including each table's tenancy boundary.
func SchemaOwnership() []TableOwnership {
	return append([]TableOwnership(nil), tableOwnership[:]...)
}

// OwnedTables is retained as a convenient flat inventory for host retirement
// checks. Schema and RLS tooling should prefer SchemaOwnership.
func OwnedTables() []string {
	result := make([]string, len(tableOwnership))
	for index, table := range tableOwnership {
		result[index] = table.Name
	}
	return result
}
