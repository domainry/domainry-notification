package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/contract"
)

type ServiceAuthentication interface {
	Authenticate(context.Context, ServiceRequest) (ServiceAuthority, error)
}

type BindingResolver interface {
	Resolve(context.Context, notificationsdk.ApplicationRef) (notificationsdk.Binding, error)
}

type Handler struct {
	authenticator ServiceAuthentication
	bindings      BindingResolver
}

func NewHandler(authenticator ServiceAuthentication, bindings BindingResolver) (*Handler, error) {
	if authenticator == nil || bindings == nil {
		return nil, errors.New("Notification SaaS HTTP authentication and Binding resolver are required")
	}
	return &Handler{authenticator: authenticator, bindings: bindings}, nil
}

func (h *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodPost {
		writeHTTPError(response, &notificationsdk.Error{StatusCode: http.StatusMethodNotAllowed, Code: "notification.method_not_allowed"})
		return
	}
	application := notificationsdk.ApplicationRef{
		TenantID: strings.TrimSpace(request.Header.Get("X-Domainry-Tenant-ID")), WorkspaceID: strings.TrimSpace(request.Header.Get("X-Domainry-Workspace-ID")), ApplicationKey: strings.TrimSpace(request.Header.Get("X-Domainry-Application-Key")),
	}
	if _, err := h.authenticator.Authenticate(request.Context(), ServiceRequest{Credential: request.Header.Get("X-Domainry-Service-Credential"), Application: application}); err != nil {
		writeHTTPError(response, err)
		return
	}
	binding, err := h.bindings.Resolve(request.Context(), application)
	if err != nil {
		writeHTTPError(response, err)
		return
	}
	if binding == nil {
		writeHTTPError(response, &notificationsdk.Error{StatusCode: http.StatusServiceUnavailable, Code: "notification.binding_unavailable", Retryable: true})
		return
	}
	switch request.URL.Path {
	case "/v1/descriptor":
		if request.Method != http.MethodGet {
			writeHTTPError(response, &notificationsdk.Error{StatusCode: http.StatusMethodNotAllowed, Code: "notification.method_not_allowed"})
			return
		}
		writeJSON(response, http.StatusOK, notificationsdk.Descriptor{ProtocolVersion: notificationsdk.CurrentProtocolVersion, Mode: notificationsdk.DeploymentModeSaaS, Audience: application.ApplicationKey, Capabilities: []string{"publication", "inbox", "templates", "delivery", "administration"}})
	case "/v1/events:publish":
		if request.Method != http.MethodPost {
			writeHTTPError(response, &notificationsdk.Error{StatusCode: http.StatusMethodNotAllowed, Code: "notification.method_not_allowed"})
			return
		}
		var intent contract.NotificationIntent
		if err := decodeJSON(request.Body, &intent); err != nil {
			writeHTTPError(response, err)
			return
		}
		if intent.WorkspaceID != application.WorkspaceID {
			writeHTTPError(response, &notificationsdk.Error{StatusCode: http.StatusForbidden, Code: "notification.application_scope_mismatch"})
			return
		}
		event, created, err := binding.Publisher().PublishIntent(request.Context(), intent)
		if err != nil {
			writeHTTPError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Event   contract.NotificationEvent `json:"event"`
			Created bool                       `json:"created"`
		}{event, created})
	default:
		writeHTTPError(response, &notificationsdk.Error{StatusCode: http.StatusNotFound, Code: "notification.route_not_found"})
	}
}

func decodeJSON(reader io.Reader, output any) error {
	decoder := json.NewDecoder(io.LimitReader(reader, 8<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return &notificationsdk.Error{StatusCode: http.StatusBadRequest, Code: "notification.request_invalid", Cause: err}
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return &notificationsdk.Error{StatusCode: http.StatusBadRequest, Code: "notification.request_invalid"}
	}
	return nil
}

func writeHTTPError(response http.ResponseWriter, err error) {
	var sdkErr *notificationsdk.Error
	if !errors.As(err, &sdkErr) {
		sdkErr = &notificationsdk.Error{StatusCode: http.StatusInternalServerError, Code: "notification.internal_error"}
	}
	status := sdkErr.StatusCode
	if status < 400 || status > 599 {
		status = http.StatusInternalServerError
	}
	writeJSON(response, status, sdkErr)
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
