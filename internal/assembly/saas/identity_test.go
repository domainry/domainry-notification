package saas

import (
	"context"
	"errors"
	"testing"

	"github.com/domainry/domainry-foundation/modulecapability"
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
	modulecapability.Binding
	descriptor   identitysdk.Descriptor
	applications *applicationRegistryStub
	permissions  identitysdk.PermissionRegistry
	tokens       identitysdk.TokenVerifier
	authorize    identitysdk.Authorization
	services     identitysdk.ApplicationServiceAuthentication
	closed       bool
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
func (*identityBindingStub) Projection() identitysdk.Projection { return projectionStub{} }
func (b *identityBindingStub) Applications() identitysdk.ApplicationRegistry {
	return b.applications
}
func (b *identityBindingStub) Permissions() identitysdk.PermissionRegistry { return b.permissions }
func (*identityBindingStub) Credentials() identitysdk.CredentialManager    { return nil }
func (b *identityBindingStub) ApplicationServices() identitysdk.ApplicationServiceAuthentication {
	if b.services == nil {
		return applicationServicesStub{}
	}
	return b.services
}
func (b *identityBindingStub) Close(context.Context) error { b.closed = true; return nil }

type tokenVerifierStub struct{}

type applicationServicesStub struct{}

func (applicationServicesStub) Exchange(context.Context, identitysdk.ExchangeApplicationServiceTokenRequest) (identitysdk.ApplicationServiceToken, error) {
	return identitysdk.ApplicationServiceToken{}, nil
}
func (applicationServicesStub) Verify(context.Context, identitysdk.VerifyApplicationServiceTokenRequest) (identitysdk.ApplicationServicePrincipal, error) {
	return identitysdk.ApplicationServicePrincipal{}, nil
}

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

type projectionStub struct{}

func (projectionStub) FindUser(context.Context, identitysdk.UserLookup) (identitysdk.User, bool, error) {
	return identitysdk.User{}, false, nil
}
func (projectionStub) FindOrganizationUnit(context.Context, identitysdk.OrganizationUnitLookup) (identitysdk.OrganizationUnit, bool, error) {
	return identitysdk.OrganizationUnit{}, false, nil
}
func (projectionStub) ListUsers(context.Context, identitysdk.ProjectionQuery) ([]identitysdk.User, error) {
	return nil, nil
}
func (projectionStub) ListRoles(context.Context, identitysdk.ProjectionQuery) ([]identitysdk.Role, error) {
	return nil, nil
}
func (projectionStub) ListUserRoleAssignments(context.Context, identitysdk.UserRoleAssignmentQuery) ([]identitysdk.UserRoleAssignment, error) {
	return nil, nil
}

type applicationRegistryStub struct {
	registered identitysdk.ApplicationRegistration
	err        error
}

func (stub *applicationRegistryStub) Register(_ context.Context, value identitysdk.ApplicationRegistration) (identitysdk.ApplicationRegistrationReceipt, error) {
	stub.registered = value
	return identitysdk.ApplicationRegistrationReceipt{Application: value.Application, RedirectURLs: value.RedirectURLs, Status: "active"}, stub.err
}

type permissionRegistryStub struct {
	reconciled                 identitysdk.PermissionReconcileRequest
	err                        error
	mutate                     func(*identitysdk.PermissionReconcileReceipt)
	snapshot                   identitysdk.PermissionSourceSnapshot
	snapshotErr                error
	loseFirstReconcileResponse bool
	reconcileCalls             int
}

func (stub *permissionRegistryStub) CurrentSourceSnapshot(_ context.Context, request identitysdk.PermissionSourceSnapshotRequest) (identitysdk.PermissionSourceSnapshot, error) {
	snapshot := stub.snapshot
	if snapshot.WorkspaceID == "" {
		snapshot.WorkspaceID = request.Application.WorkspaceID
	}
	if snapshot.SourceOwner == "" {
		snapshot.SourceOwner = request.SourceOwner
	}
	return snapshot, stub.snapshotErr
}

func (stub *permissionRegistryStub) Reconcile(_ context.Context, value identitysdk.PermissionReconcileRequest) (identitysdk.PermissionReconcileReceipt, error) {
	stub.reconciled = value
	stub.reconcileCalls++
	if stub.loseFirstReconcileResponse {
		stub.loseFirstReconcileResponse = false
		stub.snapshot = identitysdk.PermissionSourceSnapshot{
			WorkspaceID: value.Application.WorkspaceID, SourceOwner: value.SourceOwner,
			SnapshotHash: value.SnapshotHash, Definitions: append([]identitysdk.PermissionDefinition(nil), value.Definitions...),
		}
		return identitysdk.PermissionReconcileReceipt{}, errors.New("response lost after Identity committed")
	}
	receipt := identitysdk.PermissionReconcileReceipt{
		WorkspaceID: value.Application.WorkspaceID, SourceOwner: value.SourceOwner,
		PreviousSnapshotHash: value.PreviousSnapshotHash, SnapshotHash: value.SnapshotHash,
		DefinitionCount: len(value.Definitions), Inserted: len(value.Definitions),
	}
	if stub.mutate != nil {
		stub.mutate(&receipt)
	}
	return receipt, stub.err
}

func TestOpenRetriesLostReconcileResponseFromAuthoritativeSnapshot(t *testing.T) {
	application := identitysdk.ApplicationRef{TenantID: "tenant-a", WorkspaceID: "workspace-a", ApplicationKey: "domainry-notification"}
	permissions := &permissionRegistryStub{loseFirstReconcileResponse: true}
	binding := &identityBindingStub{
		descriptor:   identitysdk.Descriptor{ProtocolVersion: identitysdk.CurrentProtocolVersion, BundleVersion: identitysdk.CurrentPolicyBundleVersion, AuthorizationVersion: identitysdk.CurrentAuthorizationContractVersion, Mode: identitysdk.DeploymentModeSaaS, Issuer: "https://identity.example", Audience: string(application.ApplicationKey)},
		applications: &applicationRegistryStub{}, permissions: permissions,
	}
	factory := &identityFactoryStub{binding: binding}
	if _, err := OpenIdentity(t.Context(), IdentityOptions{Factory: factory, Application: application}); err == nil {
		t.Fatal("lost first reconcile response did not fail startup")
	}
	committedHash := permissions.snapshot.SnapshotHash
	if committedHash == "" {
		t.Fatal("Identity-side committed snapshot was not recorded by the test boundary")
	}
	binding.closed = false
	opened, err := OpenIdentity(t.Context(), IdentityOptions{Factory: factory, Application: application})
	if err != nil {
		t.Fatal(err)
	}
	if opened != binding || permissions.reconcileCalls != 2 || permissions.reconciled.PreviousSnapshotHash != committedHash || permissions.reconciled.SnapshotHash != committedHash {
		t.Fatalf("response-loss retry calls=%d request=%+v committed=%q", permissions.reconcileCalls, permissions.reconciled, committedHash)
	}
}

func TestOpenRequiresSaaSAndRegistersScopedApplicationPermissions(t *testing.T) {
	application := identitysdk.ApplicationRef{TenantID: "tenant-a", WorkspaceID: "workspace-a", ApplicationKey: "domainry-notification"}
	previousDefinitions := []identitysdk.PermissionDefinition{{
		PermissionKey: "notification.templates.list", ResourceKey: "notification.templates", OperationKey: "list",
		Label: "List notification templates", Category: "Notification", SourceKind: "module_http",
	}}
	previousHash, err := identitysdk.PermissionSnapshotHash("module:notification", previousDefinitions)
	if err != nil {
		t.Fatal(err)
	}
	applications, permissions := &applicationRegistryStub{}, &permissionRegistryStub{snapshot: identitysdk.PermissionSourceSnapshot{
		WorkspaceID: application.WorkspaceID, SourceOwner: "module:notification", SnapshotHash: previousHash, Definitions: previousDefinitions,
	}}
	binding := &identityBindingStub{descriptor: identitysdk.Descriptor{ProtocolVersion: identitysdk.CurrentProtocolVersion, BundleVersion: identitysdk.CurrentPolicyBundleVersion, AuthorizationVersion: identitysdk.CurrentAuthorizationContractVersion, Mode: identitysdk.DeploymentModeSaaS, Issuer: "https://identity.example", Audience: string(application.ApplicationKey)}, applications: applications, permissions: permissions}
	factory := &identityFactoryStub{binding: binding}
	opened, err := OpenIdentity(t.Context(), IdentityOptions{Factory: factory, Application: application})
	if err != nil {
		t.Fatal(err)
	}
	if opened != binding || !sameApplication(factory.opened, application) {
		t.Fatalf("opened=%v application=%+v", opened, factory.opened)
	}
	if !sameApplication(applications.registered.Application, application) || !sameApplication(permissions.reconciled.Application, application) || len(permissions.reconciled.Definitions) == 0 {
		t.Fatalf("application/permissions were not reconciled with exact scope: application=%+v permissions=%+v", applications.registered, permissions.reconciled)
	}
	for _, definition := range permissions.reconciled.Definitions {
		if definition.PermissionKey != definition.ResourceKey+"."+definition.OperationKey {
			t.Fatalf("permission is not exact: %+v", definition)
		}
	}
	if permissions.reconciled.SourceOwner != "module:notification" || permissions.reconciled.SnapshotHash == "" {
		t.Fatalf("permission snapshot owner/hash=%q/%q", permissions.reconciled.SourceOwner, permissions.reconciled.SnapshotHash)
	}
	if permissions.reconciled.PreviousSnapshotHash != previousHash {
		t.Fatalf("permission reconcile previous hash=%q want=%q", permissions.reconciled.PreviousSnapshotHash, previousHash)
	}
}

func sameApplication(left, right identitysdk.ApplicationRef) bool {
	return left.TenantID == right.TenantID && left.WorkspaceID == right.WorkspaceID && left.ApplicationKey == right.ApplicationKey
}

func TestOpenFailsClosedForModuleAndRegistrationFailure(t *testing.T) {
	application := identitysdk.ApplicationRef{TenantID: "tenant-a", WorkspaceID: "workspace-a", ApplicationKey: "domainry-notification"}
	for name, binding := range map[string]*identityBindingStub{
		"module":           {descriptor: identitysdk.Descriptor{ProtocolVersion: identitysdk.CurrentProtocolVersion, BundleVersion: identitysdk.CurrentPolicyBundleVersion, AuthorizationVersion: identitysdk.CurrentAuthorizationContractVersion, Mode: identitysdk.DeploymentModeModule, Issuer: "issuer", Audience: string(application.ApplicationKey)}, applications: &applicationRegistryStub{}, permissions: &permissionRegistryStub{}},
		"capabilities":     {descriptor: identitysdk.Descriptor{ProtocolVersion: identitysdk.CurrentProtocolVersion, BundleVersion: identitysdk.CurrentPolicyBundleVersion, AuthorizationVersion: identitysdk.CurrentAuthorizationContractVersion, Mode: identitysdk.DeploymentModeSaaS, Issuer: "issuer", Audience: string(application.ApplicationKey)}},
		"registration":     {descriptor: identitysdk.Descriptor{ProtocolVersion: identitysdk.CurrentProtocolVersion, BundleVersion: identitysdk.CurrentPolicyBundleVersion, AuthorizationVersion: identitysdk.CurrentAuthorizationContractVersion, Mode: identitysdk.DeploymentModeSaaS, Issuer: "issuer", Audience: string(application.ApplicationKey)}, applications: &applicationRegistryStub{err: errors.New("identity unavailable")}, permissions: &permissionRegistryStub{}},
		"permissions":      {descriptor: identitysdk.Descriptor{ProtocolVersion: identitysdk.CurrentProtocolVersion, BundleVersion: identitysdk.CurrentPolicyBundleVersion, AuthorizationVersion: identitysdk.CurrentAuthorizationContractVersion, Mode: identitysdk.DeploymentModeSaaS, Issuer: "issuer", Audience: string(application.ApplicationKey)}, applications: &applicationRegistryStub{}, permissions: &permissionRegistryStub{err: errors.New("identity unavailable")}},
		"snapshot":         {descriptor: identitysdk.Descriptor{ProtocolVersion: identitysdk.CurrentProtocolVersion, BundleVersion: identitysdk.CurrentPolicyBundleVersion, AuthorizationVersion: identitysdk.CurrentAuthorizationContractVersion, Mode: identitysdk.DeploymentModeSaaS, Issuer: "issuer", Audience: string(application.ApplicationKey)}, applications: &applicationRegistryStub{}, permissions: &permissionRegistryStub{snapshotErr: errors.New("identity unavailable")}},
		"snapshot receipt": {descriptor: identitysdk.Descriptor{ProtocolVersion: identitysdk.CurrentProtocolVersion, BundleVersion: identitysdk.CurrentPolicyBundleVersion, AuthorizationVersion: identitysdk.CurrentAuthorizationContractVersion, Mode: identitysdk.DeploymentModeSaaS, Issuer: "issuer", Audience: string(application.ApplicationKey)}, applications: &applicationRegistryStub{}, permissions: &permissionRegistryStub{snapshot: identitysdk.PermissionSourceSnapshot{WorkspaceID: application.WorkspaceID, SourceOwner: "module:other"}}},
		"receipt": {descriptor: identitysdk.Descriptor{ProtocolVersion: identitysdk.CurrentProtocolVersion, BundleVersion: identitysdk.CurrentPolicyBundleVersion, AuthorizationVersion: identitysdk.CurrentAuthorizationContractVersion, Mode: identitysdk.DeploymentModeSaaS, Issuer: "issuer", Audience: string(application.ApplicationKey)}, applications: &applicationRegistryStub{}, permissions: &permissionRegistryStub{mutate: func(receipt *identitysdk.PermissionReconcileReceipt) {
			receipt.SourceOwner = "module:other"
		}}},
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
