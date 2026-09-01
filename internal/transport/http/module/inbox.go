package module

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/domainry/domainry-foundation/modulehttp"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/contract"
)

func inboxRoutes(prefix string) []modulehttp.Route {
	patterns := []string{
		"GET " + prefix + "/notifications", "GET " + prefix + "/notifications/facets",
		"GET " + prefix + "/notifications/unread-count", "GET " + prefix + "/notifications/stream",
		"GET " + prefix + "/notification-preferences", "PUT " + prefix + "/notification-preferences",
		"GET " + prefix + "/notifications/saved-views", "PUT " + prefix + "/notifications/saved-views/{viewKey}",
		"DELETE " + prefix + "/notifications/saved-views/{viewKey}", "GET " + prefix + "/notifications/delegations",
		"PUT " + prefix + "/notifications/delegations/{delegationID}", "DELETE " + prefix + "/notifications/delegations/{delegationID}",
		"GET " + prefix + "/notifications/delegated-owners", "GET " + prefix + "/notifications/{notificationID}",
		"POST " + prefix + "/notifications/{notificationID}/read", "POST " + prefix + "/notifications/{notificationID}/unread",
		"POST " + prefix + "/notifications/{notificationID}/archive", "POST " + prefix + "/notifications/{notificationID}/restore",
		"POST " + prefix + "/notifications/{notificationID}/acknowledge", "POST " + prefix + "/notifications/read-all",
	}
	routes := make([]modulehttp.Route, 0, len(patterns))
	for _, pattern := range patterns {
		routes = append(routes, modulehttp.Route{Pattern: pattern, Exposures: []modulehttp.Exposure{modulehttp.ExposurePublic}, Authentication: modulehttp.AuthenticationAuthenticated, PrincipalOnly: true, Governance: notificationRouteGovernance(pattern)})
	}
	return routes
}

func (s *surface) registerInboxRoutes(prefix string) {
	m := s.mux
	m.HandleFunc("GET "+prefix+"/notifications", s.listInbox)
	m.HandleFunc("GET "+prefix+"/notifications/facets", s.inboxFacets)
	m.HandleFunc("GET "+prefix+"/notifications/unread-count", s.inboxUnreadCount)
	m.HandleFunc("GET "+prefix+"/notifications/stream", s.streamInbox)
	m.HandleFunc("GET "+prefix+"/notification-preferences", s.getInboxPreference)
	m.HandleFunc("PUT "+prefix+"/notification-preferences", s.saveInboxPreference)
	m.HandleFunc("GET "+prefix+"/notifications/saved-views", s.listSavedViews)
	m.HandleFunc("PUT "+prefix+"/notifications/saved-views/{viewKey}", s.saveSavedView)
	m.HandleFunc("DELETE "+prefix+"/notifications/saved-views/{viewKey}", s.deleteSavedView)
	m.HandleFunc("GET "+prefix+"/notifications/delegations", s.listDelegations)
	m.HandleFunc("PUT "+prefix+"/notifications/delegations/{delegationID}", s.saveDelegation)
	m.HandleFunc("DELETE "+prefix+"/notifications/delegations/{delegationID}", s.deleteDelegation)
	m.HandleFunc("GET "+prefix+"/notifications/delegated-owners", s.listDelegatedOwners)
	m.HandleFunc("GET "+prefix+"/notifications/{notificationID}", s.getInbox)
	m.HandleFunc("POST "+prefix+"/notifications/{notificationID}/read", func(w http.ResponseWriter, r *http.Request) { s.setInboxRead(w, r, true) })
	m.HandleFunc("POST "+prefix+"/notifications/{notificationID}/unread", func(w http.ResponseWriter, r *http.Request) { s.setInboxRead(w, r, false) })
	m.HandleFunc("POST "+prefix+"/notifications/{notificationID}/archive", func(w http.ResponseWriter, r *http.Request) { s.setInboxArchived(w, r, true) })
	m.HandleFunc("POST "+prefix+"/notifications/{notificationID}/restore", func(w http.ResponseWriter, r *http.Request) { s.setInboxArchived(w, r, false) })
	m.HandleFunc("POST "+prefix+"/notifications/{notificationID}/acknowledge", s.acknowledgeInbox)
	m.HandleFunc("POST "+prefix+"/notifications/read-all", s.markAllInboxRead)
}

func inboxSurface(r *http.Request) string {
	if strings.HasPrefix(r.URL.Path, "/portal/") {
		return "consumer_portal"
	}
	return "business_workspace"
}
func (s *surface) inboxCall(w http.ResponseWriter, r *http.Request) (contract.NotificationInboxQuery, bool) {
	if s.binding.Inbox() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "backend.notification.inbox_unavailable"})
		return contract.NotificationInboxQuery{}, false
	}
	return inboxQuery(r), true
}
func inboxAuth(r *http.Request) (a notificationsdk.UserAuthority, err error) {
	a, err = authority(r)
	a.Surface = inboxSurface(r)
	return
}
func inboxQuery(r *http.Request) contract.NotificationInboxQuery {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	member := q.Get("team_member_id")
	if strings.TrimSpace(q.Get("scope")) == contract.NotificationInboxScopeDelegated {
		member = q.Get("delegated_owner_id")
	}
	return contract.NotificationInboxQuery{Mailbox: q.Get("mailbox"), Query: q.Get("query"), Categories: q["category"], Sources: q["source"], Severities: q["severity"], ActionStates: q["action_state"], From: q.Get("from"), To: q.Get("to"), Limit: limit, Scope: q.Get("scope"), TeamMemberID: member}
}
func (s *surface) withInbox(w http.ResponseWriter, r *http.Request) (notificationsdk.UserAuthority, contract.NotificationInboxQuery, bool) {
	q, ok := s.inboxCall(w, r)
	if !ok {
		return notificationsdk.UserAuthority{}, q, false
	}
	a, err := inboxAuth(r)
	if err != nil {
		writeError(w, err)
		return a, q, false
	}
	return a, q, true
}
func (s *surface) listInbox(w http.ResponseWriter, r *http.Request) {
	a, q, ok := s.withInbox(w, r)
	if !ok {
		return
	}
	v, e := s.binding.Inbox().List(r.Context(), a, q, r.URL.Query().Get("cursor"))
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (s *surface) getInbox(w http.ResponseWriter, r *http.Request) {
	a, q, ok := s.withInbox(w, r)
	if !ok {
		return
	}
	v, e := s.binding.Inbox().Get(r.Context(), a, r.PathValue("notificationID"), q)
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (s *surface) inboxFacets(w http.ResponseWriter, r *http.Request) {
	a, q, ok := s.withInbox(w, r)
	if !ok {
		return
	}
	v, e := s.binding.Inbox().Facets(r.Context(), a, q)
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (s *surface) inboxUnreadCount(w http.ResponseWriter, r *http.Request) {
	a, q, ok := s.withInbox(w, r)
	if !ok {
		return
	}
	q.Mailbox = contract.NotificationMailboxInbox
	v, e := s.binding.Inbox().Facets(r.Context(), a, q)
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, map[string]int{"unread": v.Unread})
}
func (s *surface) setInboxRead(w http.ResponseWriter, r *http.Request, v bool) {
	a, _, ok := s.withInbox(w, r)
	if !ok {
		return
	}
	x, e := s.binding.Inbox().SetRead(r.Context(), a, r.PathValue("notificationID"), v)
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, x)
}
func (s *surface) setInboxArchived(w http.ResponseWriter, r *http.Request, v bool) {
	a, _, ok := s.withInbox(w, r)
	if !ok {
		return
	}
	x, e := s.binding.Inbox().SetArchived(r.Context(), a, r.PathValue("notificationID"), v)
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, x)
}
func (s *surface) acknowledgeInbox(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.withInbox(w, r)
	if !ok {
		return
	}
	v, e := s.binding.Inbox().AcknowledgeAlert(r.Context(), a, r.PathValue("notificationID"))
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (s *surface) markAllInboxRead(w http.ResponseWriter, r *http.Request) {
	a, q, ok := s.withInbox(w, r)
	if !ok {
		return
	}
	v, e := s.binding.Inbox().MarkAllRead(r.Context(), a, q)
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, map[string]int{"updated": v})
}
func (s *surface) getInboxPreference(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.withInbox(w, r)
	if !ok {
		return
	}
	v, e := s.binding.Inbox().GetPreference(r.Context(), a, inboxSurface(r))
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (s *surface) saveInboxPreference(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.withInbox(w, r)
	if !ok {
		return
	}
	var v contract.NotificationRecipientPreference
	if !decode(w, r, &v) {
		return
	}
	x, e := s.binding.Inbox().SavePreference(r.Context(), a, inboxSurface(r), v)
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, x)
}
func (s *surface) listSavedViews(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.withInbox(w, r)
	if !ok {
		return
	}
	v, e := s.binding.Inbox().ListSavedViews(r.Context(), a, inboxSurface(r))
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"views": v, "count": len(v)})
}
func (s *surface) saveSavedView(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.withInbox(w, r)
	if !ok {
		return
	}
	var v contract.NotificationInboxSavedView
	if !decode(w, r, &v) {
		return
	}
	v.Key = r.PathValue("viewKey")
	x, e := s.binding.Inbox().SaveSavedView(r.Context(), a, v)
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, x)
}
func (s *surface) deleteSavedView(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.withInbox(w, r)
	if !ok {
		return
	}
	if e := s.binding.Inbox().DeleteSavedView(r.Context(), a, r.PathValue("viewKey")); e != nil {
		writeError(w, e)
		return
	}
	w.WriteHeader(204)
}
func (s *surface) listDelegations(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.withInbox(w, r)
	if !ok {
		return
	}
	v, e := s.binding.Inbox().ListDelegations(r.Context(), a, inboxSurface(r))
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"delegations": v, "count": len(v)})
}
func (s *surface) saveDelegation(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.withInbox(w, r)
	if !ok {
		return
	}
	var v contract.NotificationInboxDelegation
	if !decode(w, r, &v) {
		return
	}
	v.ID = r.PathValue("delegationID")
	x, e := s.binding.Inbox().SaveDelegation(r.Context(), a, v)
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, x)
}
func (s *surface) deleteDelegation(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.withInbox(w, r)
	if !ok {
		return
	}
	if e := s.binding.Inbox().DeleteDelegation(r.Context(), a, r.PathValue("delegationID")); e != nil {
		writeError(w, e)
		return
	}
	w.WriteHeader(204)
}
func (s *surface) listDelegatedOwners(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.withInbox(w, r)
	if !ok {
		return
	}
	v, e := s.binding.Inbox().ListDelegatedOwnerIDs(r.Context(), a, inboxSurface(r))
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"owner_user_ids": v, "count": len(v)})
}

type inboxSync struct {
	Cursor    string `json:"cursor"`
	Unread    int    `json:"unread"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

func (s *surface) inboxState(r *http.Request, a notificationsdk.UserAuthority) (inboxSync, error) {
	q := contract.NotificationInboxQuery{Scope: contract.NotificationInboxScopeMine, Mailbox: contract.NotificationMailboxInbox, Limit: 1}
	p, e := s.binding.Inbox().List(r.Context(), a, q, "")
	if e != nil {
		return inboxSync{}, e
	}
	f, e := s.binding.Inbox().Facets(r.Context(), a, q)
	if e != nil {
		return inboxSync{}, e
	}
	v := inboxSync{Unread: f.Unread}
	id := struct {
		UpdatedAt string `json:"updated_at"`
		ID        string `json:"id"`
		Unread    int    `json:"unread"`
	}{Unread: f.Unread}
	if len(p.Items) > 0 {
		v.UpdatedAt = p.Items[0].UpdatedAt
		id.UpdatedAt = p.Items[0].UpdatedAt
		id.ID = p.Items[0].ID
	}
	raw, _ := json.Marshal(id)
	v.Cursor = base64.RawURLEncoding.EncodeToString(raw)
	return v, nil
}
func writeInboxSSE(w http.ResponseWriter, event, id string, v inboxSync) bool {
	raw, _ := json.Marshal(v)
	if _, e := fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", id, event, raw); e != nil {
		return false
	}
	return http.NewResponseController(w).Flush() == nil
}
func (s *surface) streamInbox(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.withInbox(w, r)
	if !ok {
		return
	}
	v, e := s.inboxState(r, a)
	if e != nil {
		writeError(w, e)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	last := strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	if x := strings.TrimSpace(r.URL.Query().Get("cursor")); x != "" {
		last = x
	}
	event := "notification.sync"
	if v.Cursor == last {
		event = "notification.ready"
	}
	if !writeInboxSSE(w, event, v.Cursor, v) {
		return
	}
	last = v.Cursor
	poll := time.NewTicker(2 * time.Second)
	heartbeat := time.NewTicker(15 * time.Second)
	defer poll.Stop()
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, e := fmt.Fprint(w, ": keepalive\n\n"); e != nil || http.NewResponseController(w).Flush() != nil {
				return
			}
		case <-poll.C:
			n, e := s.inboxState(r, a)
			if e != nil {
				return
			}
			if n.Cursor != last {
				if !writeInboxSSE(w, "notification.sync", n.Cursor, n) {
					return
				}
				last = n.Cursor
			}
		}
	}
}
