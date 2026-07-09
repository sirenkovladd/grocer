# Ticket 07 — Frontend: Receipts list page overhaul

**Type:** Frontend (page)
**Files:** `client/pages/receipts.ts`
**Depends on:** Tickets 01, 02, 03, 04, 05
**Blocks:** —

## Goal

Replace the current raw-ID list with a human-readable list showing merchant, date, item count, and total. Add date range, owner, merchant filters and a search box.

## Current state

`client/pages/receipts.ts` (full file, ~70 lines):
- Fetches `/api/receipts`
- Renders cards showing `Receipt #${id}` + date + item count
- No filters, no search

## What the new page should do

### Layout
- Page header: `h1("Receipts")` + `button("Upload Receipt")` (existing)
- Filter bar: search input (filters by merchant name or item name), date-from input, date-to input, owner dropdown
- Skeleton rows while loading
- Empty state with "Upload your first receipt" CTA
- Card grid of receipts

### Card content
- Merchant name (large, bold)
- Date (right side, muted)
- Item count + total (bottom row, money class)
- Click → `/receipts/{id}` (existing behavior)

### Data
- Fetch `/api/receipts/enriched` (from ticket 03)
- Fetch `/api/users` once on mount → build a map for the owner dropdown filter (data not displayed per plan)
- Fetch `/api/merchants` once on mount → build a map for the merchant dropdown filter
- Filters applied **client-side** (since data is already loaded). Server-side filter via query params exists but client-side is simpler for the current data volume. Mention as a trade-off in the Decisions log.

## Implementation sketch

```ts
import { api, navigate } from "../main"
import { formatDate, formatMoney, indexBy } from "../utils"

interface EnrichedReceiptSummary {
  receiptId: number
  merchantId: number
  merchantName: string
  ownerId: number
  ownerName: string       // not displayed yet per plan
  date: number
  itemCount: number
  totalCents: number
  photoUrl?: string
}

const ReceiptCard = (r: EnrichedReceiptSummary, onClick: () => void) =>
  div({ class: "receipt-card card", onclick: onClick },
    div({ class: "receipt-header" },
      div({ class: "receipt-merchant" }, r.merchantName),
      div({ class: "receipt-date" }, formatDate(r.date)),
    ),
    div({ class: "receipt-meta" },
      span(`${r.itemCount} items`),
      span({ class: "money" }, formatMoney(r.totalCents)),
    ),
  )

const ReceiptsPage = () => {
  const receipts = van.state<EnrichedReceiptSummary[]>([])
  const merchants = van.state<Record<string, string>>({})  // id → name
  const loading = van.state(true)

  const search = van.state("")
  const from = van.state("")
  const to = van.state("")
  const ownerFilter = van.state("")

  const loadData = async () => {
    loading.val = true
    try {
      const [r, m] = await Promise.all([
        api.get("/receipts/enriched"),
        api.get("/merchants"),
      ])
      receipts.val = Array.isArray(r) ? r : []
      merchants.val = indexBy(m || [], (x: any) => x.merchantId)
    } catch (err) {
      console.error("Failed to load receipts:", err)
    } finally {
      loading.val = false
    }
  }

  loadData()

  // Filter logic
  const filtered = () => {
    return receipts.val.filter(r => {
      if (from.val && r.date < new Date(from.val).getTime() / 1000) return false
      if (to.val && r.date > new Date(to.val).getTime() / 1000 + 86399) return false
      if (ownerFilter.val && r.ownerId !== parseInt(ownerFilter.val)) return false
      if (search.val) {
        const s = search.val.toLowerCase()
        if (!r.merchantName.toLowerCase().includes(s)) return false
      }
      return true
    })
  }

  return div({ class: "receipts-page" },
    div({ class: "page-header" },
      h1("Receipts"),
      button({ onclick: () => navigate("/receipts/upload") }, "Upload Receipt"),
    ),
    // Filter bar
    div({ class: "filter-bar" },
      input({
        type: "search",
        class: "search-input",
        placeholder: "Search merchant…",
        value: search,
        oninput: (e: Event) => search.val = (e.target as HTMLInputElement).value,
      }),
      input({ type: "date", value: from, oninput: ... }),
      input({ type: "date", value: to, oninput: ... }),
    ),
    // List
    () => loading.val
      ? /* skeleton rows */
      : filtered().length === 0
        ? /* empty state */
        : div({ class: "receipts-grid" }, ...filtered().map(r => ReceiptCard(r, ...))),
  )
}
```

## Acceptance criteria

- [ ] Visiting `/receipts` shows a list of cards with merchant names, dates, item counts, and totals.
- [ ] No raw `Receipt #...` text is shown.
- [ ] Loading state shows skeleton rows (not "Loading..." text).
- [ ] Empty state has a clear CTA ("Upload your first receipt" with button).
- [ ] Date range filter (from/to) works.
- [ ] Search box filters by merchant name (case-insensitive substring match).
- [ ] Each card click navigates to `/receipts/{id}`.
- [ ] Totals are right-aligned and monospaced.
- [ ] Dates use the new `formatDate` helper (e.g. "May 30, 2026").
- [ ] `mise run build_client` passes.

## Open questions (brainstorm in fresh session)

- **Sort order:** Server returns insertion order (or whatever the backend returns). Sort client-side by date desc? **Recommend: newest first.**
- **Owner filter:** Per plan, owner names aren't shown but the data is loaded. Should the owner dropdown show names or just "(All)" / owner IDs? **Recommend: dropdown with names** (it works without "displaying" owner in the card).
- **Search scope:** Just merchant name, or also total amount? Item names? **Recommend: merchant name only for now.** Future: server-side search.
- **Date filter:** `<input type="date">` is good. What if `from > to`? Show a warning? Or just return empty? **Recommend: empty result, no warning** (less code).
- **Filter persistence:** Should filters persist across navigation? URL params? **Recommend: not for now.** Easy to add later.
- **Total of filtered results:** Show a sum at the bottom? Nice-to-have. **Defer.**
- **Card layout breakpoint:** Currently a single-column grid. Switch to 2-col on wide screens? **Defer.**

## Verification commands

```bash
mise run build_client
```

Manual: load the page, verify cards render correctly, try filters.

## Decisions log

_(Append decisions made during implementation. Format: `- YYYY-MM-DD: <decision> — <reason>`)_
