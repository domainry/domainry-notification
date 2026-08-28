package saas

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	identitysdk "github.com/domainry/domainry-identity-sdk"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
)

type applicationFactoryStub struct {
	opened   []notificationsdk.ApplicationRef
	bindings []*httpBindingStub
}

type workerBindingStub struct {
	notificationsdk.Binding
	workers *localWorkersStub
}

func (b *workerBindingStub) LocalWorkers() (notificationsdk.LocalWorkers, bool) {
	return b.workers, true
}
func (*workerBindingStub) Close(context.Context) error { return nil }

type localWorkersStub struct {
	notificationsdk.LocalWorkers
	runs atomic.Int32
	due  chan struct{}
}

func (w *localWorkersStub) ProcessDuePublications(context.Context, int) (int, error) {
	if w.runs.Add(1) == 1 {
		close(w.due)
	}
	return 0, nil
}
func (*localWorkersStub) ProcessDueInboxEvents(context.Context, int) (int, error)  { return 0, nil }
func (*localWorkersStub) ProcessDueChannelPlans(context.Context, int) (int, error) { return 0, nil }

type workerFactoryStub struct{ binding notificationsdk.Binding }

func (f workerFactoryStub) OpenSaaS(context.Context, notificationsdk.ApplicationRef, identitysdk.Binding) (notificationsdk.Binding, error) {
	return f.binding, nil
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

func TestRuntimeOwnsSaaSWorkerLifecycleAndReadiness(t *testing.T) {
	identityApplication := identitysdk.ApplicationRef{TenantID: "tenant-service", WorkspaceID: "workspace-service", ApplicationKey: "domainry-notification"}
	identity := &identityBindingStub{descriptor: identitysdk.Descriptor{ProtocolVersion: identitysdk.CurrentProtocolVersion, BundleVersion: identitysdk.CurrentPolicyBundleVersion, CatalogVersion: identitysdk.CatalogVersionV1, Mode: identitysdk.DeploymentModeSaaS, Issuer: "https://identity.example", Audience: string(identityApplication.ApplicationKey)}, catalog: &catalogStub{}}
	workers := &localWorkersStub{due: make(chan struct{})}
	runtime, err := Open(t.Context(), Options{
		Identity:     IdentityOptions{Factory: &identityFactoryStub{binding: identity}, Application: identityApplication},
		Applications: workerFactoryStub{binding: &workerBindingStub{workers: workers}},
		Workers:      WorkerOptions{PollInterval: time.Millisecond, BatchSize: 7},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Resolve(t.Context(), notificationsdk.ApplicationRef{TenantID: "tenant", WorkspaceID: "workspace", ApplicationKey: "application"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-workers.due:
	case <-time.After(time.Second):
		t.Fatal("Notification SaaS workers did not scan the opened application")
	}
	deadline := time.Now().Add(time.Second)
	for !runtime.Ready() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !runtime.Ready() {
		t.Fatal("Notification SaaS Runtime did not become ready after its worker scan")
	}
	if err := runtime.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	runs := workers.runs.Load()
	time.Sleep(5 * time.Millisecond)
	if workers.runs.Load() != runs {
		t.Fatal("Notification SaaS workers continued after close")
	}
}
