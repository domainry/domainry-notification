package server

import (
	"context"
	"testing"

	identitysdk "github.com/domainry/domainry-identity-sdk"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
)

type applicationFactoryStub struct {
	opened   []notificationsdk.ApplicationRef
	bindings []*httpBindingStub
}

func (f *applicationFactoryStub) OpenSaaS(_ context.Context, application notificationsdk.ApplicationRef, identity identitysdk.Binding) (notificationsdk.Binding, error) {
	if identity == nil {
		panic("Identity Binding was not supplied")
	}
	f.opened = append(f.opened, application)
	binding := &httpBindingStub{publisher: &httpPublisherStub{}}
	f.bindings = append(f.bindings, binding)
	return binding, nil
}

func TestRuntimeOwnsOneBindingPerExactApplicationAndClosesIdentity(t *testing.T) {
	identityApplication := identitysdk.ApplicationRef{TenantID: "tenant-service", WorkspaceID: "workspace-service", ApplicationKey: "domainry-notification"}
	identity := &identityBindingStub{descriptor: identitysdk.Descriptor{ProtocolVersion: identitysdk.CurrentProtocolVersion, BundleVersion: identitysdk.CurrentPolicyBundleVersion, CatalogVersion: identitysdk.CatalogVersionV1, Mode: identitysdk.DeploymentModeSaaS, Issuer: "https://identity.example", Audience: string(identityApplication.ApplicationKey)}, catalog: &catalogStub{}}
	factory := &applicationFactoryStub{}
	runtime, err := Open(t.Context(), Options{Identity: IdentityOptions{Factory: &identityFactoryStub{binding: identity}, Application: identityApplication}, Applications: factory})
	if err != nil {
		t.Fatal(err)
	}
	first := notificationsdk.ApplicationRef{TenantID: "tenant-a", WorkspaceID: "workspace-a", ApplicationKey: "runtime-a"}
	second := notificationsdk.ApplicationRef{TenantID: "tenant-a", WorkspaceID: "workspace-a", ApplicationKey: "runtime-b"}
	firstBinding, err := runtime.Resolve(t.Context(), first)
	if err != nil {
		t.Fatal(err)
	}
	reused, err := runtime.Resolve(t.Context(), first)
	if err != nil || reused != firstBinding {
		t.Fatalf("binding was not reused: %v %v", reused, err)
	}
	if _, err := runtime.Resolve(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	if len(factory.opened) != 2 || len(factory.bindings) != 2 || runtime.Handler() == nil {
		t.Fatalf("opened=%v bindings=%d handler=%v", factory.opened, len(factory.bindings), runtime.Handler())
	}
	if err := runtime.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !identity.closed || factory.bindings[0].closed != 1 || factory.bindings[1].closed != 1 {
		t.Fatalf("identityClosed=%v bindingClosed=%d/%d", identity.closed, factory.bindings[0].closed, factory.bindings[1].closed)
	}
	if _, err := runtime.Resolve(t.Context(), first); err == nil {
		t.Fatal("closed Runtime resolved an application")
	}
}
