package schema

import "github.com/domainry/domainry-foundation/schemaownership"

const (
	MigrationOwner                    = "notification"
	NotificationAlertGroupsTable      = "_notification_alert_groups"
	NotificationDeliveriesTable       = "_notification_deliveries"
	NotificationDeliveryAttemptsTable = "_notification_delivery_attempts"
	NotificationEventsTable           = "_notification_events"
	NotificationInboxDelegationsTable = "_notification_inbox_delegations"
	NotificationInboxItemsTable       = "_notification_inbox_items"
	NotificationUserSettingsTable     = "_notification_user_settings"
)

var tableOwnership = []schemaownership.Table{
	{
		Name: NotificationAlertGroupsTable, Owner: MigrationOwner, WorkspaceScope: schemaownership.ScopeWorkspace,
		RetentionClass: schemaownership.RetentionUserErase, PrimaryKey: []string{"workspace_id", "recipient_user_id", "group_key"},
		BoundedQueryPath: "workspace/recipient/group identity or workspace state/updated_at index with bounded mailbox processing",
		DeletionPolicy:   "subject erasure anonymizes recipient and acknowledgement identities; retention archives then purges resolved groups unless held",
	},
	{
		Name: NotificationDeliveriesTable, Owner: MigrationOwner, WorkspaceScope: schemaownership.ScopeWorkspace,
		RetentionClass: schemaownership.RetentionTechnicalTTL, PrimaryKey: []string{"workspace_id", "id"},
		BoundedQueryPath: "workspace delivery identity, recipient/channel frequency and dedupe indexes, plus bounded due lease claims",
		DeletionPolicy:   "subject erasure anonymizes recipients and payloads; retention archives then purges eligible terminal delivery and reservation rows unless held",
	},
	{
		Name: NotificationDeliveryAttemptsTable, Owner: MigrationOwner, WorkspaceScope: schemaownership.ScopeWorkspace,
		RetentionClass: schemaownership.RetentionTechnicalTTL, PrimaryKey: []string{"workspace_id", "id"},
		BoundedQueryPath: "workspace attempt identity, immutable event/delivery fence identity and bounded governance time range",
		DeletionPolicy:   "history retention archives then purges attempts older than the configured cutoff unless a legal hold applies",
	},
	{
		Name: NotificationEventsTable, Owner: MigrationOwner, WorkspaceScope: schemaownership.ScopeWorkspace,
		RetentionClass: schemaownership.RetentionTechnicalTTL, PrimaryKey: []string{"workspace_id", "id"},
		BoundedQueryPath: "workspace event or source identity and bounded status/next-attempt lease claims",
		DeletionPolicy:   "subject erasure redacts recipient payloads; retention archives then purges materialized or failed events unless held",
	},
	{
		Name: NotificationInboxDelegationsTable, Owner: MigrationOwner, WorkspaceScope: schemaownership.ScopeWorkspace,
		RetentionClass: schemaownership.RetentionUserErase, PrimaryKey: []string{"workspace_id", "id"},
		BoundedQueryPath: "workspace delegation identity and workspace/delegate/enabled time-window index",
		DeletionPolicy:   "explicit delegation removal and subject erasure physically delete owner or delegate rows",
	},
	{
		Name: NotificationInboxItemsTable, Owner: MigrationOwner, WorkspaceScope: schemaownership.ScopeWorkspace,
		RetentionClass: schemaownership.RetentionUserErase, PrimaryKey: []string{"workspace_id", "id"},
		BoundedQueryPath: "workspace mailbox cursor by recipient/archived_at/updated_at/id with unread, facet and alert-group indexes",
		DeletionPolicy:   "subject erasure anonymizes recipient and rendered content; retention archives then purges closed history unless held",
	},
	{
		Name: NotificationUserSettingsTable, Owner: MigrationOwner, WorkspaceScope: schemaownership.ScopeWorkspace,
		RetentionClass: schemaownership.RetentionUserErase, PrimaryKey: []string{"workspace_id", "recipient_user_id", "setting_kind", "setting_key"},
		BoundedQueryPath: "exact workspace/recipient/kind/key identity and workspace/kind/recipient/key ordered listing",
		DeletionPolicy:   "explicit preference or saved-view deletion and subject erasure physically delete the user-scoped row",
	},
}

func SchemaOwnership() []schemaownership.Table {
	return schemaownership.Clone(tableOwnership)
}

func OwnedTables() []string {
	return schemaownership.Names(SchemaOwnership())
}
