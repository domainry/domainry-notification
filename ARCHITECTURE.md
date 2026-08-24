# Architecture

This module owns notification semantics. It does not own the host application's
identity model, authorization model, worker runtime, HTTP transport, Integration
Outbox, secrets, or concrete Connector providers.

## Package vocabulary

- `notification` — stable domain values exchanged across capabilities and hosts;
- `template` — validation, publication, rendering, and catalog behavior;
- `inbox` — intent compilation, materialization, mailbox, alert, delegation,
  saved-view, and action behavior;
- `delivery` — policy evaluation and channel-plan orchestration up to dispatch;
- `sqlstore` — SQL persistence for state owned by this module;

Do not introduce packages named `model`, `service`, `contract`, `repository`,
`common`, `util`, or `runtime`. Those names hide cohesion and recreate Plane's
horizontal package structure. Capability packages may use those words for
unexported filenames without making them import paths.

## Dependency direction

```text
host (Plane or a future service)
  -> inbox / template / delivery
       -> notification
  -> sqlstore
       -> notification

host adapters implement interfaces declared by the capability that consumes them
sqlstore never imports host code
notification never imports subpackages
```

The host owns authentication and translates its principal into explicit
workspace, actor, recipient, and surface values before invoking this module.

## Transaction boundary

Notification events may be inserted inside a source owner's existing database
transaction. `sqlstore` therefore exposes a transaction-compatible event writer
using a minimal executor. The host owns commit, rollback, and after-commit worker
wakeups. This preserves current same-database atomicity without importing Plane's
transaction or worker packages.

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
