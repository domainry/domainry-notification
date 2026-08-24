package sqlstore

var ownedTables = [...]string{
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

// OwnedTables returns the durable-state ownership contract derived from Plane's
// current notification schema. Change it only as part of an explicit schema
// ownership migration. The returned slice may be modified by the caller.
func OwnedTables() []string {
	return append([]string(nil), ownedTables[:]...)
}
