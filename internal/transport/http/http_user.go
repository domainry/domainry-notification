package notificationhttp

import (
	"net/http"
	"strings"

	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/contract"
)

type inboxRequest struct {
	Query      contract.NotificationInboxQuery           `json:"query"`
	Cursor     string                                    `json:"cursor,omitempty"`
	ID         string                                    `json:"id,omitempty"`
	ActionKey  string                                    `json:"action_key,omitempty"`
	Value      bool                                      `json:"value,omitempty"`
	Surface    string                                    `json:"surface,omitempty"`
	Key        string                                    `json:"key,omitempty"`
	Delegation *contract.NotificationInboxDelegation     `json:"delegation,omitempty"`
	SavedView  *contract.NotificationInboxSavedView      `json:"saved_view,omitempty"`
	Preference *contract.NotificationRecipientPreference `json:"preference,omitempty"`
}

type templateRequest struct {
	Key        string                         `json:"key,omitempty"`
	Version    int                            `json:"version,omitempty"`
	Expected   string                         `json:"expected,omitempty"`
	Locale     string                         `json:"locale,omitempty"`
	Recipients []string                       `json:"recipients,omitempty"`
	Variables  map[string]any                 `json:"variables,omitempty"`
	Template   *contract.NotificationTemplate `json:"template,omitempty"`
	Scheduled  string                         `json:"scheduled,omitempty"`
	ID         string                         `json:"id,omitempty"`
	Reason     string                         `json:"reason,omitempty"`
}

type deliveryRequest struct {
	Since  string                               `json:"since,omitempty"`
	Policy *contract.NotificationDeliveryPolicy `json:"policy,omitempty"`
}

func serveBindingRoute(response http.ResponseWriter, request *http.Request, binding notificationsdk.Binding) bool {
	if request.Method != http.MethodPost {
		return false
	}
	authority, err := userAuthority(request)
	if err != nil {
		writeHTTPError(response, err)
		return true
	}
	ctx := request.Context()
	switch request.URL.Path {
	case "/v1/inbox:list", "/v1/inbox:get", "/v1/inbox:facets", "/v1/inbox:set-read", "/v1/inbox:set-archived", "/v1/inbox:acknowledge", "/v1/inbox:mark-all-read", "/v1/inbox:resolve-action", "/v1/inbox/delegations:list", "/v1/inbox/delegations:save", "/v1/inbox/delegations:delete", "/v1/inbox/delegated-owners:list", "/v1/inbox/saved-views:list", "/v1/inbox/saved-views:save", "/v1/inbox/saved-views:delete", "/v1/inbox/preference:get", "/v1/inbox/preference:save":
		var input inboxRequest
		if err := decodeJSON(request.Body, &input); err != nil {
			writeHTTPError(response, err)
			return true
		}
		var output any
		switch request.URL.Path {
		case "/v1/inbox:list":
			output, err = binding.Inbox().List(ctx, authority, input.Query, input.Cursor)
		case "/v1/inbox:get":
			output, err = binding.Inbox().Get(ctx, authority, input.ID, input.Query)
		case "/v1/inbox:facets":
			output, err = binding.Inbox().Facets(ctx, authority, input.Query)
		case "/v1/inbox:set-read":
			output, err = binding.Inbox().SetRead(ctx, authority, input.ID, input.Value)
		case "/v1/inbox:set-archived":
			output, err = binding.Inbox().SetArchived(ctx, authority, input.ID, input.Value)
		case "/v1/inbox:acknowledge":
			output, err = binding.Inbox().AcknowledgeAlert(ctx, authority, input.ID)
		case "/v1/inbox:mark-all-read":
			var count int
			count, err = binding.Inbox().MarkAllRead(ctx, authority, input.Query)
			output = struct {
				Count int `json:"count"`
			}{count}
		case "/v1/inbox:resolve-action":
			output, err = binding.Inbox().ResolveAction(ctx, authority, input.ID, input.ActionKey, input.Query)
		case "/v1/inbox/delegations:list":
			output, err = binding.Inbox().ListDelegations(ctx, authority, input.Surface)
		case "/v1/inbox/delegations:save":
			if input.Delegation == nil {
				err = invalidRequest()
			} else {
				output, err = binding.Inbox().SaveDelegation(ctx, authority, *input.Delegation)
			}
		case "/v1/inbox/delegations:delete":
			err = binding.Inbox().DeleteDelegation(ctx, authority, input.ID)
		case "/v1/inbox/delegated-owners:list":
			output, err = binding.Inbox().ListDelegatedOwnerIDs(ctx, authority, input.Surface)
		case "/v1/inbox/saved-views:list":
			output, err = binding.Inbox().ListSavedViews(ctx, authority, input.Surface)
		case "/v1/inbox/saved-views:save":
			if input.SavedView == nil {
				err = invalidRequest()
			} else {
				output, err = binding.Inbox().SaveSavedView(ctx, authority, *input.SavedView)
			}
		case "/v1/inbox/saved-views:delete":
			err = binding.Inbox().DeleteSavedView(ctx, authority, input.Key)
		case "/v1/inbox/preference:get":
			output, err = binding.Inbox().GetPreference(ctx, authority, input.Surface)
		case "/v1/inbox/preference:save":
			if input.Preference == nil {
				err = invalidRequest()
			} else {
				output, err = binding.Inbox().SavePreference(ctx, authority, input.Surface, *input.Preference)
			}
		}
		return writeRouteResult(response, output, err)
	case "/v1/templates/capabilities:list", "/v1/templates:list", "/v1/templates:get", "/v1/templates/versions:list", "/v1/templates:save-draft", "/v1/templates:restore-version", "/v1/templates:disable", "/v1/templates:preview", "/v1/templates:preview-draft", "/v1/publications:list", "/v1/publications:request", "/v1/publications:approve", "/v1/publications:reject", "/v1/publications:cancel":
		var input templateRequest
		if request.ContentLength != 0 {
			if err := decodeJSON(request.Body, &input); err != nil {
				writeHTTPError(response, err)
				return true
			}
		}
		var output any
		switch request.URL.Path {
		case "/v1/templates/capabilities:list":
			output, err = binding.Templates().Capabilities(ctx, authority)
		case "/v1/templates:list":
			output, err = binding.Templates().List(ctx, authority)
		case "/v1/templates:get":
			var record contract.NotificationTemplateRecord
			var found bool
			record, found, err = binding.Templates().Get(ctx, authority, input.Key)
			output = struct {
				Record contract.NotificationTemplateRecord `json:"record"`
				Found  bool                                `json:"found"`
			}{record, found}
		case "/v1/templates/versions:list":
			output, err = binding.Templates().ListVersions(ctx, authority, input.Key)
		case "/v1/templates:save-draft":
			if input.Template == nil {
				err = invalidRequest()
			} else {
				output, err = binding.Templates().SaveDraft(ctx, authority, input.Key, *input.Template, input.Expected)
			}
		case "/v1/templates:restore-version":
			output, err = binding.Templates().RestoreVersionDraft(ctx, authority, input.Key, input.Version, input.Expected)
		case "/v1/templates:disable":
			output, err = binding.Templates().Disable(ctx, authority, input.Key, input.Expected)
		case "/v1/templates:preview":
			output, err = binding.Templates().Preview(ctx, authority, input.Key, input.Locale, input.Recipients, input.Variables)
		case "/v1/templates:preview-draft":
			if input.Template == nil {
				err = invalidRequest()
			} else {
				output, err = binding.Templates().PreviewTemplate(ctx, authority, *input.Template, input.Locale, input.Recipients, input.Variables)
			}
		case "/v1/publications:list":
			output, err = binding.Templates().ListPublicationRequests(ctx, authority, input.Key)
		case "/v1/publications:request":
			output, err = binding.Templates().RequestPublication(ctx, authority, input.Key, input.Scheduled, input.Expected)
		case "/v1/publications:approve":
			output, err = binding.Templates().ApprovePublication(ctx, authority, input.ID)
		case "/v1/publications:reject":
			output, err = binding.Templates().RejectPublication(ctx, authority, input.ID, input.Reason)
		case "/v1/publications:cancel":
			output, err = binding.Templates().CancelPublication(ctx, authority, input.ID)
		}
		return writeRouteResult(response, output, err)
	case "/v1/delivery-policy:get", "/v1/delivery-policy:save", "/v1/recipient-preferences:list", "/v1/recipient-preferences:save", "/v1/delivery-metrics:get":
		var deliveryInput deliveryRequest
		var inboxInput inboxRequest
		if request.ContentLength != 0 {
			if request.URL.Path == "/v1/recipient-preferences:save" {
				err = decodeJSON(request.Body, &inboxInput)
			} else {
				err = decodeJSON(request.Body, &deliveryInput)
			}
			if err != nil {
				writeHTTPError(response, err)
				return true
			}
		}
		var output any
		switch request.URL.Path {
		case "/v1/delivery-policy:get":
			output, err = binding.Delivery().GetPolicy(ctx, authority)
		case "/v1/delivery-policy:save":
			if deliveryInput.Policy == nil {
				err = invalidRequest()
			} else {
				output, err = binding.Delivery().SavePolicy(ctx, authority, *deliveryInput.Policy)
			}
		case "/v1/recipient-preferences:list":
			output, err = binding.Delivery().ListRecipientPreferences(ctx, authority)
		case "/v1/recipient-preferences:save":
			if inboxInput.Preference == nil {
				err = invalidRequest()
			} else {
				output, err = binding.Delivery().SaveRecipientPreference(ctx, authority, *inboxInput.Preference)
			}
		case "/v1/delivery-metrics:get":
			output, err = binding.Delivery().Metrics(ctx, authority, deliveryInput.Since)
		}
		return writeRouteResult(response, output, err)
	case "/v1/governance/catalog:get", "/v1/governance/inbox-metrics:get":
		var input struct {
			Since string `json:"since,omitempty"`
		}
		if request.ContentLength != 0 {
			if err := decodeJSON(request.Body, &input); err != nil {
				writeHTTPError(response, err)
				return true
			}
		}
		var output any
		if request.URL.Path == "/v1/governance/catalog:get" {
			output, err = binding.Administration().GovernanceCatalog(ctx, authority)
		} else {
			output, err = binding.Administration().InboxGovernanceMetrics(ctx, authority, input.Since)
		}
		return writeRouteResult(response, output, err)
	default:
		return false
	}
}

func userAuthority(request *http.Request) (notificationsdk.UserAuthority, error) {
	value := strings.TrimSpace(request.Header.Get("Authorization"))
	if len(value) < 8 || !strings.EqualFold(value[:7], "Bearer ") || strings.TrimSpace(value[7:]) == "" {
		return notificationsdk.UserAuthority{}, &notificationsdk.Error{StatusCode: http.StatusUnauthorized, Code: "notification.user_authority_required"}
	}
	authority := notificationsdk.UserAuthority{AccessToken: strings.TrimSpace(value[7:]), Surface: strings.TrimSpace(request.Header.Get("X-Domainry-Product-Surface"))}
	if err := authority.Validate(); err != nil {
		return notificationsdk.UserAuthority{}, err
	}
	return authority, nil
}

func writeRouteResult(response http.ResponseWriter, output any, err error) bool {
	if err != nil {
		writeHTTPError(response, err)
		return true
	}
	if output == nil {
		response.WriteHeader(http.StatusNoContent)
		return true
	}
	writeJSON(response, http.StatusOK, output)
	return true
}

func invalidRequest() error {
	return &notificationsdk.Error{StatusCode: http.StatusBadRequest, Code: "notification.request_invalid"}
}
