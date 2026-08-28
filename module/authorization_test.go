package module

import (
	"context"
	"errors"
	"testing"

	identitysdk "github.com/domainry/domainry-identity-sdk"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/contract"
)

type principalAuthenticatorStub struct {
	principal identitysdk.Principal
	err       error
}

func (s principalAuthenticatorStub) Authenticate(context.Context, string) (identitysdk.Principal, error) {
	return s.principal, s.err
}

type authorizationCapture struct {
	identitysdk.Authorization
	decision identitysdk.AccessDecision
	err      error
	requests []identitysdk.DecisionRequest
}

func (a *authorizationCapture) Reauthorize(_ context.Context, request identitysdk.DecisionRequest) (identitysdk.AccessDecision, error) {
	a.requests = append(a.requests, request)
	return a.decision, a.err
}

type authorizationBindingStub struct {
	identitysdk.Binding
	authorization identitysdk.Authorization
}

func (s authorizationBindingStub) Authorization() identitysdk.Authorization { return s.authorization }

func authorizedPrincipal(workspaceID string, grants ...identitysdk.FunctionGrant) identitysdk.Principal {
	return identitysdk.Principal{
		Known: true, WorkspaceID: workspaceID, UserID: "user-1",
		AccessBundle: &identitysdk.AccessBundle{FunctionGrants: grants},
	}
}

func TestAuthorizeFailsClosedWithoutRequiredPermission(t *testing.T) {
	capture := &authorizationCapture{decision: identitysdk.AccessDecision{Allowed: true}}
	b := &binding{
		application: notificationsdk.ApplicationRef{TenantID: "tenant-1", WorkspaceID: "workspace-1", ApplicationKey: "app-1"},
		identity:    authorizationBindingStub{authorization: capture},
		principals: principalAuthenticatorStub{principal: authorizedPrincipal("workspace-1",
			identitysdk.FunctionGrant{Resource: "notification_inbox", Action: "read", Effect: identitysdk.EffectAllow},
		)},
	}
	_, err := b.authorize(context.Background(), notificationsdk.UserAuthority{AccessToken: "secret"}, "notification_inbox", "update", true)
	assertNotificationAuthorizationError(t, err, 403, "notification.permission_denied", false)
	if len(capture.requests) != 0 {
		t.Fatalf("reauthorization must not run after cached permission denial: %#v", capture.requests)
	}
}

func TestAuthorizePreservesWorkspaceAdministratorOverride(t *testing.T) {
	b := &binding{
		application: notificationsdk.ApplicationRef{TenantID: "tenant-1", WorkspaceID: "workspace-1", ApplicationKey: "app-1"},
		principals: principalAuthenticatorStub{principal: authorizedPrincipal("workspace-1",
			identitysdk.FunctionGrant{Resource: "workspace", Action: "admin", Effect: identitysdk.EffectAllow},
		)},
	}
	if _, err := b.authorize(context.Background(), notificationsdk.UserAuthority{AccessToken: "secret"}, "notification_inbox", "read", false); err != nil {
		t.Fatal(err)
	}
}

func TestAuthorizeReauthorizesMutationWithExactApplicationFacts(t *testing.T) {
	capture := &authorizationCapture{decision: identitysdk.AccessDecision{Allowed: true, AuthorizationRevision: "revision-2"}}
	b := &binding{
		application: notificationsdk.ApplicationRef{TenantID: "tenant-1", WorkspaceID: "workspace-1", ApplicationKey: "app-1"},
		identity:    authorizationBindingStub{authorization: capture},
		principals: principalAuthenticatorStub{principal: authorizedPrincipal("workspace-1",
			identitysdk.FunctionGrant{Resource: "notification_template", Action: "draft", Effect: identitysdk.EffectAllow},
		)},
	}
	principal, err := b.authorize(context.Background(), notificationsdk.UserAuthority{AccessToken: "secret"}, "notification_template", "draft", true)
	if err != nil {
		t.Fatal(err)
	}
	if principal.AuthorizationRevision != "revision-2" || len(capture.requests) != 1 {
		t.Fatalf("unexpected reauthorization result: principal=%#v requests=%#v", principal, capture.requests)
	}
	request := capture.requests[0]
	if request.Identity.AccessToken != "secret" || request.Access.ObjectKey != "notification_template" || request.Access.Action != "draft" {
		t.Fatalf("unexpected decision request: %#v", request)
	}
	if request.Facts["tenant_id"] != "tenant-1" || request.Facts["workspace_id"] != "workspace-1" || request.Facts["application_key"] != "app-1" {
		t.Fatalf("application facts are not exact: %#v", request.Facts)
	}
}

func TestAuthorizeFailsClosedWhenCurrentIdentityDeniesOrIsUnavailable(t *testing.T) {
	tests := []struct {
		name      string
		decision  identitysdk.AccessDecision
		err       error
		status    int
		code      string
		retryable bool
	}{
		{name: "denied", decision: identitysdk.AccessDecision{Allowed: false}, status: 403, code: "notification.permission_denied"},
		{name: "unavailable", err: errors.New("identity unavailable"), status: 503, code: "notification.identity_reauthorization_failed", retryable: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			capture := &authorizationCapture{decision: test.decision, err: test.err}
			b := &binding{
				application: notificationsdk.ApplicationRef{TenantID: "tenant-1", WorkspaceID: "workspace-1", ApplicationKey: "app-1"},
				identity:    authorizationBindingStub{authorization: capture},
				principals: principalAuthenticatorStub{principal: authorizedPrincipal("workspace-1",
					identitysdk.FunctionGrant{Resource: "notification_publication", Action: "approve", Effect: identitysdk.EffectAllow},
				)},
			}
			_, err := b.authorize(context.Background(), notificationsdk.UserAuthority{AccessToken: "secret"}, "notification_publication", "approve", true)
			assertNotificationAuthorizationError(t, err, test.status, test.code, test.retryable)
		})
	}
}

func TestInboxScopeRequiresAdditionalTeamAndDelegationPermissions(t *testing.T) {
	b := &binding{
		application: notificationsdk.ApplicationRef{TenantID: "tenant-1", WorkspaceID: "workspace-1", ApplicationKey: "app-1"},
		principals: principalAuthenticatorStub{principal: authorizedPrincipal("workspace-1",
			identitysdk.FunctionGrant{Resource: "notification_inbox", Action: "read", Effect: identitysdk.EffectAllow},
		)},
	}
	authority := notificationsdk.UserAuthority{AccessToken: "secret", Surface: "business_workspace"}
	tests := []struct {
		name  string
		query contract.NotificationInboxQuery
	}{
		{name: "team mailbox", query: contract.NotificationInboxQuery{Scope: contract.NotificationInboxScopeMine, TeamMemberID: "user-2"}},
		{name: "delegated mailbox", query: contract.NotificationInboxQuery{Scope: contract.NotificationInboxScopeDelegated}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := (moduleInbox{b: b}).scope(context.Background(), authority, test.query, "read", false)
			assertNotificationAuthorizationError(t, err, 403, "notification.permission_denied", false)
		})
	}
}

func assertNotificationAuthorizationError(t *testing.T, err error, status int, code string, retryable bool) {
	t.Helper()
	var notificationErr *notificationsdk.Error
	if !errors.As(err, &notificationErr) || notificationErr.StatusCode != status || notificationErr.Code != code || notificationErr.Retryable != retryable {
		t.Fatalf("unexpected error: %#v", err)
	}
}
