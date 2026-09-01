package module

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/domainry/domainry-foundation/modulecapability"
	"github.com/domainry/domainry-foundation/modulehttp"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/contract"
)

// ProductRoutes is the source-owned Notification product HTTP manifest. Both
// the executable Module Surface and the model-facing capability disclosure are
// projections of this exact list. SaaS composition mounts the same Surface, so
// switching topology cannot change the product endpoint contract.
func ProductRoutes() []modulehttp.Route {
	routes := []modulehttp.Route{
		templateReadRoute("GET /notifications/capabilities"),
		templateReadRoute("GET /notifications/templates"),
		templateReadRoute("GET /notifications/templates/{templateKey}"),
		templateReadRoute("GET /notifications/publications"),
		templateRoute("POST /notifications/publications/{publicationID}/approve", "notification.template.approve"),
		templateRoute("POST /notifications/publications/{publicationID}/reject", "notification.template.approve"),
		templateRoute("POST /notifications/publications/{publicationID}/cancel", "notification.template.publish", "notification.template.approve"),
		templateRoute("POST /notifications/templates/preview", "notification.template.manage", "notification.template.test"),
		templateRoute("PUT /notifications/templates/{templateKey}/draft", "notification.template.manage"),
		templateRoute("POST /notifications/templates/{templateKey}/publish", "notification.template.approve"),
		templateRoute("POST /notifications/templates/{templateKey}/publication-requests", "notification.template.publish"),
		templateRoute("POST /notifications/templates/{templateKey}/disable", "notification.template.publish"),
		templateRoute("POST /notifications/templates/{templateKey}/preview", "notification.template.read", "notification.template.test"),
		templateReadRoute("GET /notifications/templates/{templateKey}/versions"),
		templateRoute("POST /notifications/templates/{templateKey}/versions/{version}/restore-draft", "notification.template.manage"),
		templateRoute("GET /notifications/policy", "notification.policy.read", "notification.policy.manage"),
		templateRoute("PUT /notifications/policy", "notification.policy.manage"),
		templateRoute("GET /notifications/preferences", "notification.policy.read", "notification.policy.manage"),
		templateRoute("PUT /notifications/preferences/{recipientKey}", "notification.policy.manage"),
		templateRoute("GET /notifications/metrics", "integration.audit.view", "notification.policy.read"),
		templateReadRoute("GET /notifications/governance/catalog"),
		templateRoute("GET /notifications/governance/inbox-metrics", "integration.audit.view", "notification.policy.read"),
	}
	for _, prefix := range []string{"/business", "/portal"} {
		routes = append(routes, inboxRoutes(prefix)...)
	}
	return routes
}

func notificationRouteGovernance(pattern string) *modulehttp.Governance {
	method, path, _ := strings.Cut(strings.TrimSpace(pattern), " ")
	read := method == http.MethodGet || strings.Contains(path, "/preview") || strings.HasSuffix(path, "/publish")
	if read {
		return &modulehttp.Governance{EffectClass: modulehttp.EffectRead, HighRiskPolicy: modulehttp.HighRiskNone, IdempotencyDecision: "not_applicable", AuditClass: "notification_owner_read"}
	}
	idempotency := "optimistic_only"
	if method == http.MethodPut || method == http.MethodDelete || strings.Contains(path, "/notifications/") || strings.HasSuffix(path, "/read-all") {
		idempotency = "natural_key"
	}
	return &modulehttp.Governance{EffectClass: modulehttp.EffectWrite, HighRiskPolicy: modulehttp.HighRiskNone, IdempotencyDecision: idempotency, AuditClass: "notification_owner_mutation"}
}

// NewCapabilityBinding builds the immutable disclosure envelope. The supplied
// validator remains in the owning module assembly because it needs the exact
// provider catalog opened for this application.
func NewCapabilityBinding(templateCapabilities []contract.NotificationTemplateCapability, validator modulecapability.Validator) (*modulecapability.StaticBinding, error) {
	routes := ProductRoutes()
	byCategory := map[string][]modulehttp.Route{}
	for _, route := range routes {
		// This endpoint is an intentional compatibility tombstone that always
		// returns 409. It is mounted for old callers but is not a usable model
		// capability; publication-requests is the source-owned replacement.
		if route.Pattern == "POST /notifications/templates/{templateKey}/publish" {
			continue
		}
		key := notificationCapabilityCategory(route.Pattern)
		byCategory[key] = append(byCategory[key], route)
	}
	categoryKeys := make([]string, 0, len(byCategory))
	for key := range byCategory {
		categoryKeys = append(categoryKeys, key)
	}
	sort.Strings(categoryKeys)
	documents := make([]modulecapability.CategoryDocument, 0, len(categoryKeys))
	for _, key := range categoryKeys {
		categoryRoutes := byCategory[key]
		sort.Slice(categoryRoutes, func(i, j int) bool { return categoryRoutes[i].Pattern < categoryRoutes[j].Pattern })
		paths := map[string]map[string]json.RawMessage{}
		for _, route := range categoryRoutes {
			method, path, found := strings.Cut(route.Pattern, " ")
			if !found {
				return nil, fmt.Errorf("Notification capability route %q is invalid", route.Pattern)
			}
			if paths[path] == nil {
				paths[path] = map[string]json.RawMessage{}
			}
			operation, err := notificationOpenAPIOperation(route)
			if err != nil {
				return nil, err
			}
			paths[path][strings.ToLower(method)] = operation
		}
		name, description, scopes, assembly := notificationCategoryMetadata(key)
		document := modulecapability.CategoryDocument{
			Category: modulecapability.CategorySummary{Key: key, Name: name, Description: description, OperationCount: len(categoryRoutes), AssemblyChains: assembly, ValidationScopes: scopes},
			OpenAPI: modulecapability.OpenAPIFragment{OpenAPI: "3.1.0", Paths: paths, Components: map[string]map[string]json.RawMessage{
				"securitySchemes": {"BearerAuth": json.RawMessage(`{"type":"http","scheme":"bearer","bearerFormat":"JWT"}`)},
			}},
		}
		switch key {
		case "notification.delivery_governance":
			document.ValidationContracts = []modulecapability.ValidationScopeContract{
				{Kind: "notification.event_type", Description: "Validate one published project notification event type.", Coverage: modulecapability.ValidationCoverageAllCandidates, CandidateCollections: []string{"notification_event_types"}},
				{Kind: "notification.rule", Description: "Validate one notification rule against explicitly referenced event types.", Coverage: modulecapability.ValidationCoverageAllCandidates, CandidateCollections: []string{"notification_rules"}, ReferencedCollections: []string{"notification_event_types"}},
			}
		case "notification.templates":
			document.ValidationContracts = []modulecapability.ValidationScopeContract{{Kind: "notification.template", Description: "Validate one project-owned external-channel notification template.", Coverage: modulecapability.ValidationCoverageAllCandidates, CandidateCollections: []string{"notification_templates"}}}
		}
		documents = append(documents, document)
	}
	providedCapabilities := []string{"notification.intent", "notification.template", "notification.event_type", "notification.rule", "notification.inbox_query", "notification.inbox_saved_view", "notification.inbox_delegation", "notification.delivery_policy", "notification.recipient_preference"}
	providerCapabilities := append([]contract.NotificationTemplateCapability(nil), templateCapabilities...)
	sort.Slice(providerCapabilities, func(i, j int) bool {
		if providerCapabilities[i].Channel == providerCapabilities[j].Channel {
			return providerCapabilities[i].Provider < providerCapabilities[j].Provider
		}
		return providerCapabilities[i].Channel < providerCapabilities[j].Channel
	})
	for _, capability := range providerCapabilities {
		channel, provider := strings.TrimSpace(capability.Channel), strings.TrimSpace(capability.Provider)
		if channel == "" {
			continue
		}
		key := "notification.channel." + channel
		if provider != "" {
			key = "notification.provider." + channel + "." + provider
		}
		providedCapabilities = append(providedCapabilities, key)
	}
	providedCapabilities = uniqueNotificationCapabilityStrings(providedCapabilities)
	summary := modulecapability.ModuleSummary{
		Identity: modulecapability.ModuleIdentity{
			Key: "notification", SourceOwner: "notification", ModuleVersion: notificationsdk.CurrentProtocolVersion,
			ValidationRevision:       "notification-owner-validation-v1",
			SupportedDeploymentModes: []modulecapability.DeploymentMode{modulecapability.DeploymentModeModule, modulecapability.DeploymentModeSaaS},
		},
		Name:        "Notification",
		Description: "User-facing notification inboxes, governed templates, recipient preferences, and durable channel-delivery policy.",
		Scenarios: modulecapability.AdaptationScenarios{
			UseWhen: []string{
				"A PRD requires user-visible notifications, alerts, read or archive state, recipient preferences, or a durable Inbox",
				"A PRD requires governed notification templates, publication review, digesting, or delivery through one or more channels",
			},
			DoNotUseWhen: []string{
				"A workflow only changes internal state and has no user-facing message, alert, or delivery requirement",
				"The requirement is only an external provider callback or data integration without a user notification experience",
			},
			RequirementSignals:   []string{"notify or alert a user", "inbox and unread state", "email, SMS, push, or in-app delivery", "notification template and preference", "delivery retry, digest, or governance"},
			ProvidedCapabilities: providedCapabilities,
			RequiredModules:      []string{"identity"}, OptionalModules: []string{"integration", "scheduler"}, ConflictingModules: []string{},
			AssemblyChains: []string{
				"domain_event_to_notification_intent_to_inbox_and_channel_delivery",
				"notification_template_publication_before_event_delivery",
				"identity_principal_before_business_inbox_access",
				"identity_principal_before_consumer_inbox_access",
			},
			ValidationScopes:  []string{"notification.event_type", "notification.rule", "notification.template"},
			SelectionExamples: []modulecapability.ScenarioExample{{Requirement: "Notify an approver, show it in their Inbox, and email them according to preferences", Reason: "Notification owns durable user Inbox state, templates, preferences, and channel delivery"}},
			RejectionExamples: []modulecapability.ScenarioExample{{Requirement: "Call a payment provider and store its response without notifying a user", Reason: "Integration owns the provider call; Notification is unnecessary without a user-facing message"}},
		},
	}
	return modulecapability.NewStaticBinding(summary, documents, validator)
}

func uniqueNotificationCapabilityStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func notificationCapabilityCategory(pattern string) string {
	_, path, _ := strings.Cut(pattern, " ")
	switch {
	case strings.HasPrefix(path, "/business/"):
		return "notification.business_inbox"
	case strings.HasPrefix(path, "/portal/"):
		return "notification.consumer_inbox"
	case path == "/notifications/policy" || path == "/notifications/preferences" || strings.HasPrefix(path, "/notifications/preferences/") || path == "/notifications/metrics" || strings.HasPrefix(path, "/notifications/governance/"):
		return "notification.delivery_governance"
	default:
		return "notification.templates"
	}
}

func notificationCategoryMetadata(key string) (string, string, []string, []string) {
	switch key {
	case "notification.business_inbox":
		return "Business notification Inbox", "Read and manage the authenticated workforce principal's durable Business Workspace notification Inbox.", []string{}, []string{"identity_principal_before_business_inbox_access"}
	case "notification.consumer_inbox":
		return "Consumer notification Inbox", "Read and manage the authenticated consumer principal's durable Portal notification Inbox.", []string{}, []string{"identity_principal_before_consumer_inbox_access"}
	case "notification.delivery_governance":
		return "Notification delivery governance", "Configure event types and delivery rules and inspect delivery and Inbox governance metrics.", []string{"notification.event_type", "notification.rule"}, []string{"domain_event_to_notification_intent_to_inbox_and_channel_delivery"}
	default:
		return "Notification templates", "Author, preview, review, publish, disable, and restore governed notification templates.", []string{"notification.template"}, []string{"notification_template_publication_before_event_delivery"}
	}
}

func notificationOpenAPIOperations(routes []modulehttp.Route) map[string]map[string]any {
	result := make(map[string]map[string]any, len(routes))
	for _, route := range routes {
		_, _, found := strings.Cut(route.Pattern, " ")
		if !found {
			continue
		}
		operation, err := notificationOpenAPIOperation(route)
		if err != nil {
			continue
		}
		var value map[string]any
		if json.Unmarshal(operation, &value) != nil {
			continue
		}
		result[route.Pattern] = value
	}
	return result
}

func notificationOpenAPIOperation(route modulehttp.Route) (json.RawMessage, error) {
	method, path, found := strings.Cut(strings.TrimSpace(route.Pattern), " ")
	if !found {
		return nil, fmt.Errorf("Notification route %q is invalid", route.Pattern)
	}
	method = strings.ToLower(method)
	governance := route.Governance
	if governance == nil {
		return nil, fmt.Errorf("Notification route %q has no governance", route.Pattern)
	}
	authorization := modulecapability.Authorization{Mode: modulecapability.AuthorizationPrincipal, WorkspaceScope: "authenticated_workspace_principal"}
	if len(route.AnyPermissions) != 0 {
		authorization.Mode = modulecapability.AuthorizationFixed
		authorization.AnyOf = append([]string(nil), route.AnyPermissions...)
		sort.Strings(authorization.AnyOf)
	} else if strings.TrimSpace(route.Permission) != "" {
		authorization.Mode = modulecapability.AuthorizationFixed
		authorization.AllOf = []string{strings.TrimSpace(route.Permission)}
	}
	extension := modulecapability.OperationExtension{
		Owner: "notification", Authorization: authorization,
		Effect:      modulecapability.EffectClass(governance.EffectClass),
		Idempotency: modulecapability.Idempotency{Mode: governance.IdempotencyDecision},
	}
	if strings.HasSuffix(path, "/notifications/stream") {
		extension.Transport = &modulecapability.Transport{Mode: "sse", ResumeSemantics: "Last-Event-ID or cursor; each signal requires a durable Inbox refetch", DeliveryOrdering: "latest state cursor"}
	}
	operation := map[string]any{
		"operationId":                          notificationOperationID(method, path),
		"tags":                                 []string{notificationOperationTag(path)},
		"summary":                              notificationOperationSummary(method, path),
		"description":                          notificationOperationDescription(method, path),
		"security":                             []any{map[string]any{"BearerAuth": []any{}}},
		modulecapability.OperationExtensionKey: extension,
	}
	if parameters := notificationOperationParameters(method, path); len(parameters) != 0 {
		operation["parameters"] = parameters
	}
	if body := notificationRequestSchema(method, path); body != nil {
		operation["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": body}}}
	}
	responses := map[string]any{
		"400": map[string]any{"description": "Notification request is invalid"},
		"401": map[string]any{"description": "Authentication is required"},
		"403": map[string]any{"description": "Notification access is denied"},
	}
	if method == strings.ToLower(http.MethodDelete) {
		responses["204"] = map[string]any{"description": "Notification resource removed"}
	} else if strings.HasSuffix(path, "/templates/{templateKey}/publish") {
		responses["409"] = map[string]any{"description": "Direct publication is disabled; create a publication request"}
	} else {
		status := "200"
		if strings.HasSuffix(path, "/publication-requests") {
			status = "201"
		}
		contentType := "application/json"
		if strings.HasSuffix(path, "/notifications/stream") {
			contentType = "text/event-stream"
		}
		responses[status] = map[string]any{"description": notificationResponseDescription(path), "content": map[string]any{contentType: map[string]any{"schema": notificationResponseSchema(method, path)}}}
	}
	operation["responses"] = responses
	payload, err := json.Marshal(operation)
	return json.RawMessage(payload), err
}

func notificationOperationID(method, path string) string {
	text := method + " " + strings.Trim(path, "/")
	var result strings.Builder
	upper := false
	for i, r := range text {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			upper = true
			continue
		}
		if result.Len() == 0 {
			result.WriteRune(unicode.ToLower(r))
		} else if upper || i == 0 {
			result.WriteRune(unicode.ToUpper(r))
		} else {
			result.WriteRune(r)
		}
		upper = false
	}
	return result.String()
}

func notificationOperationTag(path string) string {
	switch {
	case strings.HasPrefix(path, "/business/"):
		return "Notification Business Inbox"
	case strings.HasPrefix(path, "/portal/"):
		return "Notification Consumer Inbox"
	case strings.Contains(path, "/governance/") || strings.HasSuffix(path, "/policy") || strings.Contains(path, "/preferences") || strings.HasSuffix(path, "/metrics"):
		return "Notification Delivery Governance"
	default:
		return "Notification Templates"
	}
}

func notificationOperationSummary(method, path string) string {
	return strings.ToUpper(method) + " " + strings.ReplaceAll(strings.Trim(path, "/"), "-", " ")
}

func notificationOperationDescription(method, path string) string {
	if strings.HasSuffix(path, "/notifications/stream") {
		return "Resume the authenticated principal's Inbox synchronization stream and refetch durable state after each signal."
	}
	if strings.HasSuffix(path, "/templates/{templateKey}/publish") {
		return "Compatibility tombstone. Direct publication is rejected; use the publication request and review flow."
	}
	return "Notification-owned " + strings.ToLower(notificationOperationTag(path)) + " operation."
}

func notificationOperationParameters(method, path string) []any {
	parameters := []any{}
	for _, segment := range strings.Split(path, "/") {
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			name := strings.TrimSuffix(strings.TrimPrefix(segment, "{"), "}")
			schema := map[string]any{"type": "string", "minLength": 1}
			if name == "version" {
				schema = map[string]any{"type": "integer", "minimum": 1}
			}
			parameters = append(parameters, map[string]any{"name": name, "in": "path", "required": true, "schema": schema})
		}
	}
	addQuery := func(name string, schema map[string]any) {
		parameters = append(parameters, map[string]any{"name": name, "in": "query", "required": false, "schema": schema})
	}
	if strings.HasSuffix(path, "/notifications") || strings.HasSuffix(path, "/notifications/facets") || strings.HasSuffix(path, "/notifications/read-all") {
		for _, name := range []string{"mailbox", "query", "scope", "team_member_id", "delegated_owner_id", "from", "to"} {
			addQuery(name, map[string]any{"type": "string"})
		}
		for _, name := range []string{"category", "source", "severity", "action_state"} {
			addQuery(name, map[string]any{"type": "array", "items": map[string]any{"type": "string"}})
		}
		if !strings.HasSuffix(path, "/read-all") {
			addQuery("limit", map[string]any{"type": "integer", "minimum": 0, "maximum": 100})
			addQuery("cursor", map[string]any{"type": "string"})
		}
	}
	if strings.HasSuffix(path, "/notifications/stream") {
		parameters = append(parameters, map[string]any{"name": "Last-Event-ID", "in": "header", "required": false, "schema": map[string]any{"type": "string"}})
		addQuery("cursor", map[string]any{"type": "string"})
	}
	if path == "/notifications/publications" {
		addQuery("template_key", map[string]any{"type": "string"})
	}
	if path == "/notifications/metrics" || path == "/notifications/governance/inbox-metrics" {
		addQuery("hours", map[string]any{"type": "integer", "minimum": 1, "maximum": 720, "default": 24})
	}
	_ = method
	return parameters
}

func notificationRequestSchema(method, path string) map[string]any {
	if method != strings.ToLower(http.MethodPost) && method != strings.ToLower(http.MethodPut) && method != strings.ToLower(http.MethodPatch) {
		return nil
	}
	switch {
	case strings.HasSuffix(path, "/notifications/read-all"), strings.HasSuffix(path, "/read"), strings.HasSuffix(path, "/unread"), strings.HasSuffix(path, "/archive"), strings.HasSuffix(path, "/restore"), strings.HasSuffix(path, "/acknowledge"), strings.HasSuffix(path, "/approve"), strings.HasSuffix(path, "/cancel"), strings.HasSuffix(path, "/publish"):
		return nil
	case strings.HasSuffix(path, "/notification-preferences"):
		return openAPISchemaFor(reflect.TypeOf(contract.NotificationRecipientPreference{}), map[reflect.Type]bool{})
	case strings.Contains(path, "/saved-views/"):
		return openAPISchemaFor(reflect.TypeOf(contract.NotificationInboxSavedView{}), map[reflect.Type]bool{})
	case strings.Contains(path, "/delegations/"):
		return openAPISchemaFor(reflect.TypeOf(contract.NotificationInboxDelegation{}), map[reflect.Type]bool{})
	case path == "/notifications/templates/preview":
		return openAPISchemaFor(reflect.TypeOf(previewDraftInput{}), map[reflect.Type]bool{})
	case strings.HasSuffix(path, "/draft"):
		return openAPISchemaFor(reflect.TypeOf(draftInput{}), map[reflect.Type]bool{})
	case strings.HasSuffix(path, "/publication-requests"):
		return openAPISchemaFor(reflect.TypeOf(publicationInput{}), map[reflect.Type]bool{})
	case strings.HasSuffix(path, "/disable"), strings.HasSuffix(path, "/restore-draft"):
		return openAPISchemaFor(reflect.TypeOf(lifecycleInput{}), map[reflect.Type]bool{})
	case strings.HasSuffix(path, "/preview"):
		return openAPISchemaFor(reflect.TypeOf(previewInput{}), map[reflect.Type]bool{})
	case strings.HasSuffix(path, "/reject"):
		return openAPISchemaFor(reflect.TypeOf(reviewInput{}), map[reflect.Type]bool{})
	case path == "/notifications/policy":
		return openAPISchemaFor(reflect.TypeOf(contract.NotificationDeliveryPolicy{}), map[reflect.Type]bool{})
	case strings.HasPrefix(path, "/notifications/preferences/"):
		return openAPISchemaFor(reflect.TypeOf(contract.NotificationRecipientPreference{}), map[reflect.Type]bool{})
	default:
		return nil
	}
}

func notificationResponseSchema(method, path string) map[string]any {
	switch {
	case strings.HasSuffix(path, "/notifications/stream"):
		return map[string]any{"type": "string", "description": "Server-sent notification.sync and notification.ready events"}
	case strings.HasSuffix(path, "/notifications/unread-count"):
		return openAPISchemaFor(reflect.TypeOf(struct {
			Unread int `json:"unread"`
		}{}), map[reflect.Type]bool{})
	case strings.HasSuffix(path, "/notifications/facets"):
		return openAPISchemaFor(reflect.TypeOf(contract.NotificationInboxFacets{}), map[reflect.Type]bool{})
	case strings.HasSuffix(path, "/notifications/read-all"):
		return openAPISchemaFor(reflect.TypeOf(struct {
			Updated int `json:"updated"`
		}{}), map[reflect.Type]bool{})
	case strings.HasSuffix(path, "/notifications/saved-views"):
		return openAPISchemaFor(reflect.TypeOf(struct {
			Views []contract.NotificationInboxSavedView `json:"views"`
			Count int                                   `json:"count"`
		}{}), map[reflect.Type]bool{})
	case strings.Contains(path, "/notifications/saved-views/"):
		return openAPISchemaFor(reflect.TypeOf(contract.NotificationInboxSavedView{}), map[reflect.Type]bool{})
	case strings.HasSuffix(path, "/notifications/delegations"):
		return openAPISchemaFor(reflect.TypeOf(struct {
			Delegations []contract.NotificationInboxDelegation `json:"delegations"`
			Count       int                                    `json:"count"`
		}{}), map[reflect.Type]bool{})
	case strings.Contains(path, "/notifications/delegations/"):
		return openAPISchemaFor(reflect.TypeOf(contract.NotificationInboxDelegation{}), map[reflect.Type]bool{})
	case strings.HasSuffix(path, "/notifications/delegated-owners"):
		return openAPISchemaFor(reflect.TypeOf(struct {
			OwnerUserIDs []string `json:"owner_user_ids"`
			Count        int      `json:"count"`
		}{}), map[reflect.Type]bool{})
	case strings.HasSuffix(path, "/notification-preferences"):
		return openAPISchemaFor(reflect.TypeOf(contract.NotificationRecipientPreference{}), map[reflect.Type]bool{})
	case strings.Contains(path, "/notifications/{notificationID}"):
		return openAPISchemaFor(reflect.TypeOf(contract.NotificationInboxItem{}), map[reflect.Type]bool{})
	case strings.HasSuffix(path, "/notifications"):
		return openAPISchemaFor(reflect.TypeOf(contract.NotificationInboxPage{}), map[reflect.Type]bool{})
	case path == "/notifications/capabilities":
		return openAPISchemaFor(reflect.TypeOf(struct {
			Capabilities []contract.NotificationTemplateCapability `json:"capabilities"`
			Count        int                                       `json:"count"`
		}{}), map[reflect.Type]bool{})
	case path == "/notifications/templates":
		return openAPISchemaFor(reflect.TypeOf(struct {
			Templates []contract.NotificationTemplateRecord `json:"templates"`
			Count     int                                   `json:"count"`
		}{}), map[reflect.Type]bool{})
	case path == "/notifications/publications":
		return openAPISchemaFor(reflect.TypeOf(struct {
			Publications []contract.NotificationPublicationRequest `json:"publications"`
			Count        int                                       `json:"count"`
		}{}), map[reflect.Type]bool{})
	case strings.Contains(path, "/publications/"):
		return openAPISchemaFor(reflect.TypeOf(contract.NotificationPublicationRequest{}), map[reflect.Type]bool{})
	case path == "/notifications/templates/preview" || strings.HasSuffix(path, "/preview"):
		return openAPISchemaFor(reflect.TypeOf(contract.RenderedNotification{}), map[reflect.Type]bool{})
	case strings.HasSuffix(path, "/versions"):
		return openAPISchemaFor(reflect.TypeOf(struct {
			Versions []contract.NotificationTemplateVersion `json:"versions"`
			Count    int                                    `json:"count"`
		}{}), map[reflect.Type]bool{})
	case strings.Contains(path, "/templates/"):
		return openAPISchemaFor(reflect.TypeOf(contract.NotificationTemplateRecord{}), map[reflect.Type]bool{})
	case path == "/notifications/policy":
		return openAPISchemaFor(reflect.TypeOf(contract.NotificationDeliveryPolicy{}), map[reflect.Type]bool{})
	case path == "/notifications/preferences":
		return openAPISchemaFor(reflect.TypeOf(struct {
			Preferences []contract.NotificationRecipientPreference `json:"preferences"`
			Count       int                                        `json:"count"`
		}{}), map[reflect.Type]bool{})
	case strings.HasPrefix(path, "/notifications/preferences/"):
		return openAPISchemaFor(reflect.TypeOf(contract.NotificationRecipientPreference{}), map[reflect.Type]bool{})
	case path == "/notifications/metrics":
		return openAPISchemaFor(reflect.TypeOf(contract.NotificationDeliveryMetrics{}), map[reflect.Type]bool{})
	case path == "/notifications/governance/catalog":
		return openAPISchemaFor(reflect.TypeOf(contract.NotificationGovernanceCatalog{}), map[reflect.Type]bool{})
	case path == "/notifications/governance/inbox-metrics":
		return openAPISchemaFor(reflect.TypeOf(contract.NotificationInboxGovernanceMetrics{}), map[reflect.Type]bool{})
	default:
		_ = method
		return map[string]any{"type": "object"}
	}
}

func notificationResponseDescription(path string) string {
	if strings.HasSuffix(path, "/notifications/stream") {
		return "Inbox synchronization SSE stream"
	}
	return "Notification-owned operation result"
}

func openAPISchemaFor(value reflect.Type, stack map[reflect.Type]bool) map[string]any {
	for value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if value == reflect.TypeOf(time.Time{}) {
		return map[string]any{"type": "string", "format": "date-time"}
	}
	if value == reflect.TypeOf(json.RawMessage{}) {
		return map[string]any{}
	}
	switch value.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Interface:
		return map[string]any{}
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": openAPISchemaFor(value.Elem(), stack)}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": openAPISchemaFor(value.Elem(), stack)}
	case reflect.Struct:
		if stack[value] {
			return map[string]any{"type": "object"}
		}
		stack[value] = true
		defer delete(stack, value)
		properties := map[string]any{}
		required := []string{}
		for index := 0; index < value.NumField(); index++ {
			field := value.Field(index)
			if field.PkgPath != "" {
				continue
			}
			tag := field.Tag.Get("json")
			name, options, _ := strings.Cut(tag, ",")
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			properties[name] = openAPISchemaFor(field.Type, stack)
			if !strings.Contains(options, "omitempty") {
				required = append(required, name)
			}
		}
		sort.Strings(required)
		result := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
		if len(required) != 0 {
			result["required"] = required
		}
		return result
	default:
		return map[string]any{}
	}
}

var _ modulehttp.OpenAPIProvider = (*surface)(nil)
