# How should preferences, digests, retries, and external channels compose?

## Problems solved

- Applies recipient preferences, digest policy, retry behavior, and channel selection while keeping provider protocol outside notification intent.

## Business scenarios

- Persisting an Inbox item immediately and sending the recipient's daily email digest later.
- Retrying email, SMS, or push delivery through Integration without duplicating the original notification.
- Enforcing a mandatory security channel even when ordinary recipient preferences would suppress it.
- Ending one permanently rejected channel without erasing success on Inbox or other channels.

## Use when

Use delivery policy when intents must respect recipient preferences, channel availability, retry limits, quiet periods, or digest timing.

## Do not use when

Do not select Notification for a provider call with no human recipient/message. Do not put recipient policy into Integration.

## How to use

Notification selects allowed channels and timing. Integration performs the external provider protocol. Scheduler may wake digest work, but Notification owns which intents belong in the digest.

## Adaptation cookbook

| Business requirement | Adapt with | Concrete implementation | Wrong adaptation |
| --- | --- | --- | --- |
| Inbox item appears immediately but email is sent in the recipient's daily digest | One notification intent with Inbox delivery and digest-eligible email delivery | Persist the Inbox item now; group eligible email content by recipient/timezone at digest time; preserve one logical notification identity | Creating unrelated notifications for each channel or delaying the Inbox item until the digest |
| Recipient disables SMS but keeps critical email | Preference and policy resolution before channel delivery | Combine recipient preference with mandatory policy and event severity, record selected/suppressed channels, then dispatch allowed external channels | Letting the SMS provider decide preferences or silently dropping a mandatory message |
| Email provider returns a transient failure | Integration retry tied to the same delivery attempt | Reuse stable delivery identity, apply bounded backoff, and update delivery state without recreating notification intent | Creating a new notification on every retry and producing duplicate Inbox items |
| Provider permanently rejects an address | Terminal delivery outcome plus actionable recipient/channel state | Record the provider reason, stop automatic retries, expose remediation, and keep other allowed channels independent | Retrying forever or marking the entire logical notification failed when Inbox succeeded |

## Example

Intent `approval.assigned:task-77:user-42` creates its Inbox delivery immediately. The recipient's email preference permits digest delivery in `Asia/Shanghai`, so Notification groups it under the daily digest while keeping the same logical identity. An SMS preference suppresses ordinary events, but a published mandatory policy may still require SMS for a critical security event after severity/policy evaluation. Scheduler only wakes the digest window; Notification selects members and timing; Integration invokes the chosen Provider connection. A transient timeout retries the same delivery identity, while an invalid email address ends only that channel with remediation evidence and leaves the Inbox success intact.

## Permissions and scope

Recipients manage only their preferences. Operators may inspect delivery evidence through explicit operational permissions but cannot read unrelated message content by default.

## Boundaries

Integration owns credentials and provider responses; Scheduler owns the clock; Notification owns recipient/channel policy and durable delivery state. Installed dynamic keys such as `notification.channel.*` and `notification.provider.*` describe available adapters only; they do not override rule, preference, severity, or mandatory-policy selection.
