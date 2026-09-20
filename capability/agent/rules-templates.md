# How should an event become a governed notification intent?

## Problems solved

- Converts a source-owned event into governed recipient intent and published message content without moving event truth into Notification.

## Business scenarios

- Rendering an approved template when Workflow assigns an approval task.
- Routing invoice-overdue or account-risk events to resolved recipients under a reviewed notification rule.

## Use when

Use event types, rules, and templates when a source-owned event needs governed recipient/message/channel policy.

## Do not use when

Do not send SMTP/provider HTTP from the source Handler. Do not let a template decide whether the business event is valid.

## How to use

The source publishes a typed event after its authoritative mutation. Notification matches a published rule, resolves recipients, renders an approved template, and creates one idempotent intent.

## Adaptation cookbook

| Business requirement | Adapt with | Concrete implementation | Wrong adaptation |
| --- | --- | --- | --- |
| Workflow assigns an approval task | Source event plus Notification rule, recipient resolver, and published template | Workflow owns assignment truth; the rule matches the committed event, resolves assignee, renders approved variables, and creates one intent | Letting Notification create or change the task assignment |
| Invoice becomes overdue and account owner must be warned | Invoice-owned overdue event plus reviewed routing rule | Billing owns overdue calculation; Notification resolves account owner and permitted channels from stable event fields | Polling invoice tables from a template or embedding overdue business logic in message text |
| Template wording changes without changing the event contract | Versioned template publication | Validate declared variables, preview content, publish a new version, and retain the rendered/version evidence used for sent messages | Editing already-delivered content in place or allowing undeclared arbitrary object access during rendering |
| Recipient cannot be resolved | Explicit suppressed/failed intent outcome | Record why routing failed, expose remediation, and do not call a provider with a guessed address | Falling back to a broad administrator list without policy |

## Example

Workflow emits “approval task assigned”. Notification resolves the assignee, renders the published approval template, and creates an Inbox item. Workflow still owns task validity.

## Permissions and scope

Event publication belongs to the source’s bounded service authority. Rule/template draft, review, publish, disable, and restore use separate administrative permissions.

## Boundaries

Notification cannot invent source events or bypass business transactions. Definitions use only the published event-type, rule, and template authoring contracts.
