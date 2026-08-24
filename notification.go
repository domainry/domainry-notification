// Package notification defines the independently composable Domainry
// notification module boundary.
package notification

const ModuleVersion = "v0.1.0-dev.1"

// OwnedTables is the initial durable-state ownership contract derived from the
// current Plane notification schema. Keep this list sorted and change it only
// with an explicit schema ownership migration.
var OwnedTables = [...]string{
	"notification_alert_groups",
	"notification_channel_plans",
	"notification_delivery_policy",
	"notification_delivery_reservations",
	"notification_event_failures",
	"notification_events",
	"notification_inbox_delegations",
	"notification_inbox_items",
	"notification_inbox_saved_views",
	"notification_recipient_preferences",
	"notification_template_publication_locks",
	"notification_template_publication_requests",
	"notification_template_records",
	"notification_template_versions",
}
