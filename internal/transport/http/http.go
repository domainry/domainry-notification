package notificationhttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/contract"
	"github.com/domainry/domainry-notification/internal/application/authentication"
)

type ServiceRequest = authentication.Request
type ServiceAuthority = authentication.Authority

type ServiceAuthentication = authentication.Service

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
	grant, err := notificationsdk.ServiceGrantForRequest(request.Method, request.URL.Path)
	if err != nil {
		writeHTTPError(response, err)
		return
	}
	if _, err := h.authenticator.Authenticate(request.Context(), ServiceRequest{Credential: request.Header.Get("X-Domainry-Service-Credential"), Application: application, Grant: grant}); err != nil {
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
	case "/v1/system/templates:sync-published", "/v1/system/templates:list-published":
		if request.Method != http.MethodPost {
			writeHTTPError(response, &notificationsdk.Error{StatusCode: http.StatusMethodNotAllowed, Code: "notification.method_not_allowed"})
			return
		}
		systemBinding, ok := binding.(notificationsdk.SystemTemplateBinding)
		if !ok || systemBinding.SystemTemplates() == nil {
			writeHTTPError(response, &notificationsdk.Error{StatusCode: http.StatusNotImplemented, Code: "notification.system_templates_unsupported"})
			return
		}
		if request.URL.Path == "/v1/system/templates:sync-published" {
			var input struct {
				Templates []contract.NotificationTemplate `json:"templates"`
			}
			if err := decodeJSON(request.Body, &input); err != nil {
				writeHTTPError(response, err)
				return
			}
			if err := systemBinding.SystemTemplates().SyncPublished(request.Context(), input.Templates); err != nil {
				writeHTTPError(response, err)
				return
			}
			response.WriteHeader(http.StatusNoContent)
			return
		}
		records, err := systemBinding.SystemTemplates().ListPublished(request.Context())
		if err != nil {
			writeHTTPError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, records)
	case "/v1/system/subjects:preview", "/v1/system/subjects:export", "/v1/system/subjects:erase":
		if request.Method != http.MethodPost {
			writeHTTPError(response, &notificationsdk.Error{StatusCode: http.StatusMethodNotAllowed, Code: "notification.method_not_allowed"})
			return
		}
		systemBinding, ok := binding.(notificationsdk.SystemSubjectBinding)
		if !ok || systemBinding.SystemSubjects() == nil {
			writeHTTPError(response, &notificationsdk.Error{StatusCode: http.StatusNotImplemented, Code: "notification.system_subjects_unsupported"})
			return
		}
		var input struct {
			WorkspaceID string          `json:"workspace_id"`
			SubjectID   string          `json:"subject_id"`
			LegalHolds  json.RawMessage `json:"legal_holds,omitempty"`
		}
		if err := decodeJSON(request.Body, &input); err != nil {
			writeHTTPError(response, err)
			return
		}
		if strings.TrimSpace(input.WorkspaceID) != application.WorkspaceID || strings.TrimSpace(input.SubjectID) == "" {
			writeHTTPError(response, &notificationsdk.Error{StatusCode: http.StatusForbidden, Code: "notification.application_scope_mismatch"})
			return
		}
		var output json.RawMessage
		if request.URL.Path == "/v1/system/subjects:preview" {
			output, err = systemBinding.SystemSubjects().PreviewSubject(request.Context(), input.WorkspaceID, input.SubjectID)
		} else if request.URL.Path == "/v1/system/subjects:export" {
			output, err = systemBinding.SystemSubjects().ExportSubject(request.Context(), input.WorkspaceID, input.SubjectID)
		} else {
			output, err = systemBinding.SystemSubjects().EraseSubject(request.Context(), input.WorkspaceID, input.SubjectID, input.LegalHolds)
		}
		if err != nil {
			writeHTTPError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, output)
	case "/v1/system/retention:preview", "/v1/system/retention:process-batch":
		if request.Method != http.MethodPost {
			writeHTTPError(response, &notificationsdk.Error{StatusCode: http.StatusMethodNotAllowed, Code: "notification.method_not_allowed"})
			return
		}
		retentionBinding, ok := binding.(notificationsdk.SystemRetentionBinding)
		if !ok || retentionBinding.SystemRetention() == nil {
			writeHTTPError(response, &notificationsdk.Error{StatusCode: http.StatusNotImplemented, Code: "notification.system_retention_unsupported"})
			return
		}
		if request.URL.Path == "/v1/system/retention:preview" {
			var input contract.NotificationRetentionPreviewRequest
			if err := decodeJSON(request.Body, &input); err != nil {
				writeHTTPError(response, err)
				return
			}
			if strings.TrimSpace(input.WorkspaceID) != application.WorkspaceID {
				writeHTTPError(response, &notificationsdk.Error{StatusCode: http.StatusForbidden, Code: "notification.application_scope_mismatch"})
				return
			}
			output, err := retentionBinding.SystemRetention().Preview(request.Context(), input)
			if err != nil {
				writeHTTPError(response, err)
				return
			}
			writeJSON(response, http.StatusOK, output)
			return
		}
		var input contract.NotificationRetentionBatchRequest
		if err := decodeJSON(request.Body, &input); err != nil {
			writeHTTPError(response, err)
			return
		}
		if strings.TrimSpace(input.WorkspaceID) != application.WorkspaceID {
			writeHTTPError(response, &notificationsdk.Error{StatusCode: http.StatusForbidden, Code: "notification.application_scope_mismatch"})
			return
		}
		output, err := retentionBinding.SystemRetention().ProcessBatch(request.Context(), input)
		if err != nil {
			writeHTTPError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, output)
	case "/v1/system/migration:status", "/v1/system/migration:freeze", "/v1/system/migration:export", "/v1/system/migration:import", "/v1/system/migration:activate", "/v1/system/migration:rollback":
		if request.Method != http.MethodPost {
			writeHTTPError(response, &notificationsdk.Error{StatusCode: http.StatusMethodNotAllowed, Code: "notification.method_not_allowed"})
			return
		}
		migrationBinding, ok := binding.(notificationsdk.SystemMigrationBinding)
		if !ok || migrationBinding.SystemMigration() == nil {
			writeHTTPError(response, &notificationsdk.Error{StatusCode: http.StatusNotImplemented, Code: "notification.system_migration_unsupported"})
			return
		}
		if request.URL.Path == "/v1/system/migration:export" {
			output, err := migrationBinding.SystemMigration().Export(request.Context())
			if err != nil {
				writeHTTPError(response, err)
				return
			}
			writeJSON(response, http.StatusOK, output)
			return
		}
		if request.URL.Path == "/v1/system/migration:status" {
			output, err := migrationBinding.SystemMigration().Status(request.Context())
			if err != nil {
				writeHTTPError(response, err)
				return
			}
			writeJSON(response, http.StatusOK, output)
			return
		}
		if request.URL.Path == "/v1/system/migration:freeze" || request.URL.Path == "/v1/system/migration:activate" || request.URL.Path == "/v1/system/migration:rollback" {
			var command contract.NotificationMigrationCommand
			if err := decodeJSON(request.Body, &command); err != nil {
				writeHTTPError(response, err)
				return
			}
			var output contract.NotificationMigrationStatus
			if request.URL.Path == "/v1/system/migration:freeze" {
				output, err = migrationBinding.SystemMigration().Freeze(request.Context(), command)
			} else if request.URL.Path == "/v1/system/migration:activate" {
				output, err = migrationBinding.SystemMigration().Activate(request.Context(), command)
			} else {
				output, err = migrationBinding.SystemMigration().Rollback(request.Context(), command)
			}
			if err != nil {
				writeHTTPError(response, err)
				return
			}
			writeJSON(response, http.StatusOK, output)
			return
		}
		var bundle contract.NotificationPortableBundle
		if err := decodeJSON(request.Body, &bundle); err != nil {
			writeHTTPError(response, err)
			return
		}
		if bundle.Source.TenantID != application.TenantID || bundle.Source.WorkspaceID != application.WorkspaceID || bundle.Source.ApplicationKey != application.ApplicationKey {
			writeHTTPError(response, &notificationsdk.Error{StatusCode: http.StatusForbidden, Code: "notification.application_scope_mismatch"})
			return
		}
		output, err := migrationBinding.SystemMigration().Import(request.Context(), bundle)
		if err != nil {
			writeHTTPError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, output)
	default:
		if !serveBindingRoute(response, request, binding) {
			writeHTTPError(response, &notificationsdk.Error{StatusCode: http.StatusNotFound, Code: "notification.route_not_found"})
		}
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
