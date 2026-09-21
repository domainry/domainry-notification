# How should an event become a governed notification intent?

## Problems solved

- Converts a source-owned event into governed recipient intent and published message content without moving event truth into Notification.

## Business scenarios

- Rendering an approved template when Workflow assigns an approval task.
- Routing invoice-overdue or account-risk events to resolved recipients under a reviewed notification rule.
- Rendering a recipient's locale through an explicit fallback chain and rejecting a template whose required variable is absent.
- Publishing or rolling back a template version without rewriting evidence on previously created intents.

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
| Recipient requests `zh-CN`, while only `zh` and the declared default exist | Locale resolution with explicit fallback | Resolve in the published fallback order, record the locale/template revision actually used, and fail if a required variable is missing | Sending half-rendered content, silently using an unrelated locale, or fetching undeclared fields during render |
| A Handler-emitted event links back to its business record | `project_record` semantic route with explicit `route_key` | Declare the Action reference and exact route key in the published event type; validate the referenced Action during authoring | Inferring a route from the event name or treating any string as a client URL |
| Recipient cannot be resolved | Explicit suppressed/failed intent outcome | Record why routing failed, expose remediation, and do not call a provider with a guessed address | Falling back to a broad administrator list without policy |

## Example

After `order.approve` commits, its bounded Handler emits event type `order.approved` with stable event identity `order.approved:order-8842:v7`, declared variables `order_no`, `result`, and applicant identity, plus a `project_record` semantic route whose explicit `route_key` is `orders.detail`. Notification matches the published rule, resolves the applicant, selects the recipient locale (then the declared fallback), renders the published template revision, and creates one idempotent intent. A missing `order_no` rejects rendering; a retry reuses the intent identity; a later template rollback affects new intents only. Integration sees only the resulting external-channel delivery request and never chooses the recipient.

## Permissions and scope

Event publication belongs to the source’s bounded service authority. Rule/template draft, review, publish, disable, and restore use separate administrative permissions.

## Boundaries

Notification cannot invent source events or bypass business transactions. Definitions use only the published event-type, rule, and template authoring contracts.
