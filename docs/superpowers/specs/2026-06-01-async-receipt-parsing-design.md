# Async Receipt Parsing

## Problem

The current upload-and-parse flow blocks the user on the upload page for 3+ minutes while the LLM processes the receipt. The page appears empty during parsing. Users want to snap a receipt photo quickly and review the parsed results later.

## Goal

Decouple upload from parsing. Upload is instant. Parsing runs as a background job. Users watch live progress on the proposal detail page, or come back later when it's done.

## New Proposal Statuses

```
"parsing"  → background goroutine running, items streaming in
"pending"  → parsing complete, ready for user review
"approved" → user approved, receipt created
"failed"   → parsing errored, user can retry
```

## Flow

1. User selects photo on upload page, clicks "Upload"
2. `POST /api/receipts/upload` stores photo, creates proposal with `status = "parsing"`, spawns background goroutine, returns `{ id }` immediately
3. Client navigates to `/proposals/{id}`
4. Proposal page opens `EventSource` to `GET /api/proposals/{id}/stream`
5. SSE endpoint sends current proposal state as initial `snapshot` event, then streams live `item`, `progress`, `done`, `error` events
6. Background goroutine runs LLM streaming + item matching, writes items to store progressively, broadcasts events to connected clients via in-memory channel hub
7. On completion: status → `"pending"`, broadcast `done` event
8. On error: status → `"failed"`, broadcast `error` event
9. User reviews items and approves (existing flow)

## Backend Design

### Upload Endpoint Changes

`POST /api/receipts/upload` becomes lightweight:

- Read multipart photo (existing)
- Resize for LLM (existing)
- Store photo (existing)
- Create proposal with `status = "parsing"`, empty items
- Spawn `go parser.ParseReceiptAsync(ctx, proposal, userID)`
- Return `{ "id": proposalID }`

Remove all SSE logic from this endpoint. The old `POST /api/receipts/upload/stream` endpoint can be removed.

### Parse Goroutine

New method on `Parser`: `ParseReceiptAsync(ctx context.Context, proposal *domain.Proposal, userID uint64)`

Runs the existing `ParseReceiptStream` logic but:

1. Opens LLM streaming connection
2. As items are parsed, creates `ProposalItem` entries and writes them to the proposal in the store
3. After each item, broadcasts an `item` event via the channel hub
4. After full parse, runs item matching (fuzzy match + categorization)
5. Updates proposal with merchant, date, total, matched items
6. Sets `status = "pending"`, broadcasts `done` event
7. On any error, sets `status = "failed"`, broadcasts `error` event with message

### SSE Endpoint

New: `GET /api/proposals/{id}/stream`

1. Load proposal from store
2. If `status != "parsing"` → send single `snapshot` event with full proposal, close connection
3. If `status == "parsing"` → send `snapshot` event with current state, subscribe to channel hub, stream events until `done` or `error`

Event types:

| Event | Payload | When |
|---|---|---|
| `snapshot` | Full proposal JSON | On connect (always sent) |
| `progress` | `{ "message": "string" }` | Status updates from parser |
| `item` | `ProposalItem` JSON | Each new item parsed |
| `done` | Full proposal JSON | Parsing complete |
| `error` | `{ "message": "string" }` | Parsing failed |

### Channel Hub

In-memory pub/sub in `internal/store/` (or new `internal/events/` package).

```
type EventHub struct {
    mu          sync.RWMutex
    subscribers map[uint64][]chan ParseEvent  // proposalID → subscribers
}

func (h *EventHub) Subscribe(proposalID uint64) <-chan ParseEvent
func (h *EventHub) Unsubscribe(proposalID uint64, ch <-chan ParseEvent)
func (h *EventHub) Publish(proposalID uint64, event ParseEvent)
```

- Parse goroutine calls `Publish` after each event
- SSE handler calls `Subscribe` on connect, `Unsubscribe` on disconnect
- No persistence needed — events are ephemeral, proposal state is in the store

### Retry Endpoint

New: `POST /api/proposals/{id}/reparse`

1. Load proposal, verify `status == "failed"`
2. Reset status to `"parsing"`, clear items
3. Spawn new `ParseReceiptAsync` goroutine
4. Return `{ "id": proposalID }`

### Store Changes

- `UpdateProposalStatus(id, status)` — change proposal status
- `AppendProposalItem(id, item)` — add item to proposal during streaming
- `UpdateProposalParseResult(id, merchant, date, total, items)` — final update after matching

## Frontend Design

### Upload Page (`client/pages/upload.ts`)

Simplified dramatically:

- Dropzone + camera input (existing)
- On submit: POST photo to `/api/receipts/upload`, get `{ id }`, navigate to `/proposals/{id}`
- No SSE on this page
- Show brief "Uploading..." state while POST completes

### Proposal Detail Page (`client/pages/proposal.ts`)

This is where the main UX change lives. Page opens an `EventSource` and renders based on proposal state:

**State machine:**

```
[snapshot received, status=parsing] → show loader skeleton + stream items
[item event] → append item to list with fade-in
[done event, status=pending] → show full review form (existing approval UI)
[error event, status=failed] → show error message + retry button
[snapshot received, status=pending] → show full review form (came back later)
[snapshot received, status=failed] → show error + retry button
[snapshot received, status=approved] → show read-only receipt (redirect?)
```

**Parsing view:**

- Skeleton header (merchant, date, total placeholders with pulse animation)
- Items list with items appearing one-by-one as they arrive
- Each item shows: name, quantity, price, small spinner indicating "matching..."
- Progress text at bottom: "Parsing receipt... 5 items found"

**Failed view:**

- Error message
- "Retry" button → `POST /api/proposals/{id}/reparse`, then reconnect SSE

**Pending view:**

- Existing approval UI (unchanged)

### Proposals List Page (`client/pages/proposals.ts`)

- Show all proposals (not just pending)
- Parsing proposals show spinner badge + item count
- Failed proposals show error badge + "Retry" link
- Pending proposals show existing approve button
- Approved proposals show checkmark (or hide them — they're now receipts)

## Error Handling

| Scenario | Behavior |
|---|---|
| Parse goroutine errors | Set status="failed", broadcast error event, user can retry |
| Client disconnects from SSE | Goroutine keeps running, parse finishes server-side |
| Client reconnects to SSE | Gets snapshot with current state, then live events if still parsing |
| Retry on failed proposal | New goroutine, same photo, overwrites items |
| Browser refresh during parsing | Reconnects SSE, gets snapshot + remaining events |
| Server restart during parsing | Proposal stuck in "parsing" — add timeout/recovery (future improvement) |

## Concurrency

- One parse goroutine per proposal (enforced by status check — only "parsing" proposals have active goroutines)
- Multiple clients can watch the same proposal (fan-out via channel hub)
- No external queue needed — single-process goroutines are sufficient for family-scale usage
- Channel hub is ephemeral — if server restarts, proposals in "parsing" state need manual recovery (acceptable for v1)

## Files to Modify

| File | Change |
|---|---|
| `internal/domain/types.go` | Add "parsing" and "failed" to valid proposal statuses |
| `internal/receipt/parser.go` | Add `ParseReceiptAsync` method, refactor shared logic with `ParseReceiptStream` |
| `internal/store/proposals.go` | Add `UpdateProposalStatus`, `AppendProposalItem`, `UpdateProposalParseResult` |
| `internal/events/hub.go` | **New file** — in-memory event pub/sub |
| `internal/api/receipts.go` | Simplify upload handler, remove SSE logic |
| `internal/api/proposals.go` | Add SSE stream endpoint, add reparse endpoint |
| `internal/api/router.go` | Register new routes |
| `client/pages/upload.ts` | Simplify to upload + redirect |
| `client/pages/proposal.ts` | Add SSE consumer, parsing/pending/failed state rendering |
| `client/pages/proposals.ts` | Show all statuses with appropriate badges |

## Non-Goals (v1)

- Background parsing across server restarts (need persistent queue)
- Push notifications when parsing completes
- Parallel parsing of multiple receipts
- Webhook/callback on completion
