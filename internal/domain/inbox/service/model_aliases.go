package inbox

import inboxmodel "github.com/domainry/domainry-notification/internal/domain/inbox/model"
import inboxrepository "github.com/domainry/domainry-notification/internal/domain/inbox/repository"
import inboxvalidation "github.com/domainry/domainry-notification/internal/domain/inbox/validation"

type EventStatus = inboxmodel.EventStatus
type Mailbox = inboxmodel.Mailbox
type ActionState = inboxmodel.ActionState
type Scope = inboxmodel.Scope
type AlertState = inboxmodel.AlertState
type Aggregate = inboxmodel.Aggregate
type FailureAggregate = inboxmodel.FailureAggregate
type FailureMetrics = inboxmodel.FailureMetrics
type GovernanceMetrics = inboxmodel.GovernanceMetrics
type Intent = inboxmodel.Intent
type Snapshot = inboxmodel.Snapshot
type Event = inboxmodel.Event
type EventFailure = inboxmodel.EventFailure
type Item = inboxmodel.Item
type Query = inboxmodel.Query
type Page = inboxmodel.Page
type Facet = inboxmodel.Facet
type Facets = inboxmodel.Facets
type Delegation = inboxmodel.Delegation
type SavedView = inboxmodel.SavedView
type AlertGroup = inboxmodel.AlertGroup
type EventType = inboxmodel.EventType
type Content = inboxmodel.Content
type Rule = inboxmodel.Rule
type RuleChannel = inboxmodel.RuleChannel
type GovernanceCatalog = inboxmodel.GovernanceCatalog
type ActionRef = inboxmodel.ActionRef
type ResolvedAction = inboxmodel.ResolvedAction
type ActionDescriptor = inboxmodel.ActionDescriptor
type EventStore = inboxrepository.EventStore
type MailboxStore = inboxrepository.MailboxStore
type SavedViewStore = inboxrepository.SavedViewStore
type DelegationStore = inboxrepository.DelegationStore
type MetricsStore = inboxrepository.MetricsStore
type Configuration = inboxvalidation.Configuration
type Validator = inboxvalidation.Validator

var NewConfiguration = inboxvalidation.NewConfiguration
var NewValidator = inboxvalidation.NewValidator
var uniqueStrings = inboxvalidation.UniqueStrings
var uniqueUsers = inboxvalidation.UniqueUsers

const recipientLimit = inboxvalidation.RecipientLimit

const (
	EventQueued           = inboxmodel.EventQueued
	EventProcessing       = inboxmodel.EventProcessing
	EventMaterialized     = inboxmodel.EventMaterialized
	EventFailed           = inboxmodel.EventFailed
	MailboxInbox          = inboxmodel.MailboxInbox
	MailboxUnread         = inboxmodel.MailboxUnread
	MailboxActionRequired = inboxmodel.MailboxActionRequired
	MailboxArchived       = inboxmodel.MailboxArchived
	ActionNone            = inboxmodel.ActionNone
	ActionOpen            = inboxmodel.ActionOpen
	ActionCompleted       = inboxmodel.ActionCompleted
	ActionExpired         = inboxmodel.ActionExpired
	ActionCancelled       = inboxmodel.ActionCancelled
	ScopeMine             = inboxmodel.ScopeMine
	ScopeTeam             = inboxmodel.ScopeTeam
	ScopeDelegated        = inboxmodel.ScopeDelegated
	AlertFiring           = inboxmodel.AlertFiring
	AlertAcknowledged     = inboxmodel.AlertAcknowledged
	AlertResolved         = inboxmodel.AlertResolved
)
