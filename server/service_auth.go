package server

import (
	"context"
	"fmt"
	"strings"

	identitysdk "github.com/domainry/domainry-identity-sdk"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
)

type ServiceRequest struct {
	Credential  string
	Application notificationsdk.ApplicationRef
}

type ServiceAuthority struct {
	Application           notificationsdk.ApplicationRef
	SubjectID             string
	AuthorizationRevision string
}

type ServiceAuthenticator struct{ identity identitysdk.Binding }

func NewServiceAuthenticator(identity identitysdk.Binding) (*ServiceAuthenticator, error) {
	if identity == nil || identity.Descriptor().Mode != identitysdk.DeploymentModeSaaS || identity.Tokens() == nil || identity.Authorization() == nil {
		return nil, fmt.Errorf("Notification SaaS service authentication requires Identity SaaS token and authorization capabilities")
	}
	return &ServiceAuthenticator{identity: identity}, nil
}

func (a *ServiceAuthenticator) Authenticate(ctx context.Context, request ServiceRequest) (ServiceAuthority, error) {
	if a == nil || a.identity == nil {
		return ServiceAuthority{}, &notificationsdk.Error{StatusCode: 503, Code: "notification.identity_unavailable", Retryable: true}
	}
	if err := request.Application.Validate(); err != nil {
		return ServiceAuthority{}, err
	}
	credential := strings.TrimSpace(request.Credential)
	if credential == "" {
		return ServiceAuthority{}, &notificationsdk.Error{StatusCode: 401, Code: "notification.service_credential_required"}
	}
	descriptor := a.identity.Descriptor()
	verified, err := a.identity.Tokens().Verify(ctx, identitysdk.VerifyTokenRequest{AccessToken: credential, Issuer: descriptor.Issuer, Audience: identitysdk.ApplicationKey(descriptor.Audience)})
	if err != nil {
		return ServiceAuthority{}, &notificationsdk.Error{StatusCode: 401, Code: "notification.service_credential_invalid", Cause: err}
	}
	if string(verified.TenantID) != request.Application.TenantID || string(verified.WorkspaceID) != request.Application.WorkspaceID || string(verified.Audience) != descriptor.Audience || strings.TrimSpace(string(verified.SubjectID)) == "" {
		return ServiceAuthority{}, &notificationsdk.Error{StatusCode: 403, Code: "notification.application_scope_mismatch"}
	}
	principal := identitysdk.Principal{Known: true, WorkspaceID: string(verified.WorkspaceID), UserID: string(verified.SubjectID), AuthorizationRevision: string(verified.AuthorizationRevision)}
	decision, err := a.identity.Authorization().Reauthorize(ctx, identitysdk.DecisionRequest{
		Identity: identitysdk.RequestIdentity{Principal: principal, AccessToken: credential},
		Access:   identitysdk.AccessRequest{ObjectKey: "notification_event", Action: "publish"},
		Facts: identitysdk.ResourceFacts{
			"tenant_id": request.Application.TenantID, "workspace_id": request.Application.WorkspaceID, "application_key": request.Application.ApplicationKey,
		},
	})
	if err != nil {
		return ServiceAuthority{}, &notificationsdk.Error{StatusCode: 503, Code: "notification.identity_reauthorization_failed", Retryable: true, Cause: err}
	}
	if !decision.Allowed {
		return ServiceAuthority{}, &notificationsdk.Error{StatusCode: 403, Code: "notification.service_not_authorized"}
	}
	return ServiceAuthority{Application: request.Application, SubjectID: string(verified.SubjectID), AuthorizationRevision: decision.AuthorizationRevision}, nil
}
