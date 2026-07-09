# Ticket 08 — Frontend: Receipt detail page overhaul

**Type:** Frontend (page)
**Files:** `client/pages/receipt.ts`
**Depends on:** Tickets 01, 02, 03, 04, 05
**Blocks:** —

## Goal

Replace the raw-ID table with a human-readable detail view: merchant name, owner name (loaded but not displayed per plan), date, photo, items with names + categories + weighted quantity handling.

## Current state

`client/pages/receipt.ts` (full file, ~70 lines):
- Fetches `/api/receipts/{id}`
- Shows: `Receipt #${id}` header, date, total
- Items table: `Item #${itemId}` (×N), quantity, unit price, total

## What the new page should do

### Layout
- Breadcrumb: `Receipts › {merchant name}` (or `Receipts › Receipt #935…` if no merchant)
- Page header: merchant name (large) + date + total + owner (loaded but not shown per plan; consider showing only in tooltip on hover, or skip entirely)
- Photo: load via existing `fetchPhotoUrl` helper (already in `proposal.ts` — can copy/import)
- Items table: name (clickable → `/items/{id}`), qty (with weighted subtitle), unit price, total, category badge
- Items table should be `.responsive-table` (mobile-friendly stacked layout from ticket 05 CSS)

### Data
- Fetch `/api/receipts/{id}/enriched` (from ticket 03)
- Fetch `/api/photos/{id}` (existing endpoint, returns the photo blob)

### Weighted quantity
The proposal page already has the logic for showing `@ $1.96/kg` under a weighted item. **Copy that logic.** The condition: `item.quantity !== 1 && item.unitPriceCents !== item.totalPriceCents`.

Wait — the existing API returns `Quantity` and `UnitPriceCents` only. We need to compute `TotalPriceCents = Quantity * UnitPriceCents`. The enriched endpoint (ticket 03) returns this. Use it.

## Implementation sketch

```ts
import van from "vanjs-core"
import { api, navigate } from "../main"
import { formatDate, formatMoney, formatQuantity } from "../utils"

interface EnrichedReceiptItem {
  itemId: number
  name: string
  categoryId: number
  categoryName: string
  quantity: number
  unitPriceCents: number
  totalPriceCents: number
}

interface EnrichedReceipt {
  receiptId: number
  merchantId: number
  merchantName: string
  ownerId: number
  ownerName: string       // not displayed
  date: number
  photoUrl?: string
  items: EnrichedReceiptItem[]
  totalCents: number
}

// Photo helper — copy from proposal.ts or import if refactored
const fetchPhotoUrl = async (receiptId: number): Promise<string> => {
  const token = localStorage.getItem("token")
  const response = await fetch(`/api/photos/${receiptId}`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!response.ok) throw new Error(`HTTP ${response.status}`)
  const blob = await response.blob()
  return URL.createObjectURL(blob)
}

const ReceiptDetailPage = () => {
  const receipt = van.state<EnrichedReceipt | null>(null)
  const photoSrc = van.state("")
  const loading = van.state(true)

  const id = window.location.hash.split("/").pop()

  const load = async () => {
    if (!id) return
    loading.val = true
    try {
      const data = await api.get(`/receipts/${id}/enriched`)
      receipt.val = data
      if (data?.photoUrl) {
        photoSrc.val = await fetchPhotoUrl(data.receiptId)
      }
    } catch (err) {
      console.error("Failed to load receipt:", err)
    } finally {
      loading.val = false
    }
  }

  load()

  return div({ class: "receipt-detail-page" },
    () => loading.val
      ? div({ class: "loading" }, "Loading...")
      : !receipt.val
        ? div("Receipt not found")
        : div(
            // Breadcrumb
            div({ class: "breadcrumb" },
              a({ href: "#/receipts", onclick: (e: Event) => { e.preventDefault(); navigate("/receipts") } }, "Receipts"),
              span({ class: "separator" }, "›"),
              span({ class: "current" }, receipt.val.merchantName || `Receipt #${receipt.val.receiptId}`),
            ),
            // Header
            div({ class: "page-header" },
              h1(receipt.val.merchantName || `Receipt #${receipt.val.receiptId}`),
              div({ class: "page-header-meta" },
                span({ class: "muted" }, formatDate(receipt.val.date)),
                span({ class: "money" }, formatMoney(receipt.val.totalCents)),
              ),
            ),
            // Photo
            () => photoSrc.val
              ? div({ class: "receipt-photo" }, img({ src: photoSrc.val, alt: "Receipt" }))
              : "",
            // Items table
            h2("Items"),
            div({ class: "items-table-wrapper" },
              table({ class: "responsive-table" },
                tr(th("Item"), th("Category"), th("Qty"), th("Unit Price"), th("Total")),
                ...receipt.val.items.map(item => {
                  const isWeighted = item.quantity !== 1 && item.unitPriceCents !== item.totalPriceCents
                  return tr(
                    td({ "data-label": "Item" },
                      a({
                        href: `#/items/${item.itemId}`,
                        class: "item-name-link",
                        onclick: (e: Event) => { e.preventDefault(); navigate(`/items/${item.itemId}`) },
                      }, item.name),
                    ),
                    td({ "data-label": "Category" },
                      span({ class: "category-badge" }, item.categoryName || "Uncategorized"),
                    ),
                    td({ "data-label": "Qty" },
                      formatQuantity(item.quantity),
                      isWeighted
                        ? div({ class: "item-unit-price" }, `@ ${formatMoney(item.unitPriceCents)}/unit`)
                        : "",
                    ),
                    td({ "data-label": "Unit Price", class: "money muted" }, formatMoney(item.unitPriceCents)),
                    td({ "data-label": "Total", class: "money" }, formatMoney(item.totalPriceCents)),
                  )
                }),
              ),
            ),
          ),
  )
}

export default ReceiptDetailPage
```

## Acceptance criteria

- [ ] Items show their real names (e.g. "Bananas", "Whole Milk"), not `Item #935...`.
- [ ] Each item name is a clickable link to `/items/{id}`.
- [ ] Categories show as badges (e.g. "Produce", "Dairy").
- [ ] Weighted items show `@ $1.96/unit` subtitle under the quantity (when qty ≠ 1 and unit price ≠ total).
- [ ] Money columns are right-aligned and monospaced.
- [ ] Dates use `formatDate`.
- [ ] Photo of the receipt displays below the header (when available).
- [ ] Breadcrumb `Receipts › {merchant}` is present and clickable.
- [ ] On mobile (< 768px), table collapses to stacked card layout.
- [ ] No raw `Receipt #...` or `Item #...` text in the user-facing UI.
- [ ] Owner name is **not** displayed (per plan).
- [ ] `mise run build_client` passes.

## Open questions (brainstorm in fresh session)

- **Photo size:** Full-size from the API could be ~1MB. Should we constrain with CSS `max-height`? **Recommend: `max-height: 400px; object-fit: contain;`** in CSS. Add to ticket 05 if not already.
- **Photo on right or below?** Layout choice. Below the header is the simplest. **Stick with plan.**
- **Category link:** Should the category badge be a link to `/items?category={id}` or just static? **Recommend: static badge for now** (no filtering UI on /items yet).
- **Item link target:** Should `cmd+click` open in new tab? VanJS anchor handles that natively via `target="_blank"` — but that requires an actual `href`. The current pattern is `onclick: (e) => { e.preventDefault(); navigate(...) }` so cmd+click doesn't work. **Acceptable trade-off for SPA routing.** Future: use real hrefs and intercept clicks.
- **Photo memory leak:** `URL.createObjectURL` creates a blob URL. Should be revoked on unmount. The proposal page currently doesn't. **Out of scope** but worth a follow-up.
- **Empty items array:** Defensive UI for a receipt with 0 items. **Recommend: show "No items"** in the table body.
- **Total verification:** Show a "(matches total)" check or a discrepancy warning if `sum(item.totalPriceCents) !== receipt.totalCents`? **Defer.**

## Verification commands

```bash
mise run build_client
```

Manual: navigate to a receipt detail page, verify all the new elements render.

## Decisions log

_(Append decisions made during implementation. Format: `- YYYY-MM-DD: <decision> — <reason>`)_
