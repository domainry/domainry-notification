package validation

import inboxmodel "github.com/domainry/domainry-notification/internal/domain/inbox/model"

type ActionState = inboxmodel.ActionState
type AlertState = inboxmodel.AlertState
type Scope = inboxmodel.Scope
type Mailbox = inboxmodel.Mailbox
type Event = inboxmodel.Event
type Query = inboxmodel.Query
type SavedView = inboxmodel.SavedView
type ActionRef = inboxmodel.ActionRef
type EventType = inboxmodel.EventType
type Rule = inboxmodel.Rule
type Content = inboxmodel.Content

const (
	ActionNone            = inboxmodel.ActionNone
	ActionOpen            = inboxmodel.ActionOpen
	ActionCompleted       = inboxmodel.ActionCompleted
	ActionExpired         = inboxmodel.ActionExpired
	ActionCancelled       = inboxmodel.ActionCancelled
	AlertFiring           = inboxmodel.AlertFiring
	AlertResolved         = inboxmodel.AlertResolved
	EventQueued           = inboxmodel.EventQueued
	ScopeMine             = inboxmodel.ScopeMine
	ScopeTeam             = inboxmodel.ScopeTeam
	ScopeDelegated        = inboxmodel.ScopeDelegated
	MailboxInbox          = inboxmodel.MailboxInbox
	MailboxUnread         = inboxmodel.MailboxUnread
	MailboxActionRequired = inboxmodel.MailboxActionRequired
	MailboxArchived       = inboxmodel.MailboxArchived
)
