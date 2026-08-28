package server

import (
	"context"
	"errors"
	"testing"

	identitysdk "github.com/domainry/domainry-identity-sdk"
)

type identityFactoryStub struct {
	binding identitysdk.Binding
	opened  identitysdk.ApplicationRef
	err     error
}

func (f *identityFactoryStub) Open(_ context.Context, application identitysdk.ApplicationRef) (identitysdk.Binding, error) {
	f.opened = application
	return f.binding, f.err
}

type identityBindingStub struct {
	descriptor identitysdk.Descriptor
	catalog    *catalogStub
	tokens     identitysdk.TokenVerifier
	authorize  identitysdk.Authorization
	closed     bool
}

func (b *identityBindingStub) Descriptor() identitysdk.Descriptor       { return b.descriptor }
func (*identityBindingStub) Authentication() identitysdk.Authentication { return nil }
func (b *identityBindingStub) Tokens() identitysdk.TokenVerifier {
	if b.tokens != nil {
		return b.tokens
	}
	return tokenVerifierStub{}
}
func (b *identityBindingStub) Authorization() identitysdk.Authorization {
	if b.authorize != nil {
		return b.authorize
	}
	return authorizationStub{}
}
func (*identityBindingStub) Principals() identitysdk.PrincipalResolver {
	return principalResolverStub{}
}
func (*identityBindingStub) Directory() identitysdk.Directory           { return directoryStub{} }
func (b *identityBindingStub) Catalog() identitysdk.CatalogClient       { return b.catalog }
func (*identityBindingStub) Credentials() identitysdk.CredentialManager { return nil }
func (b *identityBindingStub) Close(context.Context) error              { b.closed = true; return nil }

type tokenVerifierStub struct{}

func (tokenVerifierStub) Verify(context.Context, identitysdk.VerifyTokenRequest) (identitysdk.VerifiedToken, error) {
	return identitysdk.VerifiedToken{}, nil
}

type authorizationStub struct{}

func (authorizationStub) ResolveAccess(context.Context, identitysdk.AccessBundleRequest) (identitysdk.AccessBundle, error) {
	return identitysdk.AccessBundle{}, nil
}
func (authorizationStub) Reauthorize(context.Context, identitysdk.DecisionRequest) (identitysdk.AccessDecision, error) {
	return identitysdk.AccessDecision{}, nil
}

type principalResolverStub struct{}

func (principalResolverStub) Resolve(context.Context, identitysdk.PrincipalResolutionRequest) (identitysdk.PrincipalResolution, error) {
	return identitysdk.PrincipalResolution{}, nil
}

type directoryStub struct{}

func (directoryStub) FindUser(context.Context, identitysdk.UserLookup) (identitysdk.User, bool, error) {
	return identitysdk.User{}, false, nil
}
func (directoryStub) FindDepartment(context.Context, identitysdk.DepartmentLookup) (identitysdk.Department, bool, error) {
	return identitysdk.Department{}, false, nil
}
func (directoryStub) ListUsers(context.Context, identitysdk.DirectoryQuery) ([]identitysdk.User, error) {
	return nil, nil
}
func (directoryStub) ListRoles(context.Context, identitysdk.DirectoryQuery) ([]identitysdk.Role, error) {
	return nil, nil
}
func (directoryStub) ListUserRoleAssignments(context.Context, identitysdk.UserRoleAssignmentQuery) ([]identitysdk.UserRoleAssignment, error) {
	return nil, nil
}
func (directoryStub) ListWorkforce(context.Context, identitysdk.DirectoryQuery) ([]identitysdk.WorkforceEntry, error) {
	return nil, nil
}

type catalogStub struct {
	validated identitysdk.AuthorizationCatalog
	published identitysdk.AuthorizationCatalog
	err       error
}

func (c *catalogStub) Validate(_ context.Context, value identitysdk.AuthorizationCatalog) error {
	c.validated = value
	return c.err
}
func (c *catalogStub) Publish(_ context.Context, value identitysdk.AuthorizationCatalog) (identitysdk.CatalogReceipt, error) {
	c.published = value
	return identitysdk.CatalogReceipt{}, c.err
}
func (*catalogStub) CurrentRevision(context.Context, identitysdk.ApplicationRef) (identitysdk.CatalogReceipt, error) {
	return identitysdk.CatalogReceipt{}, nil
}

func TestOpenIdentityRequiresSaaSAndPublishesScopedCatalog(t *testing.T) {
	application := identitysdk.ApplicationRef{TenantID: "tenant-a", WorkspaceID: "workspace-a", ApplicationKey: "domainry-notification"}
	catalog := &catalogStub{}
	binding := &identityBindingStub{descriptor: identitysdk.Descriptor{ProtocolVersion: identitysdk.CurrentProtocolVersion, BundleVersion: identitysdk.CurrentPolicyBundleVersion, CatalogVersion: identitysdk.CatalogVersionV1, Mode: identitysdk.DeploymentModeSaaS, Issuer: "https://identity.example", Audience: string(application.ApplicationKey)}, catalog: catalog}
	factory := &identityFactoryStub{binding: binding}
	opened, err := OpenIdentity(t.Context(), IdentityOptions{Factory: factory, Application: application})
	if err != nil {
		t.Fatal(err)
	}
	if opened != binding || !sameApplication(factory.opened, application) {
		t.Fatalf("opened=%v application=%+v", opened, factory.opened)
	}
	if !sameApplication(catalog.validated.Application, application) || !sameApplication(catalog.published.Application, application) || len(catalog.published.Resources) != 9 || len(catalog.published.Actions) == 0 {
		t.Fatalf("catalog was not published with exact scope: %+v", catalog.published)
	}
}

func sameApplication(left, right identitysdk.ApplicationRef) bool {
	return left.TenantID == right.TenantID && left.WorkspaceID == right.WorkspaceID && left.ApplicationKey == right.ApplicationKey
}

func TestOpenIdentityFailsClosedForModuleAndCatalogFailure(t *testing.T) {
	application := identitysdk.ApplicationRef{TenantID: "tenant-a", WorkspaceID: "workspace-a", ApplicationKey: "domainry-notification"}
	for name, binding := range map[string]*identityBindingStub{
		"module":       {descriptor: identitysdk.Descriptor{ProtocolVersion: identitysdk.CurrentProtocolVersion, BundleVersion: identitysdk.CurrentPolicyBundleVersion, CatalogVersion: identitysdk.CatalogVersionV1, Mode: identitysdk.DeploymentModeModule, Issuer: "issuer", Audience: string(application.ApplicationKey)}, catalog: &catalogStub{}},
		"capabilities": {descriptor: identitysdk.Descriptor{ProtocolVersion: identitysdk.CurrentProtocolVersion, BundleVersion: identitysdk.CurrentPolicyBundleVersion, CatalogVersion: identitysdk.CatalogVersionV1, Mode: identitysdk.DeploymentModeSaaS, Issuer: "issuer", Audience: string(application.ApplicationKey)}},
		"catalog":      {descriptor: identitysdk.Descriptor{ProtocolVersion: identitysdk.CurrentProtocolVersion, BundleVersion: identitysdk.CurrentPolicyBundleVersion, CatalogVersion: identitysdk.CatalogVersionV1, Mode: identitysdk.DeploymentModeSaaS, Issuer: "issuer", Audience: string(application.ApplicationKey)}, catalog: &catalogStub{err: errors.New("identity unavailable")}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := OpenIdentity(t.Context(), IdentityOptions{Factory: &identityFactoryStub{binding: binding}, Application: application}); err == nil {
				t.Fatal("invalid Identity binding was accepted")
			}
			if !binding.closed {
				t.Fatal("rejected Identity binding was not closed")
			}
		})
	}
}

func TestIdentityOptionsFromEnvironmentSelectsRemoteIdentity(t *testing.T) {
	t.Setenv("NOTIFICATION_TENANT_ID", "tenant-a")
	t.Setenv("NOTIFICATION_IDENTITY_WORKSPACE_ID", "workspace-a")
	t.Setenv("NOTIFICATION_IDENTITY_APPLICATION_KEY", "domainry-notification")
	options := IdentityOptionsFromEnvironment()
	if options.Factory == nil || options.Application.TenantID != "tenant-a" || options.Application.WorkspaceID != "workspace-a" || options.Application.ApplicationKey != "domainry-notification" {
		t.Fatalf("options=%+v", options)
	}
}
