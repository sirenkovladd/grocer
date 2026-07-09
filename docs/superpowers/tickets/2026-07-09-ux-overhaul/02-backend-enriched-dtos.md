# Ticket 02 — Backend: Add enriched receipt DTOs

**Type:** Backend
**Files:** `internal/api/types.go` (new) or extend an existing types file
**Depends on:** —
**Blocks:** Ticket 03

## Goal

Define the JSON response shapes for the new enriched receipt endpoints. The DTOs are pure data — no handlers here.

## Context

The existing `domain.Receipt` struct (`internal/domain/types.go`) has only IDs. The new enriched endpoints need to embed names directly so the client doesn't have to do N+1 lookups.

Two endpoints will use these DTOs (implemented in ticket 03):
- `GET /api/receipts/enriched` — list view, summary enrichment
- `GET /api/receipts/{id}/enriched` — detail view, full enrichment

## Proposed DTOs

```go
// EnrichedReceiptSummary — list view, lightweight
type EnrichedReceiptSummary struct {
    ReceiptID     uint64    `json:"receiptId,string"`
    MerchantID    uint64    `json:"merchantId,string"`
    MerchantName  string    `json:"merchantName"`
    OwnerID       uint64    `json:"ownerId,string"`
    OwnerName     string    `json:"ownerName"`
    Date          int64     `json:"date"`
    ItemCount     int       `json:"itemCount"`
    TotalCents    int64     `json:"totalCents"`
    PhotoURL      string    `json:"photoUrl,omitempty"`
}

// EnrichedReceiptItem — item with name + category name embedded
type EnrichedReceiptItem struct {
    ItemID         uint64  `json:"itemId,string"`
    Name           string  `json:"name"`
    CategoryID     uint64  `json:"categoryId,string"`
    CategoryName   string  `json:"categoryName"`
    Quantity       float64 `json:"quantity"`
    UnitPriceCents int64   `json:"unitPriceCents"`
    TotalPriceCents int64  `json:"totalPriceCents"`
}

// EnrichedReceipt — full detail
type EnrichedReceipt struct {
    ReceiptID     uint64               `json:"receiptId,string"`
    MerchantID    uint64               `json:"merchantId,string"`
    MerchantName  string               `json:"merchantName"`
    OwnerID       uint64               `json:"ownerId,string"`
    OwnerName     string               `json:"ownerName"`
    Date          int64                `json:"date"`
    PhotoURL      string               `json:"photoUrl,omitempty"`
    Items         []EnrichedReceiptItem `json:"items"`
    TotalCents    int64                `json:"totalCents"`
}
```

## Open questions (brainstorm in fresh session)

- **`TotalPriceCents` on items:** The existing `domain.ReceiptItem` has only `UnitPriceCents` and `Quantity`. The frontend needs the line total (= quantity × unit). Should we compute it server-side (cleaner, single source of truth) or client-side (matches existing `domain.ReceiptItem` shape)? **Recommendation: compute server-side.** Frontend already does it on the proposal page; doing it here keeps the formula consistent.
- **Photo URL:** Include on the list view? It's a small string and useful for showing a tiny placeholder icon. **Recommendation: include it, but the frontend won't show a thumbnail yet (per plan risk #8).**
- **Empty category name:** If a receipt's items reference a deleted category, what should `categoryName` be? Empty string is brittle. **Recommendation: `"Unknown category"` fallback at the server.**
- **Empty merchant / owner name:** Same question. Same answer — fallback string.
- **Date format:** Leave as Unix int64 (compact, matches existing API). Client formats.
- **Where to put the file:** There's no `internal/api/types.go` today. Create a new file `internal/api/types.go`. Alternative: put DTOs in the same file as the handler (`receipts.go`) — less file proliferation but mixes types with handlers. **Recommendation: new file.**
- **Naming:** `Enriched` prefix or `ReceiptWithNames` / `ReceiptDetail`? **Recommendation: `Enriched*` prefix to match the endpoint name.**

## Acceptance criteria

- [ ] DTO structs are defined with appropriate JSON tags.
- [ ] DTOs compile (`go build ./...`).
- [ ] DTOs are documented (Godoc comments on each struct).
- [ ] Decision notes added to the "Decisions log" below.

## Verification commands

```bash
go build ./...
```

## Decisions log

_(Append decisions made during implementation. Format: `- YYYY-MM-DD: <decision> — <reason>`)_
