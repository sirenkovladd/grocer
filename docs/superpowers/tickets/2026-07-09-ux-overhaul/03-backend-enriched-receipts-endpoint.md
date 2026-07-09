# Ticket 03 — Backend: Add enriched receipt endpoints

**Type:** Backend
**Files:** `internal/api/receipts.go`, `internal/api/router.go`
**Depends on:** Ticket 02 (DTOs)
**Blocks:** Tickets 07, 08 (frontend pages)

## Goal

Add two new endpoints that return receipts with embedded names (merchant, owner, item names, category names) so the frontend can render human-readable data without N+1 lookups.

## Endpoints

- `GET /api/receipts/enriched` — list view, supports existing query params (`from`, `to`, `owner`, `category`), returns array of `EnrichedReceiptSummary`.
- `GET /api/receipts/{id}/enriched` — detail view, returns one `EnrichedReceipt` with full per-item enrichment.

The existing `/api/receipts` and `/api/receipts/{id}` are **not changed** (bots and other clients depend on the existing shape).

## Context

The existing handlers in `internal/api/receipts.go` to study:
- `handleListReceipts` (line 12) — applies filters, returns `[]*domain.Receipt`.
- `handleGetReceipt` (line ~100) — single fetch.
- `loadItemMap` (line ~95) — already batches item lookups, useful pattern.

Existing store methods:
- `r.store.ListReceipts()` — all receipts
- `r.store.GetReceipt(id)` — single
- `r.store.GetMerchant(id)`, `r.store.GetItem(id)`, `r.store.GetCategory(id)`, `r.store.GetUserByUserID(id)` — all available
- `r.store.ListMerchants()`, `r.store.ListItems()`, `r.store.ListCategories()`, `r.store.ListUsers()` — for batch loading

## Implementation sketch

```go
func (r *Router) handleListEnrichedReceipts(w http.ResponseWriter, req *http.Request) {
    // Reuse the filter logic from handleListReceipts.
    // After filtering, batch-load:
    //   - merchants: r.store.ListMerchants() -> map[id]*domain.Merchant
    //   - users:     r.store.ListUsers()     -> map[id]*domain.User
    // For each receipt, build EnrichedReceiptSummary.
}

func (r *Router) handleGetEnrichedReceipt(w http.ResponseWriter, req *http.Request) {
    // Fetch receipt via r.store.GetReceipt(id)
    // Batch-load merchant, owner, all items + their categories.
    // Build EnrichedReceipt.
}
```

**Helper:** `enrichReceipts(receipts []*domain.Receipt, summaries bool) []EnrichedReceiptSummary | []EnrichedReceipt` — single helper, returns either shape based on a flag, or two separate helpers. **Recommend two separate helpers** for clarity.

## Performance

- One list query, one merchant list, one user list per list request. Constant round trips.
- One receipt fetch, one merchant fetch, one owner fetch, one item list, one category list per detail request. Constant.
- No N+1. No caching needed (memdb reads are O(1)).

## Acceptance criteria

- [ ] `GET /api/receipts/enriched` returns 200 with array of `EnrichedReceiptSummary`.
- [ ] `GET /api/receipts/{id}/enriched` returns 200 with `EnrichedReceipt`.
- [ ] Both endpoints require auth (`r.withAuth`).
- [ ] Both endpoints wrapped in `r.withCORS` and `r.withAuditLogging` (matching existing pattern).
- [ ] Filter query params (`from`, `to`, `owner`, `category`) work on `/api/receipts/enriched` identically to `/api/receipts`.
- [ ] `EnrichedReceiptSummary.merchantName` falls back to `"Unknown merchant"` if the merchant was deleted.
- [ ] `EnrichedReceiptSummary.ownerName` falls back to `"Unknown"` if the owner was deleted.
- [ ] `EnrichedReceiptItem.categoryName` falls back to `"Uncategorized"` if the category was deleted.
- [ ] `EnrichedReceiptItem.totalPriceCents` = `Quantity * UnitPriceCents` (rounded to int64).
- [ ] `go build ./...` passes.
- [ ] `go test ./internal/api/...` passes; add a test for the enrichment helper if it has logic worth testing (missing merchant fallback, etc.).
- [ ] No changes to existing `/api/receipts` or `/api/receipts/{id}` responses.

## Open questions (brainstorm in fresh session)

- **Refactor opportunity:** `handleListReceipts` and `handleListEnrichedReceipts` will share filter logic. Extract a `filterReceipts(receipts, from, to, owner, category)` helper? Or duplicate? **Recommend extract.**
- **Owner filter:** The existing list endpoint supports `?owner=ID`. Should the enriched one too? **Yes** — keep parity.
- **Sort order:** Stable sort by `date` descending (newest first)? Or insertion order? **Recommend newest first** (matches what the home page probably wants).
- **Empty list:** `200 OK []` not `null`. Make sure the helper returns `[]T{}` for empty.
- **404 vs 200 with empty body:** For `GET /api/receipts/{id}/enriched` with a missing ID, return `404` matching the existing `handleGetReceipt`.
- **Date filter input validation:** Existing code uses `time.Parse("2006-01-02", from)` and silently ignores parse errors. Mirror that behavior for consistency, or be strict? **Recommend mirror for now** to avoid breaking the existing contract.

## Verification commands

```bash
go build ./...
go test ./internal/api/...

# Manual checks (with server running)
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/receipts/enriched | jq
# Expected: array with merchantName, ownerName fields embedded

curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/receipts/123/enriched | jq
# Expected: full detail with items[].name and items[].categoryName

curl -s -H "Authorization: Bearer $TOKEN" "http://localhost:8080/api/receipts/enriched?from=2026-01-01&to=2026-12-31" | jq
# Expected: filtered list
```

## Decisions log

_(Append decisions made during implementation. Format: `- YYYY-MM-DD: <decision> — <reason>`)_
