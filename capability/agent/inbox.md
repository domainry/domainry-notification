# How should Inbox read, unread, archive, and delegation work?

## Problems solved

- Gives every principal durable personal notification state and governed delegation without exposing arbitrary user identifiers to callers.

## Business scenarios

- An approver lists unread work messages, marks one read, and archives completed items.
- An assistant sees delegated Inbox items only during an active authorized delegation.

## Use when

Use Inbox when recipients need durable read/unread/archive state, saved views, or governed delegation.

## Do not use when

Do not persist every frontend toast. Do not let callers supply another recipient ID to read their Inbox.

## How to use

Resolve recipient from the Identity principal. Use bounded stable queries and explicit item-state operations. Delegation must be an Identity/governance fact, not a query parameter.

## Adaptation cookbook

| Business requirement | Adapt with | Concrete implementation | Wrong adaptation |
| --- | --- | --- | --- |
| Approver lists unread work messages and marks one read | Principal-scoped Inbox query and idempotent read transition | Derive principal from authenticated context, page results, and update only that principal's Inbox state | Accepting an arbitrary user ID from the client or storing read state only in browser storage |
| Completed notification should leave the active view but remain inspectable | Archive transition | Preserve the durable item and timestamps; default active queries exclude archived items while authorized history can include them | Hard-deleting the item or conflating archive with provider delivery success |
| Assistant handles a manager's Inbox during approved leave | Time-bounded governed delegation | Resolve active delegation server-side, restrict allowed actions/categories, and attribute the delegate action to both principals | Sharing login credentials or allowing permanent unrestricted impersonation |
| UI displays a transient success toast after saving | Client feedback, not Notification Inbox | Render ephemeral confirmation from the Operation result | Persisting every UI toast as a durable Inbox item |

## Example

An approver lists their unread items, marks one read, and archives it. An assistant sees delegated items only through an active governed delegation, never by changing `user_id`.

## Permissions and scope

Inbox query, item state, saved-view management, and delegation management are distinct permissions. The effective row scope is always the resolved recipient/delegate boundary.

## Boundaries

Inbox state is Notification-owned. It does not grant access to the underlying business record; opening that record triggers its own authorization.
