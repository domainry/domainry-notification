# Architecture

This module owns notification semantics and Notification product HTTP handlers.
It does not own the host application's identity model, authorization model,
listener/middleware runtime, worker runtime, Integration Outbox, secrets, or
concrete Connector providers.

## DDD package structure

Notification follows the same dependency-oriented DDD structure as Identity
and Runtime. Deployment topology is not a domain boundary.

- `internal/domain/<capability>/model` owns domain state and value types;
- `internal/domain/<capability>/repository` owns persistence ports;
- `internal/domain/<capability>/service` owns domain behavior;
- `internal/application/<capability>` owns use-case orchestration, durable
  processors, leases, retries, and contracts shared by adapters;
- `internal/adapter/identitysdk` and `internal/adapter/notificationsdk` isolate
  external SDK and wire-contract conversion;
- `internal/infrastructure` implements persistence ports;
- `internal/transport/http/module` implements the topology-neutral product HTTP
  Adapter over the SDK Binding;
- `internal/transport/http/saas` implements the standalone service protocol;
- `internal/assembly/module` and `internal/assembly/saas` are the two composition
  roots over the same domain implementation;
- public `module` is the narrow in-process Factory and schema contract facade;
- `cmd/notification-server` is the standalone SaaS process entry point.

No domain or application package may import infrastructure, transport, assembly,
or the public Module facade. SDK wire contracts are converted at adapter
boundaries and do not define domain ownership.

## Dependency direction

```text
cmd / public module
  -> assembly
       -> transport / adapter / infrastructure / application
            -> domain service -> domain repository + domain model

domain never imports application, infrastructure, transport, or assembly
application never imports infrastructure, transport, or assembly
infrastructure and transport never import assembly
```

Durable inbox, delivery, and publication processors live in
`internal/application`. Domain services contain domain behavior and do not own
worker scheduling, claim leases, retry policy execution, or cross-capability
use-case orchestration.

The host owns authentication and translates its principal into explicit
workspace, actor, recipient, and adapter values before invoking this module.

The internal inbox mailbox service accepts that already-authorized query scope and owns
mailbox behavior. The internal action resolver only resolves catalog-backed semantic
actions into host route input. Team and delegated views are read-only; the host
must authorize the referenced business resource after resolution and before
navigation or execution.

## Transaction boundary

Notification events may be inserted inside a source owner's existing database
transaction. `sqlstore` therefore exposes a transaction-compatible event writer
using a minimal executor. The host owns commit, rollback, and after-commit worker
wakeups. This preserves current same-database atomicity without importing Plane's
transaction or worker packages.

## Schema tenancy

All seven notification-owned durable tables are workspace scoped: events,
deliveries, delivery attempts, inbox items, alert groups, delegations and typed
user settings. Notification accepts only an exact same-key Identity
`data_scope=all`, which deliberately adds no row-level data predicate; every
durable access still uses the bound workspace predicate. Templates and delivery
policies use shared Definitions, publication requests use shared Operations,
cutover state uses shared Operation Controls, and retention payloads use the
Lifecycle ArchiveStore. Embedded mode borrows that Artifact-backed port from
the host. Standalone SaaS installs the same canonical `_artifacts` and
`_artifact_bindings` tables through its one migration ledger and writes the
immutable JSON payload to the private BlobStore configured by
`NOTIFICATION_ARTIFACT_STORAGE_PATH`; SQL contains only the opaque storage
reference, hash, size, state, metadata and job/source bindings. There is no
standalone `_lifecycle_archive_entries` or database payload column. None of
those shared records appears as a Notification-owned table or in its portable
bundle.

`module.SchemaOwnership` is the public authoritative machine-readable inventory;
its implementation delegates to the internal SQL adapter.
Only workspace-scoped tables participate in host RLS and workspace-retirement
workflows. A table must not be moved between scopes as a naming-only refactor;
that is a data migration and authorization change.

`module.SchemaMigrations` is the authoritative ordered DDL history. The host
executes it with its existing migration ledger; notification does not create a
parallel migration-history table. A fresh deployment applies version 1. An
existing Plane database must first prove that all owned tables, columns,
indexes, and compatible types already exist, then baseline version 1 without
executing it. Table prefixes and PostgreSQL schemas are physical deployment
configuration and never change logical ownership names.

When separately deployed, source owners can replace that adapter with their own
transactional outbox while retaining the same notification intent.

## External delivery boundary

`delivery` owns channel-plan state and retry decisions. It emits an immutable
dispatch request through its `Dispatcher` interface. The host adapter writes it to the Integration
Outbox. Integration and Connector providers own provider execution, credentials,
provider retries, callbacks, and delivery ledgers.

The dispatch request carries provider-neutral rendered content. Connector-owned
code compiles Slack blocks, Feishu cards, Teams adaptive cards, WhatsApp payloads,
and equivalent provider formats; this module does not contain provider switches.

## Registration boundary

The module owns event-type, audience-resolver, action-authorizer, and provider-
capability registries. Definitions tied to Workflow, Report, Automation, Record,
Scheduler, or Integration are registered by those owners during host composition;
they are not built into this module.

See `MIGRATION.md` for the Plane composition map, cutover order, compatibility
matrix, and rollback boundary.
