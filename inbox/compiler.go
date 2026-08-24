package inbox

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/domainry/domainry-notification"
	"github.com/domainry/domainry-notification/delivery"
	"github.com/domainry/domainry-notification/template"
)

// Compiler transforms producer-owned intents into complete durable events. It
// performs no I/O, audience lookup, transaction, or worker wakeup.
type Compiler struct {
	catalog   *Catalog
	validator *Validator
	clock     notification.Clock
}

func NewCompiler(catalog *Catalog, validator *Validator, clock notification.Clock) (*Compiler, error) {
	if catalog == nil || validator == nil || clock == nil {
		return nil, fmt.Errorf("notification inbox compiler dependencies are required")
	}
	return &Compiler{catalog: catalog, validator: validator, clock: clock}, nil
}

// Compile returns a validated event ready for Store.Enqueue or a source-owned
// transaction writer. Generated plan IDs are stable across retries.
func (c *Compiler) Compile(intent Intent) (Event, error) {
	eventType, found := c.catalog.EventType(strings.TrimSpace(intent.EventType))
	if !found {
		return Event{}, invalid("backend.notification.event_type_not_published", "event_type", intent.EventType)
	}
	if !eventTypeAllowsSurface(eventType, intent.Surface) {
		return Event{}, invalid("backend.notification.event_type_surface_invalid", "event_type", intent.EventType)
	}
	resolverKeys, err := c.validateAudienceResolvers(eventType, intent.AudienceResolverKeys)
	if err != nil {
		return Event{}, err
	}
	variables, err := template.ValidateRenderVariables(template.Template{Key: eventType.TemplateKey, Variables: eventType.Variables}, intent.Variables)
	if err != nil {
		return Event{}, err
	}
	locale, _ := eventTypeContent(eventType, intent.Locale)
	if locale == "" {
		return Event{}, invalid("backend.notification.event_type_locale_unavailable", "event_type", intent.EventType)
	}
	localized := make(map[string]Snapshot, len(eventType.Locales))
	for localizedLocale, content := range eventType.Locales {
		snapshot, compileErr := compileSnapshot(eventType, localizedLocale, content, variables, intent.SubjectID)
		if compileErr != nil {
			return Event{}, compileErr
		}
		localized[localizedLocale] = snapshot
	}
	snapshot := localized[locale]
	severity := strings.TrimSpace(intent.Severity)
	if severity == "" {
		severity = eventType.DefaultSeverity
	}
	actionState := ActionNone
	if len(snapshot.Actions) > 0 {
		actionState = ActionOpen
	}
	if intent.ActionState != "" {
		actionState = ActionState(strings.TrimSpace(string(intent.ActionState)))
	}
	event := Event{
		ID: strings.TrimSpace(intent.ID), WorkspaceID: notification.WorkspaceID(strings.TrimSpace(intent.WorkspaceID.String())), Source: eventType.Source,
		SourceEventID: strings.TrimSpace(intent.SourceEventID), EventType: eventType.Key, Category: eventType.Category,
		Severity: severity, Surface: notification.Surface(strings.TrimSpace(string(intent.Surface))), RecipientUserIDs: append([]notification.UserID(nil), intent.RecipientUserIDs...), AudienceResolverKeys: resolverKeys,
		SubjectType: strings.TrimSpace(intent.SubjectType), SubjectID: strings.TrimSpace(intent.SubjectID), SubjectVersion: strings.TrimSpace(intent.SubjectVersion),
		GroupKey: strings.TrimSpace(intent.GroupKey), DedupeKey: strings.TrimSpace(intent.DedupeKey), ActionState: actionState, AlertState: AlertState(strings.TrimSpace(string(intent.AlertState))),
		ExpiresAt: strings.TrimSpace(intent.ExpiresAt), OccurredAt: strings.TrimSpace(intent.OccurredAt), CorrelationID: strings.TrimSpace(intent.CorrelationID), TraceID: strings.TrimSpace(intent.TraceID),
		Snapshot: snapshot, LocalizedSnapshots: localized,
	}
	event.ChannelPlans = c.compilePlans(eventType.Key, intent, snapshot)
	event, err = c.validator.ValidateEvent(event)
	if err != nil {
		return Event{}, err
	}
	if err := c.validateEventContract(event); err != nil {
		return Event{}, err
	}
	now := notification.Timestamp(c.clock.Now())
	event.CreatedAt, event.UpdatedAt = now, now
	for index := range event.ChannelPlans {
		plan := &event.ChannelPlans[index]
		plan.ID = stableID(event.WorkspaceID.String(), event.ID, plan.Channel, plan.ConnectorKey, plan.Operation, fmt.Sprint(plan.EscalationStep))
		plan.WorkspaceID, plan.EventID, plan.Status = event.WorkspaceID, event.ID, "queued"
		plan.CreatedAt, plan.UpdatedAt = now, now
	}
	return event, nil
}

func (c *Compiler) validateAudienceResolvers(eventType EventType, requested []string) ([]string, error) {
	requested = uniqueStrings(requested)
	if len(requested) == 0 {
		return nil, nil
	}
	allowed := map[string]bool{}
	for _, key := range eventType.AudienceResolvers {
		allowed[strings.TrimSpace(key)] = true
	}
	if rule, found := c.catalog.Rule(eventType.Key); found {
		for _, key := range rule.AudienceResolvers {
			allowed[strings.TrimSpace(key)] = true
		}
	}
	for _, key := range requested {
		if !allowed[key] {
			return nil, invalid("backend.notification.inbox_audience_resolver_not_allowed", "resolver_key", key)
		}
	}
	return requested, nil
}

func (c *Compiler) validateEventContract(event Event) error {
	eventType, found := c.catalog.EventType(event.EventType)
	if !found || eventType.Source != event.Source || eventType.Category != event.Category || !eventTypeAllowsSurface(eventType, event.Surface) {
		return invalid("backend.notification.event_type_contract_mismatch", "event_type", event.EventType)
	}
	snapshots := []Snapshot{event.Snapshot}
	for _, snapshot := range event.LocalizedSnapshots {
		snapshots = append(snapshots, snapshot)
	}
	for _, snapshot := range snapshots {
		for _, action := range snapshot.Actions {
			descriptor, found := c.catalog.Action(action.Key)
			if !found || descriptor.Kind != action.Kind || descriptor.ResourceType != action.ResourceType || strings.TrimSpace(descriptor.SurfaceRoutes[string(event.Surface)]) == "" {
				return invalid("backend.notification.inbox_action_contract_invalid", "action_key", action.Key)
			}
		}
	}
	return nil
}

func compileSnapshot(eventType EventType, locale string, content Content, variables map[string]any, subjectID string) (Snapshot, error) {
	title, err := template.RenderRestricted(content.Title, variables, false)
	if err != nil {
		return Snapshot{}, invalid("backend.notification.inbox_template_render_failed", "field", "title")
	}
	body, err := template.RenderRestricted(content.Body, variables, false)
	if err != nil {
		return Snapshot{}, invalid("backend.notification.inbox_template_render_failed", "field", "body")
	}
	facts := make([]template.Fact, 0, len(content.Facts))
	for _, fact := range content.Facts {
		key, keyErr := template.RenderRestricted(fact.Key, variables, false)
		value, valueErr := template.RenderRestricted(fact.Value, variables, false)
		if keyErr != nil || valueErr != nil {
			return Snapshot{}, invalid("backend.notification.inbox_template_render_failed", "field", "facts")
		}
		if strings.TrimSpace(value) != "" {
			facts = append(facts, template.Fact{Key: key, Value: value})
		}
	}
	actions := make([]ActionRef, 0, len(eventType.Actions))
	for _, descriptor := range eventType.Actions {
		label, labelErr := template.RenderRestricted(content.ActionLabels[descriptor.Key], variables, false)
		if labelErr != nil || strings.TrimSpace(label) == "" {
			return Snapshot{}, invalid("backend.notification.inbox_template_render_failed", "field", "action_label", "action_key", descriptor.Key)
		}
		actions = append(actions, ActionRef{Key: descriptor.Key, Kind: descriptor.Kind, Label: label, ResourceType: descriptor.ResourceType, ResourceID: strings.TrimSpace(subjectID), Style: "primary"})
	}
	snapshot := Snapshot{Title: title, Body: body, Facts: facts, Actions: actions, TemplateKey: eventType.TemplateKey, TemplateVersion: eventType.Version, TemplateLocale: locale}
	snapshot.TemplateContentHash = snapshotHash(snapshot)
	return snapshot, nil
}

func (c *Compiler) compilePlans(eventTypeKey string, intent Intent, snapshot Snapshot) []delivery.Plan {
	rule, found := c.catalog.Rule(eventTypeKey)
	if !found || !rule.Enabled {
		return nil
	}
	plans := []delivery.Plan{}
	for _, channel := range rule.Channels {
		if strings.TrimSpace(channel.Channel) == "in_app" {
			continue
		}
		mode := strings.TrimSpace(channel.DeliveryMode)
		if mode == "" {
			mode = "immediate"
		}
		plans = append(plans, delivery.Plan{
			Channel: strings.TrimSpace(channel.Channel), TemplateKey: strings.TrimSpace(channel.TemplateKey), ConnectorKey: strings.TrimSpace(channel.ConnectorKey),
			ConnectionKey: strings.TrimSpace(channel.ConnectionKey), Operation: strings.TrimSpace(channel.Operation), RecipientUserIDs: append([]notification.UserID(nil), intent.RecipientUserIDs...),
			Locale: strings.TrimSpace(intent.Locale), Variables: cloneVariables(intent.Variables), DedupeKey: strings.TrimSpace(intent.DedupeKey), Mandatory: channel.Mandatory,
			DeliveryMode: mode, DigestKey: digestKey(intent, channel), DigestMaximumItems: channel.DigestMaximumItems,
			DigestItemTitle: snapshot.Title, DigestItemBody: snapshot.Body, EscalationStep: channel.EscalationStep,
			CancelWhenActionTerminal: channel.CancelWhenActionTerminal, NextAttemptAt: firstAttemptAt(intent.OccurredAt, channel.DelaySeconds, channel.DigestWindowSeconds),
		})
	}
	return plans
}

func firstAttemptAt(occurredAt string, delaySeconds, digestWindowSeconds int) string {
	value, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(occurredAt))
	if err != nil {
		return ""
	}
	delay := delaySeconds
	if digestWindowSeconds > 0 {
		delay = digestWindowSeconds
	}
	if delay <= 0 {
		return ""
	}
	return notification.Timestamp(value.Add(time.Duration(delay) * time.Second))
}

func digestKey(intent Intent, channel RuleChannel) string {
	if strings.TrimSpace(channel.DeliveryMode) != "digest" {
		return ""
	}
	recipients := make([]string, 0, len(intent.RecipientUserIDs))
	for _, recipient := range intent.RecipientUserIDs {
		recipients = append(recipients, recipient.String())
	}
	return stableID(intent.WorkspaceID.String(), intent.EventType, channel.Channel, channel.TemplateKey, channel.ConnectionKey, strings.Join(recipients, ","), strings.TrimSpace(intent.Locale))
}

func cloneVariables(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	raw, err := json.Marshal(source)
	if err != nil {
		return nil
	}
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil {
		return nil
	}
	return result
}

func eventTypeContent(eventType EventType, requested string) (string, Content) {
	requested = normalizeLocale(requested)
	for locale, content := range eventType.Locales {
		if strings.EqualFold(normalizeLocale(locale), requested) {
			return locale, content
		}
	}
	defaultLocale := normalizeLocale(eventType.DefaultLocale)
	for locale, content := range eventType.Locales {
		if strings.EqualFold(normalizeLocale(locale), defaultLocale) {
			return locale, content
		}
	}
	return "", Content{}
}

func normalizeLocale(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(value, "_", "-"))
}

func eventTypeAllowsSurface(eventType EventType, surface notification.Surface) bool {
	for _, allowed := range eventType.Surfaces {
		if strings.TrimSpace(string(allowed)) == strings.TrimSpace(string(surface)) {
			return true
		}
	}
	return false
}

func stableID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "inbox_" + hex.EncodeToString(sum[:16])
}

func snapshotHash(snapshot Snapshot) string {
	raw, _ := json.Marshal(snapshot)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
