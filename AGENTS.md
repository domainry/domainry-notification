# Domainry Notification development guide

- This repository is an independent Go module and must not import `domainry-plane/internal/**`.
- This repository must not depend on concrete Connector Provider packages.
- Plane composes this module through public contracts; Plane identity, workspace authorization, audit, transaction coordination, secrets, and Connector execution remain host capabilities.
- Notification owns notification domain semantics, its durable state, workers, and transport-neutral application services.
- Do not copy Plane packages wholesale. Migrate one dependency-closed slice at a time and replace Plane-internal types with module-owned values or explicit host ports.
- Define host interfaces in the capability package that consumes them. Do not create generic top-level `port`, `contract`, `service`, `model`, or `repository` packages.
- Keep schema migrations under `internal/infrastructure/persistence`; do not expose persistence ownership from the root domain package.
- Keep SDK wire conversion under `internal/adapter`; domain packages must not import Notification or Identity SDK packages.
- Keep durable processors and worker orchestration under `internal/application`, not `internal/domain`.
