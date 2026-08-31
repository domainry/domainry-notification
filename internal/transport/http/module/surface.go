package module

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/domainry/domainry-foundation/modulehttp"
	identitysdk "github.com/domainry/domainry-identity-sdk"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/contract"
)

type surface struct {
	binding notificationsdk.Binding
	mux     *http.ServeMux
	routes  []modulehttp.Route
}

func (*surface) ContractVersion() string { return modulehttp.ContractVersion }
func (*surface) Owner() string           { return "notification" }
func (*surface) Name() string            { return "template_management" }
func (s *surface) Handler() http.Handler { return s.mux }
func (s *surface) Routes() []modulehttp.Route {
	return append([]modulehttp.Route(nil), s.routes...)
}

func NewSurface(binding notificationsdk.Binding) (modulehttp.Surface, error) {
	if binding == nil || binding.Templates() == nil {
		return nil, errors.New("Notification template binding is unavailable")
	}
	s := &surface{binding: binding, mux: http.NewServeMux()}
	s.routes = []modulehttp.Route{
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
	s.mux.HandleFunc("GET /notifications/capabilities", s.capabilities)
	s.mux.HandleFunc("GET /notifications/templates", s.list)
	s.mux.HandleFunc("GET /notifications/templates/{templateKey}", s.get)
	s.mux.HandleFunc("GET /notifications/publications", s.listPublications)
	s.mux.HandleFunc("POST /notifications/publications/{publicationID}/approve", s.approvePublication)
	s.mux.HandleFunc("POST /notifications/publications/{publicationID}/reject", s.rejectPublication)
	s.mux.HandleFunc("POST /notifications/publications/{publicationID}/cancel", s.cancelPublication)
	s.mux.HandleFunc("POST /notifications/templates/preview", s.previewDraft)
	s.mux.HandleFunc("PUT /notifications/templates/{templateKey}/draft", s.saveDraft)
	s.mux.HandleFunc("POST /notifications/templates/{templateKey}/publish", s.publishLegacy)
	s.mux.HandleFunc("POST /notifications/templates/{templateKey}/publication-requests", s.requestPublication)
	s.mux.HandleFunc("POST /notifications/templates/{templateKey}/disable", s.disable)
	s.mux.HandleFunc("POST /notifications/templates/{templateKey}/preview", s.preview)
	s.mux.HandleFunc("GET /notifications/templates/{templateKey}/versions", s.listVersions)
	s.mux.HandleFunc("POST /notifications/templates/{templateKey}/versions/{version}/restore-draft", s.restoreVersion)
	s.mux.HandleFunc("GET /notifications/policy", s.getPolicy)
	s.mux.HandleFunc("PUT /notifications/policy", s.savePolicy)
	s.mux.HandleFunc("GET /notifications/preferences", s.listPreferences)
	s.mux.HandleFunc("PUT /notifications/preferences/{recipientKey}", s.savePreference)
	s.mux.HandleFunc("GET /notifications/metrics", s.metrics)
	s.mux.HandleFunc("GET /notifications/governance/catalog", s.governanceCatalog)
	s.mux.HandleFunc("GET /notifications/governance/inbox-metrics", s.inboxGovernanceMetrics)
	return s, nil
}

func templateReadRoute(pattern string) modulehttp.Route {
	return templateRoute(pattern, "notification.template.read")
}

func templateRoute(pattern string, permissions ...string) modulehttp.Route {
	return modulehttp.Route{
		Pattern: pattern, Exposures: []modulehttp.Exposure{modulehttp.ExposureTenantAdmin},
		Authentication: modulehttp.AuthenticationAuthenticated,
		AnyPermissions: append([]string{"workspace.admin"}, permissions...),
	}
}

func authority(r *http.Request) (notificationsdk.UserAuthority, error) {
	identity, ok := identitysdk.RequestIdentityFromContext(r.Context())
	if !ok || strings.TrimSpace(identity.AccessToken) == "" {
		return notificationsdk.UserAuthority{}, &notificationsdk.Error{StatusCode: http.StatusUnauthorized, Code: "notification.user_authority_required"}
	}
	surface := strings.TrimSpace(r.Header.Get("X-Domainry-Product-Surface"))
	if surface == "" {
		surface = "admin_console"
	}
	return notificationsdk.UserAuthority{AccessToken: identity.AccessToken, Surface: surface}, nil
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

func (s *surface) capabilities(w http.ResponseWriter, r *http.Request) {
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

func (s *surface) list(w http.ResponseWriter, r *http.Request) {
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

func (s *surface) get(w http.ResponseWriter, r *http.Request) {
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

type lifecycleInput struct {
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty"`
}
type publicationInput struct {
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty"`
	ScheduledFor      string `json:"scheduled_for,omitempty"`
}
type reviewInput struct {
	Reason string `json:"reason,omitempty"`
}
type draftInput struct {
	Template          contract.NotificationTemplate `json:"template"`
	ExpectedUpdatedAt string                        `json:"expected_updated_at,omitempty"`
}
type previewInput struct {
	Locale     string         `json:"locale,omitempty"`
	Recipients []string       `json:"recipients,omitempty"`
	Variables  map[string]any `json:"variables,omitempty"`
}
type previewDraftInput struct {
	Template   contract.NotificationTemplate `json:"template"`
	Locale     string                        `json:"locale,omitempty"`
	Recipients []string                      `json:"recipients,omitempty"`
	Variables  map[string]any                `json:"variables,omitempty"`
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

func (s *surface) listPublications(w http.ResponseWriter, r *http.Request) {
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
func (s *surface) approvePublication(w http.ResponseWriter, r *http.Request) {
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
func (s *surface) rejectPublication(w http.ResponseWriter, r *http.Request) {
	a, ok := withAuthority(w, r)
	if !ok {
		return
	}
	var input reviewInput
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
func (s *surface) cancelPublication(w http.ResponseWriter, r *http.Request) {
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
func (s *surface) previewDraft(w http.ResponseWriter, r *http.Request) {
	a, ok := withAuthority(w, r)
	if !ok {
		return
	}
	var input previewDraftInput
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
func (s *surface) saveDraft(w http.ResponseWriter, r *http.Request) {
	a, ok := withAuthority(w, r)
	if !ok {
		return
	}
	var input draftInput
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
func (*surface) publishLegacy(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusConflict, map[string]string{"code": "backend.notification.publication_request_required", "template_key": r.PathValue("templateKey")})
}
func (s *surface) requestPublication(w http.ResponseWriter, r *http.Request) {
	a, ok := withAuthority(w, r)
	if !ok {
		return
	}
	var input publicationInput
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
func (s *surface) disable(w http.ResponseWriter, r *http.Request) {
	a, ok := withAuthority(w, r)
	if !ok {
		return
	}
	var input lifecycleInput
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
func (s *surface) preview(w http.ResponseWriter, r *http.Request) {
	a, ok := withAuthority(w, r)
	if !ok {
		return
	}
	var input previewInput
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
func (s *surface) listVersions(w http.ResponseWriter, r *http.Request) {
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
func (s *surface) restoreVersion(w http.ResponseWriter, r *http.Request) {
	a, ok := withAuthority(w, r)
	if !ok {
		return
	}
	var input lifecycleInput
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
func (s *surface) getPolicy(w http.ResponseWriter, r *http.Request) {
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
func (s *surface) savePolicy(w http.ResponseWriter, r *http.Request) {
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
func (s *surface) listPreferences(w http.ResponseWriter, r *http.Request) {
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
func (s *surface) savePreference(w http.ResponseWriter, r *http.Request) {
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
func (s *surface) metrics(w http.ResponseWriter, r *http.Request) {
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
func (s *surface) governanceCatalog(w http.ResponseWriter, r *http.Request) {
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
func (s *surface) inboxGovernanceMetrics(w http.ResponseWriter, r *http.Request) {
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

var _ modulehttp.Surface = (*surface)(nil)
