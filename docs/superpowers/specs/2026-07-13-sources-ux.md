# Sources UX — Paste Token to Merge

**Date:** 2026-07-13
**Status:** Spec
**Scope:** Web client (`client/`) — Settings page, notification bell, receipt list/detail badges, proposal review duplicate warning
**Implementation plan:** `docs/superpowers/plans/2026-07-13-external-receipt-sources.md` (Tasks 9, 10b, 11, 11b, 12, 13b)

## Problem

The implementation plan in `2026-07-13-external-receipt-sources.md` covers the data flow from PC Optimum's API into the local store: syncing, matching, reconciling. It does **not** cover the user-facing experience of getting from "I have a PC Optimum account" to "my receipts are showing up with UPCs and items already matched." Today that flow is undefined. A family member opening Settings for the first time has no idea what to do; the failure modes (token expired, account disabled, sync produced nothing) are invisible; the user has to remember to click "Sync now" forever.

This spec designs that end-to-end experience, drawing on the patterns used by real-world production apps that integrate third-party financial or loyalty data: **Plaid** (bank linking), **Mint / Monarch** (transaction aggregation), **Splitwise** (OCR receipt + matching), **Expensify** (receipt + corporate card reconciliation). The common threads are: an explicit, well-illustrated connection flow, a continuous sync that doesn't need user intervention, **clear and timely failure communication**, and a graceful way to deal with duplicates and mismatches.

## User stories (the flows we must support)

### Flow 1 — First-time connect

> Vlad has just installed the app. He wants to add his PC Optimum account so future grocery trips land in Grocer without him photographing receipts.

The flow is: Vlad logs in → clicks a profile/avatar in the nav → lands on Settings → sees an empty state ("No loyalty accounts yet") with a single primary button "Connect a loyalty account" → the button opens a **modal** (not a separate page) because the flow is short and modal keeps context → the modal has a source picker (just "PC Optimum" today) → after picking, a step shows instructions to copy the bearer token out of PC Optimum's web session → a paste field with a "Test connection" button → on success, a "Connected" success state with the account name and a "Sync now" button → on failure, a typed error ("Token expired or invalid — make sure you copied the whole `Bearer eyJ...` value").

Why a modal, not a full settings sub-page: the connection flow is linear and short (4 steps), and a modal can be dismissed without losing settings context. Plaid uses a modal stack for the same reason.

Why paste-token, not OAuth: the PC Optimum OAuth flow is undocumented and reversing the JS bundle is fragile. The paste flow is honest about what we're doing — we're using the user's session cookie-equivalent. We document the steps in the modal, not hide them.

### Flow 2 — Background sync, success

> Three days after connecting, Vlad has shopped at No Frills twice. The receipts are already in Grocer.

The flow is invisible. The background ticker (Task 10b) syncs every 15 minutes. The user only sees the result: new rows on the home page with a `🛒 PC Optimum` badge. No notification, no toast. Quiet success is the goal.

When the sync finds an existing photo receipt that matches (Task 13), the API items backfill UPC/LIAM onto the photo receipt's items. The user sees no UI for this; the item page just has UPCs now where it didn't before. Quiet enrichment is the goal.

### Flow 3 — Token expires

> Vlad's PC Optimum token has a 24h TTL. After it expires, the next sync fails with 401.

The flow is: ticker calls PC Optimum → 401 → manager marks account `auth_expired` and creates a notification → on Vlad's next page load, the bell icon in the nav shows a red badge with "1" → clicking the bell opens a dropdown with "PC Optimum token expired — Re-paste the token in Settings" → clicking the link navigates to `/settings`, focuses the token field for that account, and shows an inline error above the field ("Last sync failed: token expired. Paste a new token to resume.") → on paste + save, the account flips back to `active` and the next ticker tick succeeds.

The ticker **does not retry** while the account is in `auth_expired`. This is important: hammering a dead token would burn the user's IP and get the account flagged. The user must explicitly re-paste to resume.

The notification is per-user. If Vlad is logged in, he sees it; if his partner Marie is logged in, she sees her own copy of the same notification (one per user; the create query is per-user-id). Both can independently re-paste and the first one wins; the second paste overwrites the token.

### Flow 4 — Sync error (non-auth)

> PC Optimum's API is down for maintenance. The ticker gets a 503.

The flow is: ticker gets 503 → manager records `lastSyncError` on the account but leaves `status = active` → creates a `sync_error` notification → next ticker tick (15 min later) retries → if it succeeds, the notification is not auto-cleared (the user might miss it; they can dismiss from the bell) → if it fails 3 times in a row, escalate: change notification to "PC Optimum is unreachable — last 3 syncs failed" and badge the bell.

The 3-strikes escalation is a follow-up; the MVP just creates one notification per error and leaves the user to investigate.

### Flow 5 — Photo upload, possible duplicate

> Vlad photographs a No Frills receipt he already has in the app from a PC Optimum sync.

The flow is: upload → parse → API call to `/api/receipts/upload` returns `{proposal, potentialDuplicates: [<existing receipt summary>]}` → the proposal review page shows a **yellow banner** above the items: "This looks like a duplicate" with a link to the existing receipt → Vlad clicks the link to confirm, or dismisses the warning and continues editing → on approve, the new photo receipt is created anyway (we don't auto-suppress; that's a merge feature, not a duplicate-detection feature).

Why not auto-merge: auto-merging is irreversible and risky. The duplicate banner is information; the user decides. This matches how Splitwise and Expensify handle the same situation.

Why not block the approve: legitimate reasons exist to have two records (e.g. the user wants to track the photo for visual reference, plus the structured API data). The banner is advisory.

### Flow 6 — Manual sync, partial success

> Vlad clicks "Sync now" on a connection that has 12 new events, 2 of which return transient errors.

The flow is: handler returns 200 with `{created: 10, skipped: 0, updated: 0, newItemsAdded: 14, otherError: "2 events failed: <details>"}` → the Settings page shows the result inline as a collapsible panel under the account → the 2 failed events are re-tried on the next ticker tick (they have no per-event retry state in MVP; the ticker just re-fetches the whole list, which means the failed events get a second chance).

## Non-goals (deferred)

- **OAuth flow for PC Optimum** — the documented endpoint is the public web bundle's IDCS, not a public API. Reverse-engineering the redirect flow is out of scope. (Future ticket if the user wants a one-click connect.)
- **Token refresh** — the user re-pastes when it expires. (Future ticket: IDCS refresh token.)
- **Per-user connections** — only one PC Optimum account per User today. Two is allowed (the schema supports it) but the UI assumes one.
- **Receipt merge UI** — auto-merge, manual merge, "this is the same as X" workflows. The duplicate banner (Flow 5) is the seed for this; the actual merge is a separate ticket.
- **Push notifications / email digests** — in-app bell only.
- **Connection health dashboard** — no "last 30 days of syncs" chart. Just per-account `lastSyncedAt` and a "Sync now" button.

## Tier breakdown

### Tier 1 — Settings page (Task 11)

- New route `/settings`, gated by auth (the auth guard already covers this).
- New API methods in `client/main.ts`: `sourcesApi.list / connect / disconnect / sync`.
- Empty state: centered icon + "No loyalty accounts yet" + "Connect a loyalty account" button.
- Connected state: list of accounts, each a card with: source icon, display name, status badge (active / auth_expired / paused), last sync time, last sync error if any, "Sync now" button (disabled during cooldown), "Disconnect" link.
- "Connect a loyalty account" opens the **paste-token modal** (the 4-step flow in Story 1).
- Sync result panel below the card on demand (collapsible).

### Tier 2 — Notification bell (Task 11b)

- Bell icon in the existing sidebar/nav, between the nav items and the user avatar.
- Polls `GET /api/notifications?includeRead=false` every 30s while the page is open. The client uses `visibilitychange` to pause polling when the tab is hidden.
- Red badge with the unread count (max "9+").
- Dropdown shows: title, message, relative time ("2 minutes ago"), and a "Mark all read" footer.
- Each notification is a link (navigates to `link` on click and marks as read via POST `/api/notifications/{id}/read`).
- Empty dropdown state: "No new notifications."

### Tier 3 — Receipts list + detail badges (Task 12)

- List view: small badge next to the merchant name on every row. Mobile: badge stays inline; doesn't break the table layout (the existing `.responsive-table` rules from 2026-07-09 cover this).
- Detail view: badge in the header, clickable if `externalId` is set (links to `https://www.pcoptimum.ca/` for now; future tickets can deep-link to the specific transaction).

### Tier 4 — Duplicate banner (Task 13b)

- Proposal review page: yellow banner above the items list when the upload response has `potentialDuplicates`.
- Banner lists each potential duplicate as a link to the receipt detail page.
- "Dismiss" button (just for visual clarity; the banner re-appears if the user navigates away and back, because the API call returns the duplicates again on the next load).

## State machines

### LoyaltyAccount.status

```
        ┌──────┐  token paste success
        │      │ ◄────────────────────────┐
        ▼      │                          │
   ┌─────────┐ │  401 from source         │
   │ active  │ ├──────────────┐           │
   └────┬────┘ │              ▼           │
        │      │       ┌─────────────┐    │
   user │      │       │ auth_expired│    │
   pauses     │       └──────┬──────┘    │
        ▼      ▲              │ user     │
   ┌─────────┐ │              │ re-pastes│
   │ paused  │ │              └──────────┘
   └────┬────┘ │
        │ user│
        │ un- │
        │pause│
        └─────┘
```

`paused` is a user choice (they don't want background sync). `auth_expired` is set by the system; the user can only escape it by re-pasting. The status is exposed in the API and rendered in the Settings card.

### Notification lifecycle

```
  created (ReadAt = 0)
       │
       │ user clicks link or POSTs /read
       ▼
  read (ReadAt = <timestamp>)
       │
       │ not auto-deleted; stays in store
       │ "Mark all read" only affects unread
```

We don't auto-delete read notifications. The bell badge counts unread only. Future ticket: prune read notifications older than 30 days.

## Real-world app comparisons

| Behavior | Plaid | Mint | Splitwise | Grocer (this spec) |
|---|---|---|---|---|
| Connect flow | OAuth-style modal stack, "securely" framed | Username + password (deprecated) | Camera-first; no accounts | Paste-token modal with explicit steps |
| Failure communication | Per-institution error in the dropdown | Email if it persists for days | Toast + retry button | In-app bell, persists until read |
| Background sync | Continuous, transparent | Hourly | Per-photo | 15min ticker, skippable per-account |
| Duplicate detection | N/A (banks don't dup) | Flags "possible duplicate transfer" | "You may have already added this" banner | Yellow banner on proposal review |
| Token recovery | Session re-auth | Re-link | N/A | Re-paste, manual |

## What this spec is NOT

This is not a design document for "what should the app look like in two years." It's the minimum set of UI affordances needed to make Tasks 9, 10b, 11, 11b, 12, 13b in the plan feel finished to a real user. The follow-up tickets (merge, OAuth, encrypted tokens, OCR UPC extraction) each carry their own design pass.

The non-trivial design decision in this spec is **the paste-token modal**. Anything that takes user credentials — even a temporary bearer token — deserves a flow that:
- is short (4 steps, not 7),
- shows progress,
- explains *why* we need the credential (the modal text, not a help doc),
- surfaces test feedback inline (Test connection button),
- has a clear success and failure path,
- can be cancelled at any step without leaving partial state.

That's the bar. Anything less is a flow that users will quietly stop using.
