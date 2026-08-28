package server

import (
	"context"
	"errors"
	"testing"

	identitysdk "github.com/domainry/domainry-identity-sdk"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
)

type configuredTokenVerifier struct {
	request identitysdk.VerifyTokenRequest
	token   identitysdk.VerifiedToken
	err     error
}

func (v *configuredTokenVerifier) Verify(_ context.Context, request identitysdk.VerifyTokenRequest) (identitysdk.VerifiedToken, error) {
	v.request = request
	return v.token, v.err
}

type configuredAuthorization struct {
	request  identitysdk.DecisionRequest
	decision identitysdk.AccessDecision
	err      error
}

func (*configuredAuthorization) ResolveAccess(context.Context, identitysdk.AccessBundleRequest) (identitysdk.AccessBundle, error) {
	return identitysdk.AccessBundle{}, nil
}
func (a *configuredAuthorization) Reauthorize(_ context.Context, request identitysdk.DecisionRequest) (identitysdk.AccessDecision, error) {
	a.request = request
	return a.decision, a.err
}

func TestServiceAuthenticatorBindsCredentialToExactApplicationScope(t *testing.T) {
	application := notificationsdk.ApplicationRef{TenantID: "tenant-a", WorkspaceID: "workspace-a", ApplicationKey: "runtime-a"}
	tokens := &configuredTokenVerifier{token: identitysdk.VerifiedToken{Issuer: "https://identity.example", Audience: "domainry-notification", SubjectID: "runtime-service", TenantID: "tenant-a", WorkspaceID: "workspace-a", AuthorizationRevision: "revision-1"}}
	authorization := &configuredAuthorization{decision: identitysdk.AccessDecision{Allowed: true, AuthorizationRevision: "revision-2"}}
	binding := &identityBindingStub{descriptor: identitysdk.Descriptor{Mode: identitysdk.DeploymentModeSaaS, Issuer: "https://identity.example", Audience: "domainry-notification"}, tokens: tokens, authorize: authorization}
	authenticator, err := NewServiceAuthenticator(binding)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := authenticator.Authenticate(t.Context(), ServiceRequest{Credential: "service-token", Application: application})
	if err != nil {
		t.Fatal(err)
	}
	if authority.Application != application || authority.SubjectID != "runtime-service" || authority.AuthorizationRevision != "revision-2" {
		t.Fatalf("authority=%+v", authority)
	}
	if tokens.request.AccessToken != "service-token" || tokens.request.Issuer != "https://identity.example" || tokens.request.Audience != "domainry-notification" {
		t.Fatalf("verify request=%+v", tokens.request)
	}
	if authorization.request.Access.ObjectKey != "notification_event" || authorization.request.Access.Action != "publish" || authorization.request.Facts["application_key"] != "runtime-a" {
		t.Fatalf("authorization request=%+v", authorization.request)
	}
}

func TestServiceAuthenticatorFailsClosedForScopeDenialAndIdentityOutage(t *testing.T) {
	application := notificationsdk.ApplicationRef{TenantID: "tenant-a", WorkspaceID: "workspace-a", ApplicationKey: "runtime-a"}
	baseToken := identitysdk.VerifiedToken{Audience: "domainry-notification", SubjectID: "runtime-service", TenantID: "tenant-a", WorkspaceID: "workspace-a"}
	tenantMismatch := baseToken
	tenantMismatch.TenantID = "tenant-b"
	workspaceMismatch := baseToken
	workspaceMismatch.WorkspaceID = "workspace-b"
	tests := []struct {
		name          string
		token         identitysdk.VerifiedToken
		authorization *configuredAuthorization
		code          string
	}{
		{"tenant mismatch", tenantMismatch, &configuredAuthorization{decision: identitysdk.AccessDecision{Allowed: true}}, "notification.application_scope_mismatch"},
		{"workspace mismatch", workspaceMismatch, &configuredAuthorization{decision: identitysdk.AccessDecision{Allowed: true}}, "notification.application_scope_mismatch"},
		{"denied", baseToken, &configuredAuthorization{decision: identitysdk.AccessDecision{Allowed: false}}, "notification.service_not_authorized"},
		{"outage", baseToken, &configuredAuthorization{err: errors.New("identity unavailable")}, "notification.identity_reauthorization_failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			binding := &identityBindingStub{descriptor: identitysdk.Descriptor{Mode: identitysdk.DeploymentModeSaaS, Issuer: "issuer", Audience: "domainry-notification"}, tokens: &configuredTokenVerifier{token: test.token}, authorize: test.authorization}
			authenticator, _ := NewServiceAuthenticator(binding)
			_, err := authenticator.Authenticate(t.Context(), ServiceRequest{Credential: "token", Application: application})
			var sdkErr *notificationsdk.Error
			if !errors.As(err, &sdkErr) || sdkErr.Code != test.code {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
