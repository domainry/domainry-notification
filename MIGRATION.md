# Plane migration contract

This is an incremental source migration, not a data move. Plane and this module
currently use the same fourteen logical tables. During migration there must be
one writer implementation for each lifecycle; old Plane repositories and
The internal SQL store must never process the same durable work concurrently.

## Composition map

| Plane responsibility | Module entry point | Responsibility retained by Plane |
| --- | --- | --- |
| template administration | internal template domain service | HTTP authorization and DTO mapping |
| scheduled template publication | internal template publication service | worker scheduling and wakeups |
| event-type and rule catalog | internal inbox catalog service | source modules contribute definitions at startup |
| producer notification intents | Notification SDK `Publisher` | source transaction and after-commit wakeup |
| event materialization | internal inbox processor | audience and recipient-locale adapters |
| inbox reads and recipient state | Notification SDK `Inbox` | principal, reporting tree, delegated-scope composition |
| semantic action resolution | internal inbox action service | resource authorization and host routing |
| recipient delivery decisions | internal delivery policy service | identity and connector availability inputs |
| channel-plan execution | internal delivery processor | worker scheduling and Integration Outbox dispatcher |
| notification persistence | internal SQL adapter | database handle, dialect, workspace context and migration ledger |

## Required host adapters

Plane must provide these adapters without moving their source ownership into the
module:

- internal inbox audience and recipient-locale ports backed by current
  identity, workflow, record, or other source-owned queries;
- `notification.WorkNotifier` backed by the single Runtime worker scheduler;
- the internal delivery dispatcher port backed by the Integration Outbox. It may select a
  connector connection, but credential loading and provider payload compilation
  remain Connector responsibilities;
- action resource authorizers keyed by `ResolvedAction.RouteParams["resource_type"]`;
- database dialect, workspace context, clock, and existing migration ledger for
  the internal SQL adapter.

The module receives identifiers and authorized recipient sets. It must not
import Plane principals, system scopes, request contexts, worker types,
Integration aggregates, or Connector SDK/provider packages.

## Safe cutover order

1. Add the module dependency and Plane adapters while old code remains the only
   production path. Compare catalog and schema ownership in tests.
2. Baseline schema migration version 1 only after proving all fourteen existing
   tables, required columns, compatible types, and indexes are present. Do not
   execute the version-1 create statements over an existing Plane schema.
3. Switch read-only template, governance, and mailbox queries to module entry
   points. Keep writes on the old path until parity tests pass.
4. Switch synchronous writes: template drafts, preferences, mailbox mutations,
   saved views, and delegations. Each lifecycle changes owner in one commit.
5. Quiesce notification workers, switch event materialization, publication, and
   channel-plan processing to module processors, then restart the single worker
   runtime. Never run old and new processors together.
6. Switch source producers to the Notification SDK `Publisher` or its transaction-compatible
   event writer. Wake workers only after the source transaction commits.
7. Remove Plane notification domain models, repositories, validation, and SQL
   implementations only after imports and compatibility tests prove no caller
   remains. Keep HTTP and host adapters in Plane.

## Compatibility matrix

Every cutover must keep these observable contracts stable:

| Contract | Verification |
| --- | --- |
| JSON event, inbox, template, policy, and plan shapes | golden wire-format tests against current Plane payloads |
| stable error code and semantic error kind | table tests mapped to Plane HTTP errors |
| all logical tables and tenancy classifications | `module.SchemaOwnership` comparison |
| SQLite, PostgreSQL, and MySQL DDL | migration snapshot and fresh-database tests |
| event, publication, reservation, and plan fencing | concurrent claim and stale-writer tests |
| personal/team/delegated mailbox scope | query and mutation authorization tests |
| provider-neutral dispatch payload | adapter contract tests; no provider SDK imports |
| startup event/action registration | catalog conflict, missing-route, and missing-authorizer tests |

## Rollback

Before old implementations are deleted, rollback is a code-path switch after
quiescing workers. No reverse data migration is required because logical tables
and wire values remain shared. After old code removal, rollback requires
deploying the prior Plane build; schema version 1 is additive and must not be
deleted or re-created.
