package saas

import (
	"context"
	"errors"
	"testing"

	identitysdk "github.com/domainry/domainry-identity-sdk"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
)

type configuredApplicationServices struct {
	request   identitysdk.VerifyApplicationServiceTokenRequest
	principal identitysdk.ApplicationServicePrincipal
	err       error
}

func (*configuredApplicationServices) Exchange(context.Context, identitysdk.ExchangeApplicationServiceTokenRequest) (identitysdk.ApplicationServiceToken, error) {
	return identitysdk.ApplicationServiceToken{}, errors.New("unexpected exchange")
}

func (services *configuredApplicationServices) Verify(_ context.Context, request identitysdk.VerifyApplicationServiceTokenRequest) (identitysdk.ApplicationServicePrincipal, error) {
	services.request = request
	return services.principal, services.err
}

func TestServiceAuthenticatorBindsShortTokenToExactApplicationScope(t *testing.T) {
	application := notificationsdk.ApplicationRef{WorkspaceID: "workspace-a", ApplicationKey: "runtime-a"}
	services := &configuredApplicationServices{principal: identitysdk.ApplicationServicePrincipal{
		SubjectID:   "service:runtime-a",
		Application: identitysdk.ApplicationRef{WorkspaceID: "workspace-a", ApplicationKey: "runtime-a"},
		Audience:    "domainry-notification", AuthorizationRevision: "revision-2",
	}}
	binding := &identityBindingStub{descriptor: identitysdk.Descriptor{Mode: identitysdk.DeploymentModeSaaS, Issuer: "https://identity.example", Audience: "domainry-notification"}, services: services}
	authenticator, err := NewServiceAuthenticator(binding)
	if err != nil {
		t.Fatal(err)
	}
	grant := identitysdk.ApplicationServiceGrant{Resource: "notification_event", Action: "publish"}
	authority, err := authenticator.Authenticate(t.Context(), ServiceRequest{Credential: "short-service-token", Application: application, Grant: grant})
	if err != nil {
		t.Fatal(err)
	}
	if authority.Application != application || authority.SubjectID != "service:runtime-a" || authority.AuthorizationRevision != "revision-2" {
		t.Fatalf("authority=%+v", authority)
	}
	if services.request.AccessToken != "short-service-token" || services.request.Audience != "domainry-notification" || services.request.Grant.Resource != "notification_event" || services.request.Grant.Action != "publish" {
		t.Fatalf("verify request=%+v", services.request)
	}
}

func TestServiceAuthenticatorFailsClosedForScopeMismatchExpiryAndIdentityOutage(t *testing.T) {
	application := notificationsdk.ApplicationRef{WorkspaceID: "workspace-a", ApplicationKey: "runtime-a"}
	base := identitysdk.ApplicationServicePrincipal{SubjectID: "service:runtime-a", Application: identitysdk.ApplicationRef{WorkspaceID: "workspace-a", ApplicationKey: "runtime-a"}, Audience: "domainry-notification"}
	workspaceMismatch := base
	workspaceMismatch.Application.WorkspaceID = "workspace-b"
	applicationMismatch := base
	applicationMismatch.Application.ApplicationKey = "runtime-b"
	tests := []struct {
		name      string
		principal identitysdk.ApplicationServicePrincipal
		verifyErr error
		code      string
	}{
		{"workspace mismatch", workspaceMismatch, nil, "notification.application_scope_mismatch"},
		{"application mismatch", applicationMismatch, nil, "notification.application_scope_mismatch"},
		{"expired or rotated", base, errors.New("identity.application_service_token_invalid"), "notification.service_credential_invalid"},
		{"identity outage", base, errors.New("identity unavailable"), "notification.service_credential_invalid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			services := &configuredApplicationServices{principal: test.principal, err: test.verifyErr}
			binding := &identityBindingStub{descriptor: identitysdk.Descriptor{Mode: identitysdk.DeploymentModeSaaS, Issuer: "issuer", Audience: "domainry-notification"}, services: services}
			authenticator, err := NewServiceAuthenticator(binding)
			if err != nil {
				t.Fatal(err)
			}
			_, err = authenticator.Authenticate(t.Context(), ServiceRequest{Credential: "token", Application: application, Grant: identitysdk.ApplicationServiceGrant{Resource: "notification_event", Action: "publish"}})
			var sdkErr *notificationsdk.Error
			if !errors.As(err, &sdkErr) || sdkErr.Code != test.code {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
