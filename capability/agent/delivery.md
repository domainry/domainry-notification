# How should preferences, digests, retries, and external channels compose?

## Problems solved

- Applies recipient preferences, digest policy, retry behavior, and channel selection while keeping provider protocol outside notification intent.

## Business scenarios

- Persisting an Inbox item immediately and sending the recipient's daily email digest later.
- Retrying email, SMS, or push delivery through Integration without duplicating the original notification.

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

An approver prefers Inbox plus daily email digest. Notification persists the Inbox item immediately, groups email intent, and later requests Integration delivery through the configured provider.

## Permissions and scope

Recipients manage only their preferences. Operators may inspect delivery evidence through explicit operational permissions but cannot read unrelated message content by default.

## Boundaries

Integration owns credentials and provider responses; Scheduler owns the clock; Notification owns recipient/channel policy and durable delivery state.
