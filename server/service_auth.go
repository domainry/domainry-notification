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

type ServiceAuthenticator struct {
	identity identitysdk.Binding
	services identitysdk.ApplicationServiceAuthentication
}

func NewServiceAuthenticator(identity identitysdk.Binding) (*ServiceAuthenticator, error) {
	serviceBinding, ok := identity.(identitysdk.ApplicationServiceBinding)
	if identity == nil || !ok || identity.Descriptor().Mode != identitysdk.DeploymentModeSaaS || serviceBinding.ApplicationServices() == nil {
		return nil, fmt.Errorf("Notification SaaS service authentication requires Identity SaaS application-service authentication")
	}
	return &ServiceAuthenticator{identity: identity, services: serviceBinding.ApplicationServices()}, nil
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
	verified, err := a.services.Verify(ctx, identitysdk.VerifyApplicationServiceTokenRequest{
		AccessToken: credential, Audience: identitysdk.ApplicationKey(descriptor.Audience),
		Grant: identitysdk.ApplicationServiceGrant{Resource: "notification_event", Action: "publish"},
	})
	if err != nil {
		return ServiceAuthority{}, &notificationsdk.Error{StatusCode: 401, Code: "notification.service_credential_invalid", Cause: err}
	}
	if string(verified.Application.TenantID) != request.Application.TenantID || string(verified.Application.WorkspaceID) != request.Application.WorkspaceID || string(verified.Application.ApplicationKey) != request.Application.ApplicationKey || string(verified.Audience) != descriptor.Audience || strings.TrimSpace(string(verified.SubjectID)) == "" {
		return ServiceAuthority{}, &notificationsdk.Error{StatusCode: 403, Code: "notification.application_scope_mismatch"}
	}
	return ServiceAuthority{Application: request.Application, SubjectID: string(verified.SubjectID), AuthorizationRevision: string(verified.AuthorizationRevision)}, nil
}
