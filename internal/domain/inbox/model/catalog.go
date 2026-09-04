package model

import (
	template "github.com/domainry/domainry-notification/internal/domain/template/model"
)

type EventType struct {
	Key               string              `json:"key"`
	Source            string              `json:"source"`
	Category          string              `json:"category"`
	DefaultSeverity   string              `json:"default_severity"`
	MandatoryInApp    bool                `json:"mandatory_in_app"`
	TemplateKey       string              `json:"template_key"`
	DefaultLocale     string              `json:"default_locale"`
	Locales           map[string]Content  `json:"locales"`
	Variables         []template.Variable `json:"variables,omitempty"`
	Actions           []ActionDescriptor  `json:"actions,omitempty"`
	AudienceResolvers []string            `json:"audience_resolvers,omitempty"`
	Version           int                 `json:"version"`
	Status            string              `json:"status"`
}

type Content struct {
	Title        string            `json:"title"`
	Body         string            `json:"body"`
	Facts        []template.Fact   `json:"facts,omitempty"`
	ActionLabels map[string]string `json:"action_labels,omitempty"`
}

type Rule struct {
	EventTypeKey             string        `json:"event_type_key"`
	Enabled                  bool          `json:"enabled"`
	AudienceResolvers        []string      `json:"audience_resolvers,omitempty"`
	MandatoryInApp           bool          `json:"mandatory_in_app"`
	MinimumSeverity          string        `json:"minimum_severity"`
	DedupeWindowSeconds      int           `json:"dedupe_window_seconds,omitempty"`
	AggregationWindowSeconds int           `json:"aggregation_window_seconds,omitempty"`
	ReminderIntervalSeconds  int           `json:"reminder_interval_seconds,omitempty"`
	MaximumReminders         int           `json:"maximum_reminders,omitempty"`
	RecoveryEventTypeKey     string        `json:"recovery_event_type_key,omitempty"`
	AutoResolveOnRecovery    bool          `json:"auto_resolve_on_recovery,omitempty"`
	UserMutable              bool          `json:"user_mutable,omitempty"`
	Channels                 []RuleChannel `json:"channels,omitempty"`
}

type RuleChannel struct {
	Channel                  string `json:"channel"`
	TemplateKey              string `json:"template_key,omitempty"`
	ConnectorKey             string `json:"connector_key,omitempty"`
	ConnectionKey            string `json:"connection_key,omitempty"`
	Operation                string `json:"operation,omitempty"`
	Mandatory                bool   `json:"mandatory,omitempty"`
	DelaySeconds             int    `json:"delay_seconds,omitempty"`
	EscalationStep           int    `json:"escalation_step,omitempty"`
	CancelWhenActionTerminal bool   `json:"cancel_when_action_terminal,omitempty"`
	DeliveryMode             string `json:"delivery_mode,omitempty"`
	DigestWindowSeconds      int    `json:"digest_window_seconds,omitempty"`
	DigestMaximumItems       int    `json:"digest_maximum_items,omitempty"`
}

type GovernanceCatalog struct {
	EventTypes []EventType `json:"event_types"`
	Rules      []Rule      `json:"rules"`
}
