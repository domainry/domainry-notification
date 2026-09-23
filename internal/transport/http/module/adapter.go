package module

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/domainry/domainry-foundation/modulehttp"
	identitysdk "github.com/domainry/domainry-identity-sdk"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/contract"
	notificationapplication "github.com/domainry/domainry-notification/internal/application"
)

type adapter struct {
	binding notificationsdk.Binding
	mux     *http.ServeMux
	routes  []modulehttp.Route
}

func (*adapter) ContractVersion() string { return modulehttp.ContractVersion }
func (*adapter) Owner() string           { return "notification" }
func (*adapter) Name() string            { return "template_management" }
func (s *adapter) Handler() http.Handler { return s.mux }
func (s *adapter) Routes() []modulehttp.Route {
	return append([]modulehttp.Route(nil), s.routes...)
}

func NewAdapter(binding notificationsdk.Binding) (modulehttp.Adapter, error) {
	if binding == nil || binding.Templates() == nil {
		return nil, errors.New("Notification template binding is unavailable")
	}
	routes, err := notificationRoutes()
	if err != nil {
		return nil, err
	}
	s := &adapter{binding: binding, mux: http.NewServeMux(), routes: routes}
	handlers := s.actionHandlers()
	for key, handler := range s.inboxActionHandlers() {
		handlers[key] = handler
	}
	for _, route := range routes {
		key := strings.TrimSpace(route.Action.Key)
		handler, found := handlers[key]
		if !found {
			return nil, fmt.Errorf("Notification Action %q has no HTTP handler", key)
		}
		exactKey := key
		s.mux.HandleFunc(route.Pattern(), func(w http.ResponseWriter, r *http.Request) {
			handler(w, r.WithContext(notificationapplication.WithExactAction(r.Context(), exactKey)))
		})
		delete(handlers, key)
	}
	if len(handlers) != 0 {
		keys := make([]string, 0, len(handlers))
		for key := range handlers {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("Notification HTTP handlers have no Action manifest entries: %v", keys)
	}
	return s, nil
}

func (s *adapter) actionHandlers() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		notificationapplication.ActionCapabilitiesRead:             s.capabilities,
		notificationapplication.ActionTemplatesList:                s.list,
		notificationapplication.ActionTemplatesGet:                 s.get,
		notificationapplication.ActionPublicationsList:             s.listPublications,
		notificationapplication.ActionPublicationsApprove:          s.approvePublication,
		notificationapplication.ActionPublicationsReject:           s.rejectPublication,
		notificationapplication.ActionPublicationsCancel:           s.cancelPublication,
		notificationapplication.ActionTemplatesPreviewDraft:        s.previewDraft,
		notificationapplication.ActionTemplatesDraftSave:           s.saveDraft,
		notificationapplication.ActionPublicationsRequest:          s.requestPublication,
		notificationapplication.ActionTemplatesDisable:             s.disable,
		notificationapplication.ActionTemplatesPreview:             s.preview,
		notificationapplication.ActionTemplateVersionsList:         s.listVersions,
		notificationapplication.ActionTemplateVersionsRestoreDraft: s.restoreVersion,
		notificationapplication.ActionDeliveryPolicyGet:            s.getPolicy,
		notificationapplication.ActionDeliveryPolicyUpdate:         s.savePolicy,
		notificationapplication.ActionRecipientPreferencesList:     s.listPreferences,
		notificationapplication.ActionRecipientPreferencesUpdate:   s.savePreference,
		notificationapplication.ActionDeliveryMetricsRead:          s.metrics,
		notificationapplication.ActionGovernanceCatalogRead:        s.governanceCatalog,
		notificationapplication.ActionGovernanceInboxMetricsRead:   s.inboxGovernanceMetrics,
	}
}

func authority(r *http.Request) (notificationsdk.UserAuthority, error) {
	identity, ok := identitysdk.RequestIdentityFromContext(r.Context())
	if !ok || strings.TrimSpace(identity.AccessToken) == "" {
		return notificationsdk.UserAuthority{}, &notificationsdk.Error{StatusCode: http.StatusUnauthorized, Code: "notification.user_authority_required"}
	}
	return notificationsdk.UserAuthority{AccessToken: identity.AccessToken}, nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "notification.internal"
	var sdkError *notificationsdk.Error
	if errors.As(err, &sdkError) {
		if sdkError.StatusCode > 0 {
			status = sdkError.StatusCode
		}
		if strings.TrimSpace(sdkError.Code) != "" {
			code = sdkError.Code
		}
	}
	writeJSON(w, status, map[string]string{"code": "backend." + strings.TrimPrefix(code, "backend.")})
}

func (s *adapter) capabilities(w http.ResponseWriter, r *http.Request) {
	a, err := authority(r)
	if err != nil {
		writeError(w, err)
		return
	}
	values, err := s.binding.Templates().Capabilities(r.Context(), a)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"capabilities": values, "count": len(values)})
}

func (s *adapter) list(w http.ResponseWriter, r *http.Request) {
	a, err := authority(r)
	if err != nil {
		writeError(w, err)
		return
	}
	values, err := s.binding.Templates().List(r.Context(), a)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": values, "count": len(values)})
}

func (s *adapter) get(w http.ResponseWriter, r *http.Request) {
	a, err := authority(r)
	if err != nil {
		writeError(w, err)
		return
	}
	value, found, err := s.binding.Templates().Get(r.Context(), a, strings.TrimSpace(r.PathValue("templateKey")))
	if err != nil {
		writeError(w, err)
		return
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"code": "backend.notification.template_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "backend.request.invalid_json"})
		return false
	}
	return true
}
func withAuthority(w http.ResponseWriter, r *http.Request) (notificationsdk.UserAuthority, bool) {
	a, err := authority(r)
	if err != nil {
		writeError(w, err)
		return a, false
	}
	return a, true
}
func defaultRecipients(values []string) []string {
	if len(values) == 0 {
		return []string{"preview@example.com"}
	}
	return values
}

func (s *adapter) listPublications(w http.ResponseWriter, r *http.Request) {
	a, ok := withAuthority(w, r)
	if !ok {
		return
	}
	values, err := s.binding.Templates().ListPublicationRequests(r.Context(), a, r.URL.Query().Get("template_key"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"publications": values, "count": len(values)})
}
func (s *adapter) approvePublication(w http.ResponseWriter, r *http.Request) {
	a, ok := withAuthority(w, r)
	if !ok {
		return
	}
	value, err := s.binding.Templates().ApprovePublication(r.Context(), a, r.PathValue("publicationID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (s *adapter) rejectPublication(w http.ResponseWriter, r *http.Request) {
	a, ok := withAuthority(w, r)
	if !ok {
		return
	}
	var input templateReviewInput
	if !decode(w, r, &input) {
		return
	}
	value, err := s.binding.Templates().RejectPublication(r.Context(), a, r.PathValue("publicationID"), input.Reason)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (s *adapter) cancelPublication(w http.ResponseWriter, r *http.Request) {
	a, ok := withAuthority(w, r)
	if !ok {
		return
	}
	value, err := s.binding.Templates().CancelPublication(r.Context(), a, r.PathValue("publicationID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (s *adapter) previewDraft(w http.ResponseWriter, r *http.Request) {
	a, ok := withAuthority(w, r)
	if !ok {
		return
	}
	var input templatePreviewDraftInput
	if !decode(w, r, &input) {
		return
	}
	value, err := s.binding.Templates().PreviewTemplate(r.Context(), a, input.Template, input.Locale, defaultRecipients(input.Recipients), input.Variables)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (s *adapter) saveDraft(w http.ResponseWriter, r *http.Request) {
	a, ok := withAuthority(w, r)
	if !ok {
		return
	}
	var input templateDraftInput
	if !decode(w, r, &input) {
		return
	}
	value, err := s.binding.Templates().SaveDraft(r.Context(), a, r.PathValue("templateKey"), input.Template, input.ExpectedUpdatedAt)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (s *adapter) requestPublication(w http.ResponseWriter, r *http.Request) {
	a, ok := withAuthority(w, r)
	if !ok {
		return
	}
	var input templatePublicationInput
	if !decode(w, r, &input) {
		return
	}
	value, err := s.binding.Templates().RequestPublication(r.Context(), a, r.PathValue("templateKey"), input.ScheduledFor, input.ExpectedUpdatedAt)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}
func (s *adapter) disable(w http.ResponseWriter, r *http.Request) {
	a, ok := withAuthority(w, r)
	if !ok {
		return
	}
	var input templateLifecycleInput
	if !decode(w, r, &input) {
		return
	}
	value, err := s.binding.Templates().Disable(r.Context(), a, r.PathValue("templateKey"), input.ExpectedUpdatedAt)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (s *adapter) preview(w http.ResponseWriter, r *http.Request) {
	a, ok := withAuthority(w, r)
	if !ok {
		return
	}
	var input templatePreviewInput
	if !decode(w, r, &input) {
		return
	}
	value, err := s.binding.Templates().Preview(r.Context(), a, r.PathValue("templateKey"), input.Locale, defaultRecipients(input.Recipients), input.Variables)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (s *adapter) listVersions(w http.ResponseWriter, r *http.Request) {
	a, ok := withAuthority(w, r)
	if !ok {
		return
	}
	values, err := s.binding.Templates().ListVersions(r.Context(), a, r.PathValue("templateKey"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": values, "count": len(values)})
}
func (s *adapter) restoreVersion(w http.ResponseWriter, r *http.Request) {
	a, ok := withAuthority(w, r)
	if !ok {
		return
	}
	var input templateLifecycleInput
	if !decode(w, r, &input) {
		return
	}
	version, err := strconv.Atoi(strings.TrimSpace(r.PathValue("version")))
	if err != nil || version < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "backend.notification.template_version_invalid"})
		return
	}
	value, err := s.binding.Templates().RestoreVersionDraft(r.Context(), a, r.PathValue("templateKey"), version, input.ExpectedUpdatedAt)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func metricSince(r *http.Request) string {
	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if hours <= 0 {
		hours = 24
	}
	if hours > 720 {
		hours = 720
	}
	return time.Now().UTC().Add(-time.Duration(hours) * time.Hour).Format(time.RFC3339)
}
func (s *adapter) getPolicy(w http.ResponseWriter, r *http.Request) {
	a, ok := withAuthority(w, r)
	if !ok {
		return
	}
	value, err := s.binding.Delivery().GetPolicy(r.Context(), a)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (s *adapter) savePolicy(w http.ResponseWriter, r *http.Request) {
	a, ok := withAuthority(w, r)
	if !ok {
		return
	}
	var input contract.NotificationDeliveryPolicy
	if !decode(w, r, &input) {
		return
	}
	value, err := s.binding.Delivery().SavePolicy(r.Context(), a, input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (s *adapter) listPreferences(w http.ResponseWriter, r *http.Request) {
	a, ok := withAuthority(w, r)
	if !ok {
		return
	}
	values, err := s.binding.Delivery().ListRecipientPreferences(r.Context(), a)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"preferences": values, "count": len(values)})
}
func (s *adapter) savePreference(w http.ResponseWriter, r *http.Request) {
	a, ok := withAuthority(w, r)
	if !ok {
		return
	}
	var input contract.NotificationRecipientPreference
	if !decode(w, r, &input) {
		return
	}
	input.RecipientKey = strings.TrimSpace(r.PathValue("recipientKey"))
	value, err := s.binding.Delivery().SaveRecipientPreference(r.Context(), a, input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (s *adapter) metrics(w http.ResponseWriter, r *http.Request) {
	a, ok := withAuthority(w, r)
	if !ok {
		return
	}
	value, err := s.binding.Delivery().Metrics(r.Context(), a, metricSince(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (s *adapter) governanceCatalog(w http.ResponseWriter, r *http.Request) {
	a, ok := withAuthority(w, r)
	if !ok {
		return
	}
	value, err := s.binding.Administration().GovernanceCatalog(r.Context(), a)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (s *adapter) inboxGovernanceMetrics(w http.ResponseWriter, r *http.Request) {
	a, ok := withAuthority(w, r)
	if !ok {
		return
	}
	value, err := s.binding.Administration().InboxGovernanceMetrics(r.Context(), a, metricSince(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

var _ modulehttp.Adapter = (*adapter)(nil)
