package saas

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/domainry/domainry-foundation/worker"
	identitysdk "github.com/domainry/domainry-identity-sdk"
	notificationsdk "github.com/domainry/domainry-notification-sdk"
	notificationidentity "github.com/domainry/domainry-notification/internal/adapter/identitysdk"
	notificationhttp "github.com/domainry/domainry-notification/internal/transport/http/saas"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// ApplicationFactory opens one Notification application against
// service-owned, workspace-scoped persistence. Implementations must never borrow
// a Runtime database and must preserve workspace/application isolation.
type ApplicationFactory interface {
	OpenSaaS(context.Context, notificationsdk.ApplicationRef, identitysdk.Binding) (notificationsdk.Binding, error)
}

type IdentityOptions = notificationidentity.Options
type ServiceRequest = notificationidentity.ServiceRequest
type ServiceAuthority = notificationidentity.ServiceAuthority
type ServiceAuthenticator = notificationidentity.ServiceAuthenticator
type Handler = notificationhttp.Handler

func IdentityOptionsFromEnvironment() IdentityOptions {
	return notificationidentity.OptionsFromEnvironment()
}

func OpenIdentity(ctx context.Context, options IdentityOptions) (identitysdk.Binding, error) {
	return notificationidentity.Open(ctx, options)
}

func NewServiceAuthenticator(binding identitysdk.Binding) (*ServiceAuthenticator, error) {
	return notificationidentity.NewServiceAuthenticator(binding)
}

func NewHandler(authenticator notificationhttp.ServiceAuthentication, bindings notificationhttp.BindingResolver) (*Handler, error) {
	return notificationhttp.NewHandler(authenticator, bindings)
}

type applicationFactoryCloser interface {
	Close(context.Context) error
}

type Options struct {
	Identity     notificationidentity.Options
	Applications ApplicationFactory
	Workers      WorkerOptions
	Metrics      *notificationhttp.OperationalMetrics
}

type WorkerOptions struct {
	PollInterval time.Duration
	BatchSize    int
	OnError      func(error)
	Disabled     bool
}

type Runtime struct {
	identity      identitysdk.Binding
	factory       ApplicationFactory
	handler       *notificationhttp.Handler
	mu            sync.Mutex
	bindings      map[string]notificationsdk.Binding
	closed        bool
	cancel        context.CancelFunc
	workers       sync.WaitGroup
	ready         atomic.Bool
	workerOptions WorkerOptions
	metrics       *notificationhttp.OperationalMetrics
}

func Open(ctx context.Context, options Options) (*Runtime, error) {
	if options.Applications == nil {
		return nil, fmt.Errorf("Notification SaaS application factory is required")
	}
	identity, err := notificationidentity.Open(ctx, options.Identity)
	if err != nil {
		return nil, err
	}
	authenticator, err := notificationidentity.NewServiceAuthenticator(identity)
	if err != nil {
		_ = identity.Close(ctx)
		return nil, err
	}
	workerOptions := options.Workers
	if workerOptions.PollInterval <= 0 {
		workerOptions.PollInterval = time.Second
	}
	if workerOptions.BatchSize <= 0 {
		workerOptions.BatchSize = 100
	}
	workerContext, cancel := context.WithCancel(ctx)
	runtime := &Runtime{identity: identity, factory: options.Applications, bindings: map[string]notificationsdk.Binding{}, cancel: cancel, workerOptions: workerOptions, metrics: options.Metrics}
	handler, err := notificationhttp.NewHandler(authenticator, runtime)
	if err != nil {
		_ = identity.Close(ctx)
		return nil, err
	}
	runtime.handler = handler
	if workerOptions.Disabled {
		runtime.ready.Store(true)
	} else {
		runtime.workers.Add(1)
		go runtime.runWorkers(workerContext)
	}
	return runtime, nil
}

func (r *Runtime) Handler() *notificationhttp.Handler {
	if r == nil {
		return nil
	}
	return r.handler
}

func (r *Runtime) Ready() bool { return r != nil && r.ready.Load() }

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
	cancel := r.cancel
	r.cancel = nil
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
	if cancel != nil {
		cancel()
	}
	r.workers.Wait()
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

func (r *Runtime) runWorkers(ctx context.Context) {
	defer r.workers.Done()
	done := worker.StartNamedLoop(ctx, "notification", r.workerOptions.PollInterval, func() {
		r.processDue(ctx)
		r.ready.Store(true)
	})
	<-done
}

func (r *Runtime) processDue(ctx context.Context) {
	r.mu.Lock()
	bindings := make([]notificationsdk.Binding, 0, len(r.bindings))
	for _, binding := range r.bindings {
		bindings = append(bindings, binding)
	}
	r.mu.Unlock()
	for _, binding := range bindings {
		workers, local := binding.LocalWorkers()
		if !local || workers == nil {
			continue
		}
		for _, work := range []struct {
			kind string
			run  func(context.Context, int) (int, error)
		}{{"publication", workers.ProcessDuePublications}, {"inbox", workers.ProcessDueInboxEvents}, {"delivery", workers.ProcessDueChannelPlans}} {
			started := time.Now()
			workerContext, span := otel.Tracer("domainry.notification.worker").Start(ctx, "notification.worker."+work.kind)
			processed, err := work.run(workerContext, r.workerOptions.BatchSize)
			result := "success"
			if err != nil {
				result = "error"
				span.SetStatus(codes.Error, "worker failed")
			}
			span.SetAttributes(attribute.String("notification.worker.kind", work.kind), attribute.Int("notification.worker.processed", processed), attribute.String("notification.worker.result", result))
			span.End()
			r.metrics.ObserveWorker(work.kind, result, processed, time.Since(started))
			if err != nil && ctx.Err() == nil && r.workerOptions.OnError != nil {
				r.workerOptions.OnError(err)
			}
		}
	}
}

func applicationKey(application notificationsdk.ApplicationRef) string {
	return url.PathEscape(strings.TrimSpace(application.WorkspaceID)) + "/" + url.PathEscape(strings.TrimSpace(application.ApplicationKey))
}
