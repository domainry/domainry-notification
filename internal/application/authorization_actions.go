package application

import (
	"fmt"
	"net/http"
	"strings"

	actioncontract "github.com/domainry/domainry-foundation/action"
)

const NotificationAuthorizationOwner = "module:notification"

const (
	ActionCapabilitiesRead             = "notification.capabilities.read"
	ActionTemplatesList                = "notification.templates.list"
	ActionTemplatesGet                 = "notification.templates.get"
	ActionPublicationsList             = "notification.publications.list"
	ActionPublicationsApprove          = "notification.publications.approve"
	ActionPublicationsReject           = "notification.publications.reject"
	ActionPublicationsCancel           = "notification.publications.cancel"
	ActionTemplatesPreviewDraft        = "notification.templates.preview_draft"
	ActionTemplatesDraftSave           = "notification.templates.draft.save"
	ActionPublicationsRequest          = "notification.publications.request"
	ActionTemplatesDisable             = "notification.templates.disable"
	ActionTemplatesPreview             = "notification.templates.preview"
	ActionTemplateVersionsList         = "notification.templates.versions.list"
	ActionTemplateVersionsRestoreDraft = "notification.templates.versions.restore_draft"
	ActionDeliveryPolicyGet            = "notification.delivery_policy.get"
	ActionDeliveryPolicyUpdate         = "notification.delivery_policy.update"
	ActionRecipientPreferencesList     = "notification.recipient_preferences.list"
	ActionRecipientPreferencesUpdate   = "notification.recipient_preferences.update"
	ActionDeliveryMetricsRead          = "notification.delivery_metrics.read"
	ActionGovernanceCatalogRead        = "notification.governance.catalog.read"
	ActionGovernanceInboxMetricsRead   = "notification.governance.inbox_metrics.read"
)

type notificationHTTPActionSpec struct {
	Key, Pattern, Label string
	Effect              actioncontract.EffectClass
	Risk                actioncontract.RiskLevel
}

// AuthorizationActions is Notification's single executable HTTP authorization
// manifest. Every user route owns one exact same-key Permission. Identity must
// pair it with canonical data_scope=all; repositories still enforce workspace
// isolation on every durable read and write.
func AuthorizationActions() ([]actioncontract.ActionDefinition, error) {
	management := []notificationHTTPActionSpec{
		{Key: ActionCapabilitiesRead, Pattern: "GET /notification/capabilities", Label: "Read notification capabilities", Effect: actioncontract.EffectRead, Risk: actioncontract.RiskLow},
		{Key: ActionTemplatesList, Pattern: "GET /notification/templates", Label: "List notification templates", Effect: actioncontract.EffectRead, Risk: actioncontract.RiskLow},
		{Key: ActionTemplatesGet, Pattern: "GET /notification/templates/{templateKey}", Label: "Get notification template", Effect: actioncontract.EffectRead, Risk: actioncontract.RiskLow},
		{Key: ActionPublicationsList, Pattern: "GET /notification/publications", Label: "List publication requests", Effect: actioncontract.EffectRead, Risk: actioncontract.RiskLow},
		{Key: ActionPublicationsApprove, Pattern: "POST /notification/publications/{publicationID}/approve", Label: "Approve publication request", Effect: actioncontract.EffectWrite, Risk: actioncontract.RiskHigh},
		{Key: ActionPublicationsReject, Pattern: "POST /notification/publications/{publicationID}/reject", Label: "Reject publication request", Effect: actioncontract.EffectWrite, Risk: actioncontract.RiskHigh},
		{Key: ActionPublicationsCancel, Pattern: "POST /notification/publications/{publicationID}/cancel", Label: "Cancel publication request", Effect: actioncontract.EffectWrite, Risk: actioncontract.RiskMedium},
		{Key: ActionTemplatesPreviewDraft, Pattern: "POST /notification/templates/preview", Label: "Preview notification template draft", Effect: actioncontract.EffectRead, Risk: actioncontract.RiskLow},
		{Key: ActionTemplatesDraftSave, Pattern: "PUT /notification/templates/{templateKey}/draft", Label: "Save notification template draft", Effect: actioncontract.EffectWrite, Risk: actioncontract.RiskMedium},
		{Key: ActionPublicationsRequest, Pattern: "POST /notification/templates/{templateKey}/publication-requests", Label: "Request notification template publication", Effect: actioncontract.EffectWrite, Risk: actioncontract.RiskHigh},
		{Key: ActionTemplatesDisable, Pattern: "POST /notification/templates/{templateKey}/disable", Label: "Disable notification template", Effect: actioncontract.EffectWrite, Risk: actioncontract.RiskHigh},
		{Key: ActionTemplatesPreview, Pattern: "POST /notification/templates/{templateKey}/preview", Label: "Preview notification template", Effect: actioncontract.EffectRead, Risk: actioncontract.RiskLow},
		{Key: ActionTemplateVersionsList, Pattern: "GET /notification/templates/{templateKey}/versions", Label: "List notification template versions", Effect: actioncontract.EffectRead, Risk: actioncontract.RiskLow},
		{Key: ActionTemplateVersionsRestoreDraft, Pattern: "POST /notification/templates/{templateKey}/versions/{version}/restore-draft", Label: "Restore notification template draft", Effect: actioncontract.EffectWrite, Risk: actioncontract.RiskMedium},
		{Key: ActionDeliveryPolicyGet, Pattern: "GET /notification/policy", Label: "Get notification delivery policy", Effect: actioncontract.EffectRead, Risk: actioncontract.RiskLow},
		{Key: ActionDeliveryPolicyUpdate, Pattern: "PUT /notification/policy", Label: "Update notification delivery policy", Effect: actioncontract.EffectWrite, Risk: actioncontract.RiskHigh},
		{Key: ActionRecipientPreferencesList, Pattern: "GET /notification/preferences", Label: "List recipient preferences", Effect: actioncontract.EffectRead, Risk: actioncontract.RiskLow},
		{Key: ActionRecipientPreferencesUpdate, Pattern: "PUT /notification/preferences/{recipientKey}", Label: "Update recipient preference", Effect: actioncontract.EffectWrite, Risk: actioncontract.RiskMedium},
		{Key: ActionDeliveryMetricsRead, Pattern: "GET /notification/metrics", Label: "Read notification delivery metrics", Effect: actioncontract.EffectRead, Risk: actioncontract.RiskLow},
		{Key: ActionGovernanceCatalogRead, Pattern: "GET /notification/governance/catalog", Label: "Read notification governance catalog", Effect: actioncontract.EffectRead, Risk: actioncontract.RiskLow},
		{Key: ActionGovernanceInboxMetricsRead, Pattern: "GET /notification/governance/inbox-metrics", Label: "Read Inbox governance metrics", Effect: actioncontract.EffectRead, Risk: actioncontract.RiskLow},
	}
	definitions := make([]actioncontract.ActionDefinition, 0, len(management)+20)
	for _, spec := range management {
		definition, err := notificationHTTPAction(spec, true, "notification.management", "Notification management", actioncontract.ExposureManagement)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, definition)
	}
	for _, spec := range notificationInboxActionSpecs() {
		definition, err := notificationHTTPAction(spec, true, "notification.inbox", "Notification Inbox", actioncontract.ExposurePublic)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, definition)
	}
	return definitions, nil
}

func notificationHTTPAction(spec notificationHTTPActionSpec, permission bool, capabilityKey, capabilityLabel string, exposure actioncontract.Exposure) (actioncontract.ActionDefinition, error) {
	method, route, found := strings.Cut(strings.TrimSpace(spec.Pattern), " ")
	separator := strings.LastIndex(spec.Key, ".")
	if !found || separator <= 0 || separator == len(spec.Key)-1 {
		return actioncontract.ActionDefinition{}, fmt.Errorf("Notification Action %q has invalid identity or HTTP pattern", spec.Key)
	}
	resourceKey, operationKey := spec.Key[:separator], spec.Key[separator+1:]
	idempotency := "not_applicable"
	if spec.Effect == actioncontract.EffectWrite {
		idempotency = "natural_key"
	}
	definition := actioncontract.ActionDefinition{
		Key: spec.Key, Owner: NotificationAuthorizationOwner, SourceKind: "module_http",
		CapabilityKey: capabilityKey, CapabilityLabel: capabilityLabel,
		OperationKey: operationKey, OperationLabel: spec.Label, Label: spec.Label,
		Exposures: []actioncontract.Exposure{exposure}, HTTP: &actioncontract.HTTPBinding{Method: method, RouteTemplate: route},
		EffectClass: spec.Effect, RiskLevel: spec.Risk, IdempotencyDecision: idempotency,
		AuditClass: "notification_http", LifecycleStatus: actioncontract.LifecycleActive,
	}
	if permission {
		definition.Authorization = actioncontract.Authorization{Strategy: actioncontract.AuthorizationAuthenticated}
		definition.Permission = &actioncontract.PermissionDefinition{
			Key: spec.Key, Owner: NotificationAuthorizationOwner, ResourceKey: resourceKey, OperationKey: operationKey,
			Label: spec.Label, Category: capabilityLabel, LifecycleStatus: actioncontract.LifecycleActive,
		}
	} else {
		definition.Authorization = actioncontract.Authorization{Strategy: actioncontract.AuthorizationAuthenticated}
	}
	definition, err := actioncontract.NormalizeDefinition(definition)
	if err != nil {
		return actioncontract.ActionDefinition{}, fmt.Errorf("Notification Action %q: %w", spec.Key, err)
	}
	return definition, nil
}

func notificationInboxActionSpecs() []notificationHTTPActionSpec {
	read := actioncontract.EffectRead
	write := actioncontract.EffectWrite
	low, medium := actioncontract.RiskLow, actioncontract.RiskMedium
	base := "notification.inbox"
	prefix := "/notification/inbox"
	return []notificationHTTPActionSpec{
		{Key: base + ".list", Pattern: http.MethodGet + " " + prefix, Label: "List Inbox notifications", Effect: read, Risk: low},
		{Key: base + ".facets", Pattern: http.MethodGet + " " + prefix + "/facets", Label: "Read Inbox facets", Effect: read, Risk: low},
		{Key: base + ".unread_count", Pattern: http.MethodGet + " " + prefix + "/unread-count", Label: "Read Inbox unread count", Effect: read, Risk: low},
		{Key: base + ".stream", Pattern: http.MethodGet + " " + prefix + "/stream", Label: "Open Inbox synchronization stream", Effect: read, Risk: low},
		{Key: base + ".preference.get", Pattern: http.MethodGet + " " + prefix + "/preference", Label: "Get personal notification preference", Effect: read, Risk: low},
		{Key: base + ".preference.update", Pattern: http.MethodPut + " " + prefix + "/preference", Label: "Update personal notification preference", Effect: write, Risk: medium},
		{Key: base + ".saved_views.list", Pattern: http.MethodGet + " " + prefix + "/saved-views", Label: "List personal Inbox saved views", Effect: read, Risk: low},
		{Key: base + ".saved_views.save", Pattern: http.MethodPut + " " + prefix + "/saved-views/{viewKey}", Label: "Save personal Inbox view", Effect: write, Risk: medium},
		{Key: base + ".saved_views.delete", Pattern: http.MethodDelete + " " + prefix + "/saved-views/{viewKey}", Label: "Delete personal Inbox view", Effect: write, Risk: medium},
		{Key: base + ".delegations.list", Pattern: http.MethodGet + " " + prefix + "/delegations", Label: "List personal Inbox delegations", Effect: read, Risk: low},
		{Key: base + ".delegations.save", Pattern: http.MethodPut + " " + prefix + "/delegations/{delegationID}", Label: "Save personal Inbox delegation", Effect: write, Risk: medium},
		{Key: base + ".delegations.delete", Pattern: http.MethodDelete + " " + prefix + "/delegations/{delegationID}", Label: "Delete personal Inbox delegation", Effect: write, Risk: medium},
		{Key: base + ".delegated_owners.list", Pattern: http.MethodGet + " " + prefix + "/delegated-owners", Label: "List delegated Inbox owners", Effect: read, Risk: low},
		{Key: base + ".item.get", Pattern: http.MethodGet + " " + prefix + "/{notificationID}", Label: "Get Inbox notification", Effect: read, Risk: low},
		{Key: base + ".item.mark_read", Pattern: http.MethodPost + " " + prefix + "/{notificationID}/read", Label: "Mark Inbox notification read", Effect: write, Risk: low},
		{Key: base + ".item.mark_unread", Pattern: http.MethodPost + " " + prefix + "/{notificationID}/unread", Label: "Mark Inbox notification unread", Effect: write, Risk: low},
		{Key: base + ".item.archive", Pattern: http.MethodPost + " " + prefix + "/{notificationID}/archive", Label: "Archive Inbox notification", Effect: write, Risk: low},
		{Key: base + ".item.restore", Pattern: http.MethodPost + " " + prefix + "/{notificationID}/restore", Label: "Restore Inbox notification", Effect: write, Risk: low},
		{Key: base + ".item.acknowledge", Pattern: http.MethodPost + " " + prefix + "/{notificationID}/acknowledge", Label: "Acknowledge Inbox notification", Effect: write, Risk: medium},
		{Key: base + ".mark_all_read", Pattern: http.MethodPost + " " + prefix + "/read-all", Label: "Mark all Inbox notifications read", Effect: write, Risk: medium},
	}
}
