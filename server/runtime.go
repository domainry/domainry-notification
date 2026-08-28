package server

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	identitysdk "github.com/domainry/domainry-identity-sdk"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
)

// ApplicationFactory opens one Notification application against
// service-owned, tenant-scoped persistence. Implementations must never borrow
// a Runtime database and must preserve tenant/workspace/application isolation.
type ApplicationFactory interface {
	OpenSaaS(context.Context, notificationsdk.ApplicationRef, identitysdk.Binding) (notificationsdk.Binding, error)
}

type applicationFactoryCloser interface {
	Close(context.Context) error
}

type Options struct {
	Identity     IdentityOptions
	Applications ApplicationFactory
}

type Runtime struct {
	identity identitysdk.Binding
	factory  ApplicationFactory
	handler  *Handler
	mu       sync.Mutex
	bindings map[string]notificationsdk.Binding
	closed   bool
}

func Open(ctx context.Context, options Options) (*Runtime, error) {
	if options.Applications == nil {
		return nil, fmt.Errorf("Notification SaaS application factory is required")
	}
	identity, err := OpenIdentity(ctx, options.Identity)
	if err != nil {
		return nil, err
	}
	authenticator, err := NewServiceAuthenticator(identity)
	if err != nil {
		_ = identity.Close(ctx)
		return nil, err
	}
	runtime := &Runtime{identity: identity, factory: options.Applications, bindings: map[string]notificationsdk.Binding{}}
	handler, err := NewHandler(authenticator, runtime)
	if err != nil {
		_ = identity.Close(ctx)
		return nil, err
	}
	runtime.handler = handler
	return runtime, nil
}

func (r *Runtime) Handler() *Handler {
	if r == nil {
		return nil
	}
	return r.handler
}

func (r *Runtime) Resolve(ctx context.Context, application notificationsdk.ApplicationRef) (notificationsdk.Binding, error) {
	if r == nil {
		return nil, fmt.Errorf("Notification SaaS Runtime is unavailable")
	}
	if err := application.Validate(); err != nil {
		return nil, err
	}
	key := applicationKey(application)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, fmt.Errorf("Notification SaaS Runtime is closed")
	}
	if binding := r.bindings[key]; binding != nil {
		return binding, nil
	}
	binding, err := r.factory.OpenSaaS(ctx, application, r.identity)
	if err != nil {
		return nil, fmt.Errorf("open Notification SaaS application %s: %w", key, err)
	}
	if binding == nil {
		return nil, fmt.Errorf("Notification SaaS application Factory returned no Binding")
	}
	r.bindings[key] = binding
	return binding, nil
}

func (r *Runtime) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	keys := make([]string, 0, len(r.bindings))
	for key := range r.bindings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	bindings := make([]notificationsdk.Binding, 0, len(keys))
	for _, key := range keys {
		bindings = append(bindings, r.bindings[key])
	}
	r.bindings = nil
	identity := r.identity
	factory := r.factory
	r.identity = nil
	r.factory = nil
	r.mu.Unlock()
	errs := make([]error, 0, len(bindings)+2)
	for _, binding := range bindings {
		if err := binding.Close(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if closer, ok := factory.(applicationFactoryCloser); ok {
		if err := closer.Close(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if identity != nil {
		if err := identity.Close(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func applicationKey(application notificationsdk.ApplicationRef) string {
	return strings.TrimSpace(application.TenantID) + "/" + strings.TrimSpace(application.WorkspaceID) + "/" + strings.TrimSpace(application.ApplicationKey)
}
