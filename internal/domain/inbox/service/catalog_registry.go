package inbox

import (
	"encoding/json"
	"sort"
	"strings"
)

// Catalog is the immutable effective event contract assembled from event
// definitions contributed by source-owning modules and project configuration.
type Catalog struct {
	eventTypes map[string]EventType
	actions    map[string]ActionDescriptor
	rules      map[string]Rule
}

func NewCatalog(validator *Validator, eventTypes []EventType, rules []Rule) (*Catalog, error) {
	if validator == nil {
		return nil, unavailable("backend.notification.inbox_unavailable", nil)
	}
	if err := validator.ValidateCatalog(eventTypes, rules); err != nil {
		return nil, err
	}
	catalog := &Catalog{eventTypes: map[string]EventType{}, actions: map[string]ActionDescriptor{}, rules: map[string]Rule{}}
	for _, eventType := range eventTypes {
		validated, _ := validator.ValidateEventType(cloneEventType(eventType))
		catalog.eventTypes[validated.Key] = validated
		for _, descriptor := range validated.Actions {
			if current, exists := catalog.actions[descriptor.Key]; exists && (current.Kind != descriptor.Kind || current.ResourceType != descriptor.ResourceType) {
				return nil, invalid("backend.notification.inbox_action_contract_conflict", "action_key", descriptor.Key)
			}
			catalog.actions[descriptor.Key] = descriptor
		}
	}
	for _, rule := range rules {
		catalog.rules[strings.TrimSpace(rule.EventTypeKey)] = cloneRule(rule)
	}
	return catalog, nil
}

func (c *Catalog) Governance() GovernanceCatalog {
	result := GovernanceCatalog{EventTypes: []EventType{}, Rules: []Rule{}}
	if c == nil {
		return result
	}
	for _, value := range c.eventTypes {
		result.EventTypes = append(result.EventTypes, cloneEventType(value))
	}
	for _, value := range c.rules {
		result.Rules = append(result.Rules, cloneRule(value))
	}
	sort.Slice(result.EventTypes, func(i, j int) bool { return result.EventTypes[i].Key < result.EventTypes[j].Key })
	sort.Slice(result.Rules, func(i, j int) bool { return result.Rules[i].EventTypeKey < result.Rules[j].EventTypeKey })
	return result
}

func (c *Catalog) EventType(key string) (EventType, bool) {
	if c == nil {
		return EventType{}, false
	}
	value, found := c.eventTypes[strings.TrimSpace(key)]
	return cloneEventType(value), found
}

func (c *Catalog) Action(key string) (ActionDescriptor, bool) {
	if c == nil {
		return ActionDescriptor{}, false
	}
	value, found := c.actions[strings.TrimSpace(key)]
	return cloneActionDescriptor(value), found
}

func (c *Catalog) Rule(eventTypeKey string) (Rule, bool) {
	if c == nil {
		return Rule{}, false
	}
	value, found := c.rules[strings.TrimSpace(eventTypeKey)]
	return cloneRule(value), found
}

func cloneEventType(value EventType) EventType {
	raw, _ := json.Marshal(value)
	var result EventType
	if json.Unmarshal(raw, &result) != nil {
		return value
	}
	return result
}

func cloneRule(value Rule) Rule {
	raw, _ := json.Marshal(value)
	var result Rule
	if json.Unmarshal(raw, &result) != nil {
		return value
	}
	return result
}

func cloneActionDescriptor(value ActionDescriptor) ActionDescriptor {
	raw, _ := json.Marshal(value)
	var result ActionDescriptor
	if json.Unmarshal(raw, &result) != nil {
		return value
	}
	return result
}
