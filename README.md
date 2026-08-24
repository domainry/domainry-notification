# Domainry Notification

`domainry-notification` is the source-owned Go module for Domainry notification
semantics. It is extracted from Domainry Plane and can be composed into a
Runtime without importing Plane internals.

## Ownership

The module owns:

- notification templates, versions, publication requests and publication locks;
- durable notification events and failure history;
- recipient inbox items, delegations, alert groups and saved views;
- recipient preferences, delivery policy, frequency reservations and channel plans;
- notification workers and transport-neutral application services.

The initial durable table contract is exposed by `notification.OwnedTables`.

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

The repository currently contains the extraction boundary and ownership
contract. Plane source will be migrated in dependency-closed increments.
