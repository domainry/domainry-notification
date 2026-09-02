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
// manifest. Management routes own one exact same-key Permission. Personal
// Inbox routes are authenticated-principal Actions whose source-owned domain
// service constrains every query and mutation to the resolved principal.
func AuthorizationActions() ([]actioncontract.ActionDefinition, error) {
	management := []notificationHTTPActionSpec{
		{Key: ActionCapabilitiesRead, Pattern: "GET /notifications/capabilities", Label: "Read notification capabilities", Effect: actioncontract.EffectRead, Risk: actioncontract.RiskLow},
		{Key: ActionTemplatesList, Pattern: "GET /notifications/templates", Label: "List notification templates", Effect: actioncontract.EffectRead, Risk: actioncontract.RiskLow},
		{Key: ActionTemplatesGet, Pattern: "GET /notifications/templates/{templateKey}", Label: "Get notification template", Effect: actioncontract.EffectRead, Risk: actioncontract.RiskLow},
		{Key: ActionPublicationsList, Pattern: "GET /notifications/publications", Label: "List publication requests", Effect: actioncontract.EffectRead, Risk: actioncontract.RiskLow},
		{Key: ActionPublicationsApprove, Pattern: "POST /notifications/publications/{publicationID}/approve", Label: "Approve publication request", Effect: actioncontract.EffectWrite, Risk: actioncontract.RiskHigh},
		{Key: ActionPublicationsReject, Pattern: "POST /notifications/publications/{publicationID}/reject", Label: "Reject publication request", Effect: actioncontract.EffectWrite, Risk: actioncontract.RiskHigh},
		{Key: ActionPublicationsCancel, Pattern: "POST /notifications/publications/{publicationID}/cancel", Label: "Cancel publication request", Effect: actioncontract.EffectWrite, Risk: actioncontract.RiskMedium},
		{Key: ActionTemplatesPreviewDraft, Pattern: "POST /notifications/templates/preview", Label: "Preview notification template draft", Effect: actioncontract.EffectRead, Risk: actioncontract.RiskLow},
		{Key: ActionTemplatesDraftSave, Pattern: "PUT /notifications/templates/{templateKey}/draft", Label: "Save notification template draft", Effect: actioncontract.EffectWrite, Risk: actioncontract.RiskMedium},
		{Key: ActionPublicationsRequest, Pattern: "POST /notifications/templates/{templateKey}/publication-requests", Label: "Request notification template publication", Effect: actioncontract.EffectWrite, Risk: actioncontract.RiskHigh},
		{Key: ActionTemplatesDisable, Pattern: "POST /notifications/templates/{templateKey}/disable", Label: "Disable notification template", Effect: actioncontract.EffectWrite, Risk: actioncontract.RiskHigh},
		{Key: ActionTemplatesPreview, Pattern: "POST /notifications/templates/{templateKey}/preview", Label: "Preview notification template", Effect: actioncontract.EffectRead, Risk: actioncontract.RiskLow},
		{Key: ActionTemplateVersionsList, Pattern: "GET /notifications/templates/{templateKey}/versions", Label: "List notification template versions", Effect: actioncontract.EffectRead, Risk: actioncontract.RiskLow},
		{Key: ActionTemplateVersionsRestoreDraft, Pattern: "POST /notifications/templates/{templateKey}/versions/{version}/restore-draft", Label: "Restore notification template draft", Effect: actioncontract.EffectWrite, Risk: actioncontract.RiskMedium},
		{Key: ActionDeliveryPolicyGet, Pattern: "GET /notifications/policy", Label: "Get notification delivery policy", Effect: actioncontract.EffectRead, Risk: actioncontract.RiskLow},
		{Key: ActionDeliveryPolicyUpdate, Pattern: "PUT /notifications/policy", Label: "Update notification delivery policy", Effect: actioncontract.EffectWrite, Risk: actioncontract.RiskHigh},
		{Key: ActionRecipientPreferencesList, Pattern: "GET /notifications/preferences", Label: "List recipient preferences", Effect: actioncontract.EffectRead, Risk: actioncontract.RiskLow},
		{Key: ActionRecipientPreferencesUpdate, Pattern: "PUT /notifications/preferences/{recipientKey}", Label: "Update recipient preference", Effect: actioncontract.EffectWrite, Risk: actioncontract.RiskMedium},
		{Key: ActionDeliveryMetricsRead, Pattern: "GET /notifications/metrics", Label: "Read notification delivery metrics", Effect: actioncontract.EffectRead, Risk: actioncontract.RiskLow},
		{Key: ActionGovernanceCatalogRead, Pattern: "GET /notifications/governance/catalog", Label: "Read notification governance catalog", Effect: actioncontract.EffectRead, Risk: actioncontract.RiskLow},
		{Key: ActionGovernanceInboxMetricsRead, Pattern: "GET /notifications/governance/inbox-metrics", Label: "Read Inbox governance metrics", Effect: actioncontract.EffectRead, Risk: actioncontract.RiskLow},
	}
	definitions := make([]actioncontract.ActionDefinition, 0, len(management)+40)
	for _, spec := range management {
		definition, err := notificationHTTPAction(spec, true, "notification.management", "Notification management", actioncontract.ExposureTenantAdmin)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, definition)
	}
	for _, surface := range []struct {
		key, label, prefix string
	}{
		{key: "business_inbox", label: "Business notification Inbox", prefix: "/business"},
		{key: "portal_inbox", label: "Portal notification Inbox", prefix: "/portal"},
	} {
		for _, spec := range notificationInboxActionSpecs(surface.key, surface.prefix) {
			definition, err := notificationHTTPAction(spec, false, "notification."+surface.key, surface.label, actioncontract.ExposurePublic)
			if err != nil {
				return nil, err
			}
			definitions = append(definitions, definition)
		}
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
		Key: spec.Key, Owner: NotificationAuthorizationOwner, SourceKind: "module_surface",
		CapabilityKey: capabilityKey, CapabilityLabel: capabilityLabel,
		OperationKey: operationKey, OperationLabel: spec.Label, Label: spec.Label,
		Exposures: []actioncontract.Exposure{exposure}, HTTP: &actioncontract.HTTPBinding{Method: method, RouteTemplate: route},
		EffectClass: spec.Effect, RiskLevel: spec.Risk, IdempotencyDecision: idempotency,
		AuditClass: "notification_http", LifecycleStatus: actioncontract.LifecycleActive,
	}
	if permission {
		definition.Authorization = actioncontract.Authorization{Strategy: actioncontract.AuthorizationExactRolePermission}
		definition.Permission = &actioncontract.PermissionDefinition{
			Key: spec.Key, Owner: NotificationAuthorizationOwner, ResourceKey: resourceKey, OperationKey: operationKey,
			Label: spec.Label, Category: capabilityLabel, LifecycleStatus: actioncontract.LifecycleActive,
		}
	} else {
		definition.Authorization = actioncontract.Authorization{Strategy: actioncontract.AuthorizationAuthenticatedPrincipal}
	}
	definition, err := actioncontract.NormalizeDefinition(definition)
	if err != nil {
		return actioncontract.ActionDefinition{}, fmt.Errorf("Notification Action %q: %w", spec.Key, err)
	}
	return definition, nil
}

func notificationInboxActionSpecs(surfaceKey, prefix string) []notificationHTTPActionSpec {
	read := actioncontract.EffectRead
	write := actioncontract.EffectWrite
	low, medium := actioncontract.RiskLow, actioncontract.RiskMedium
	base := "notification." + surfaceKey
	return []notificationHTTPActionSpec{
		{Key: base + ".list", Pattern: http.MethodGet + " " + prefix + "/notifications", Label: "List Inbox notifications", Effect: read, Risk: low},
		{Key: base + ".facets", Pattern: http.MethodGet + " " + prefix + "/notifications/facets", Label: "Read Inbox facets", Effect: read, Risk: low},
		{Key: base + ".unread_count", Pattern: http.MethodGet + " " + prefix + "/notifications/unread-count", Label: "Read Inbox unread count", Effect: read, Risk: low},
		{Key: base + ".stream", Pattern: http.MethodGet + " " + prefix + "/notifications/stream", Label: "Open Inbox synchronization stream", Effect: read, Risk: low},
		{Key: base + ".preference.get", Pattern: http.MethodGet + " " + prefix + "/notification-preferences", Label: "Get personal notification preference", Effect: read, Risk: low},
		{Key: base + ".preference.update", Pattern: http.MethodPut + " " + prefix + "/notification-preferences", Label: "Update personal notification preference", Effect: write, Risk: medium},
		{Key: base + ".saved_views.list", Pattern: http.MethodGet + " " + prefix + "/notifications/saved-views", Label: "List personal Inbox saved views", Effect: read, Risk: low},
		{Key: base + ".saved_views.save", Pattern: http.MethodPut + " " + prefix + "/notifications/saved-views/{viewKey}", Label: "Save personal Inbox view", Effect: write, Risk: medium},
		{Key: base + ".saved_views.delete", Pattern: http.MethodDelete + " " + prefix + "/notifications/saved-views/{viewKey}", Label: "Delete personal Inbox view", Effect: write, Risk: medium},
		{Key: base + ".delegations.list", Pattern: http.MethodGet + " " + prefix + "/notifications/delegations", Label: "List personal Inbox delegations", Effect: read, Risk: low},
		{Key: base + ".delegations.save", Pattern: http.MethodPut + " " + prefix + "/notifications/delegations/{delegationID}", Label: "Save personal Inbox delegation", Effect: write, Risk: medium},
		{Key: base + ".delegations.delete", Pattern: http.MethodDelete + " " + prefix + "/notifications/delegations/{delegationID}", Label: "Delete personal Inbox delegation", Effect: write, Risk: medium},
		{Key: base + ".delegated_owners.list", Pattern: http.MethodGet + " " + prefix + "/notifications/delegated-owners", Label: "List delegated Inbox owners", Effect: read, Risk: low},
		{Key: base + ".item.get", Pattern: http.MethodGet + " " + prefix + "/notifications/{notificationID}", Label: "Get Inbox notification", Effect: read, Risk: low},
		{Key: base + ".item.mark_read", Pattern: http.MethodPost + " " + prefix + "/notifications/{notificationID}/read", Label: "Mark Inbox notification read", Effect: write, Risk: low},
		{Key: base + ".item.mark_unread", Pattern: http.MethodPost + " " + prefix + "/notifications/{notificationID}/unread", Label: "Mark Inbox notification unread", Effect: write, Risk: low},
		{Key: base + ".item.archive", Pattern: http.MethodPost + " " + prefix + "/notifications/{notificationID}/archive", Label: "Archive Inbox notification", Effect: write, Risk: low},
		{Key: base + ".item.restore", Pattern: http.MethodPost + " " + prefix + "/notifications/{notificationID}/restore", Label: "Restore Inbox notification", Effect: write, Risk: low},
		{Key: base + ".item.acknowledge", Pattern: http.MethodPost + " " + prefix + "/notifications/{notificationID}/acknowledge", Label: "Acknowledge Inbox notification", Effect: write, Risk: medium},
		{Key: base + ".mark_all_read", Pattern: http.MethodPost + " " + prefix + "/notifications/read-all", Label: "Mark all Inbox notifications read", Effect: write, Risk: medium},
	}
}
