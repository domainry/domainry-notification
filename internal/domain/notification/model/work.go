package model

// Work identifies durable notification work. A wakeup is only an acceleration
// hint; persistent state remains the source of truth for recovery.
type Work struct {
	Kind        WorkKind
	WorkspaceID WorkspaceID
	TaskID      string
}

type WorkKind string

const (
	WorkInboxEvent  WorkKind = "notification_inbox"
	WorkChannelPlan WorkKind = "notification_channel"
	WorkPublication WorkKind = "notification_publication"
)
