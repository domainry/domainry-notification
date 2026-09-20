# Domainry Notification

Agent-facing question index and source-owned guides: [`capability/agent/index.json`](capability/agent/index.json).

`domainry-notification` is the source-owned Go module for Domainry notification
semantics. It is extracted from Domainry Plane and can be composed into a
Runtime without importing Plane internals.

## Ownership

The module owns:

- notification templates, versions, publication requests and publication locks;
- durable notification events and failure history;
- recipient inbox items, delegations, alert groups and saved views;
- recipient preferences, delivery policy, frequency reservations and channel plans;
- durable-work processors and transport-neutral application services; the host
  retains worker scheduling, lifecycle, and wakeup transport.

The public `module` package exposes only the in-process Factory and the schema
contract required by a host. Domain, application, SQL, Identity, HTTP, and
composition implementations are internal.

## Host boundary

The host supplies explicit ports for identity lookup, workspace authorization,
clock and worker control, audit, transactions, wakeups and Connector delivery.
Concrete Web Push, SMTP, Slack and similar delivery implementations remain
Connector Providers. Integration outbox persistence remains owned by the host;
this module produces channel-plan delivery intents rather than importing the
Plane Integration aggregate.

## Development

```sh
make check
```

## Database schema

`module.SchemaMigrations` returns ordered SQL statements for SQLite,
PostgreSQL, or MySQL. Apply them through the host's migration runner. Fresh
databases start at version 1; databases that already contain Plane's
notification tables must verify schema compatibility before recording the
version-1 baseline. Do not execute the base migration over an existing schema.

`module.SchemaOwnership` identifies system-scoped and workspace-scoped tables
so RLS and workspace retirement apply only where appropriate.

See [ARCHITECTURE.md](ARCHITECTURE.md) for package naming, dependency direction,
transaction ownership, and the Integration/Connector boundary.
See [MIGRATION.md](MIGRATION.md) for the concrete Plane adapter map and safe
incremental cutover sequence.
