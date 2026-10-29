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
	ActionGovernanceDeadLettersList    = "notification.governance.dead_letters.list"
	ActionGovernanceDeadLettersRedrive = "notification.governance.dead_letters.redrive"
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
		{Key: ActionGovernanceDeadLettersList, Pattern: "GET /notification/governance/dead-letters", Label: "List notification dead letters", Effect: actioncontract.EffectRead, Risk: actioncontract.RiskMedium},
		{Key: ActionGovernanceDeadLettersRedrive, Pattern: "POST /notification/governance/dead-letters/{eventID}/redrive", Label: "Redrive notification dead letter", Effect: actioncontract.EffectWrite, Risk: actioncontract.RiskHigh},
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
			Key: spec.Key, Owner: NotificationAuthorizationOwner, ModuleKey: "notification", GroupKey: capabilityKey,
			ResourceKey: resourceKey, OperationKey: operationKey, Label: spec.Label, Category: capabilityLabel,
			GrantMode: actioncontract.PermissionGrantInternal, ScopeKind: actioncontract.PermissionScopeNone,
			DefaultLocale: "en", I18n: notificationPermissionText(capabilityKey, resourceKey, operationKey, spec.Label),
			LifecycleStatus: actioncontract.LifecycleActive,
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

func notificationPermissionText(capabilityKey, resourceKey, operationKey, englishLabel string) actioncontract.PermissionLocalizedText {
	groupEnglish := map[string]string{"notification.management": "Notification management", "notification.inbox": "Personal Inbox"}[capabilityKey]
	groupChinese := map[string]string{"notification.management": "通知管理", "notification.inbox": "个人收件箱"}[capabilityKey]
	resourceEnglish := map[string]string{
		"notification.capabilities": "Notification capabilities", "notification.templates": "Notification templates",
		"notification.publications": "Publication requests", "notification.templates.draft": "Template drafts",
		"notification.templates.versions": "Template versions", "notification.delivery_policy": "Delivery policy",
		"notification.recipient_preferences": "Recipient preferences", "notification.delivery_metrics": "Delivery metrics",
		"notification.governance.catalog": "Governance catalog", "notification.governance.inbox_metrics": "Inbox governance metrics",
		"notification.governance.dead_letters": "Failed notification events", "notification.inbox": "Inbox notifications",
		"notification.inbox.preference": "Personal notification preference", "notification.inbox.saved_views": "Saved Inbox views",
		"notification.inbox.delegations": "Inbox delegations", "notification.inbox.delegated_owners": "Delegated Inbox owners",
		"notification.inbox.item": "Inbox notification",
	}[resourceKey]
	resourceChinese := map[string]string{
		"notification.capabilities": "通知能力", "notification.templates": "通知模板",
		"notification.publications": "发布申请", "notification.templates.draft": "模板草稿",
		"notification.templates.versions": "模板版本", "notification.delivery_policy": "投递策略",
		"notification.recipient_preferences": "收件人偏好", "notification.delivery_metrics": "投递指标",
		"notification.governance.catalog": "治理目录", "notification.governance.inbox_metrics": "收件箱治理指标",
		"notification.governance.dead_letters": "失败通知事件", "notification.inbox": "收件箱通知",
		"notification.inbox.preference": "个人通知偏好", "notification.inbox.saved_views": "收件箱保存视图",
		"notification.inbox.delegations": "收件箱委派", "notification.inbox.delegated_owners": "受委派的收件箱所有者",
		"notification.inbox.item": "收件箱通知",
	}[resourceKey]
	operationEnglish := map[string]string{
		"read": "View", "list": "List", "get": "View details", "approve": "Approve", "reject": "Reject", "cancel": "Cancel",
		"preview_draft": "Preview draft", "save": "Save", "request": "Request publication", "disable": "Disable", "preview": "Preview",
		"restore_draft": "Restore as draft", "update": "Update", "redrive": "Redeliver", "facets": "View filters",
		"unread_count": "View unread count", "stream": "Synchronize", "delete": "Delete", "mark_read": "Mark read",
		"mark_unread": "Mark unread", "archive": "Archive", "restore": "Restore", "acknowledge": "Acknowledge",
		"mark_all_read": "Mark all read",
	}[operationKey]
	operationChinese := map[string]string{
		"read": "查看", "list": "查看列表", "get": "查看详情", "approve": "批准", "reject": "拒绝", "cancel": "取消",
		"preview_draft": "预览草稿", "save": "保存", "request": "申请发布", "disable": "停用", "preview": "预览",
		"restore_draft": "恢复为草稿", "update": "更新", "redrive": "重新投递", "facets": "查看筛选项",
		"unread_count": "查看未读数", "stream": "同步", "delete": "删除", "mark_read": "标为已读",
		"mark_unread": "标为未读", "archive": "归档", "restore": "恢复", "acknowledge": "确认收到",
		"mark_all_read": "全部标为已读",
	}[operationKey]
	return actioncontract.PermissionLocalizedText{
		"en":    {ModuleLabel: "Notifications", GroupLabel: groupEnglish, ResourceLabel: resourceEnglish, OperationLabel: operationEnglish, Label: englishLabel, Description: "Provides access to " + resourceEnglish + " for the “" + operationEnglish + "” operation."},
		"zh-CN": {ModuleLabel: "通知", GroupLabel: groupChinese, ResourceLabel: resourceChinese, OperationLabel: operationChinese, Label: resourceChinese + " · " + operationChinese, Description: "允许对“" + resourceChinese + "”执行“" + operationChinese + "”操作。"},
	}
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
