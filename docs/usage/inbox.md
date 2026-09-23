# How should Inbox read, unread, archive, and delegation work?

## Problems solved

- Gives every principal durable personal notification state and governed delegation without exposing arbitrary user identifiers to callers.

## Business scenarios

- An approver lists unread work messages, marks one read, and archives completed items.
- An assistant sees delegated Inbox items only during an active authorized delegation.
- A recipient saves an “unread approval reminders” view that stores filters but grants no additional message access.
- A transient save-success toast stays in the client, while the underlying Workflow task remains Workflow-owned.

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
| User saves an “unread approval reminders” filter | Principal-owned saved view | Store only the bounded filter/sort definition; apply the same recipient and delegation authorization on every execution | Treating the view as a shared Report or as authority to reveal messages outside the Inbox scope |
| UI displays a transient success toast after saving | Client feedback, not Notification Inbox | Render ephemeral confirmation from the Operation result | Persisting every UI toast as a durable Inbox item |

## Example

Principal `user-42` lists a stable page of their unread Inbox items, marks `item-7` read idempotently, and archives it without deleting history. Their saved view stores `unread=true` plus the approval category but does not store a recipient override. During an approved leave window, delegate `user-51` sees only allowed delegated categories; every read/archive action records both owner and delegate, and access ends immediately on expiry or revocation. Changing `user_id` in a request never selects another mailbox. The notification can point to a Workflow task, but the task's status and authorization remain Workflow-owned.

## Permissions and scope

Inbox query, item state, saved-view management, and delegation management are distinct permissions. The effective row scope is always the resolved recipient/delegate boundary.

## Boundaries

Inbox state is Notification-owned. It does not grant access to the underlying business record; opening that record triggers its own authorization.
