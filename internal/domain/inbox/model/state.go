package model

type EventStatus string

const (
	EventQueued       EventStatus = "queued"
	EventProcessing   EventStatus = "processing"
	EventMaterialized EventStatus = "materialized"
	EventFailed       EventStatus = "failed"
)

type Mailbox string

const (
	MailboxInbox          Mailbox = "inbox"
	MailboxUnread         Mailbox = "unread"
	MailboxActionRequired Mailbox = "action_required"
	MailboxArchived       Mailbox = "archived"
)

type ActionState string

const (
	ActionNone      ActionState = "none"
	ActionOpen      ActionState = "open"
	ActionCompleted ActionState = "completed"
	ActionExpired   ActionState = "expired"
	ActionCancelled ActionState = "cancelled"
)

type Scope string

const (
	ScopeMine      Scope = "mine"
	ScopeTeam      Scope = "team"
	ScopeDelegated Scope = "delegated"
)

type AlertState string

const (
	AlertFiring       AlertState = "firing"
	AlertAcknowledged AlertState = "acknowledged"
	AlertResolved     AlertState = "resolved"
)
