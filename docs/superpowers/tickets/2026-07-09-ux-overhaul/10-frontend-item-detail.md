# Ticket 10 — Frontend: Item detail page improvements

**Type:** Frontend (page)
**Files:** `client/pages/item-detail.ts`
**Depends on:** Tickets 04, 05
**Blocks:** —

## Goal

Add category name, breadcrumb, better money formatting, and a richer stats layout.

## Current state

`client/pages/item-detail.ts` (full file, ~140 lines):
- Fetches item + price history
- Shows name, `normalized`, `aliases`
- Stats: latest, average, min, max, trend
- Chart and purchase history table
- "Back" button (works but no breadcrumb)

## What the new page should do

### Layout
- Breadcrumb: `Items › {item name}`
- Header: item name + back button
- Category badge (new — currently no category shown despite the data being there)
- Stats grid (existing — keep, but use `formatMoney` for currency, `formatDate` for the purchase history dates)
- Chart (existing — keep)
- Purchase history table (existing — keep, add `data-label` for mobile responsive)

### Data
- Fetch `/api/items/{id}` (existing)
- Fetch `/api/analysis/items/{id}` (existing — returns price history)
- Fetch `/api/categories` (existing) — small additional fetch to resolve category name

## Implementation sketch

```ts
import van from "vanjs-core"
import { api, navigate } from "../main"
import { formatMoney, formatDate, indexBy } from "../utils"
import { Chart, registerables } from "chart.js"

Chart.register(...registerables)

// existing Item, PricePoint interfaces...

const ItemDetailPage = () => {
  const item = van.state<Item | null>(null)
  const history = van.state<PricePoint[]>([])
  const categories = van.state<Record<string, Category>>({})
  const loading = van.state(true)

  const load = async () => {
    const id = window.location.hash.split("/").pop()
    if (!id) return
    loading.val = true
    try {
      const [itemData, historyData, cats] = await Promise.all([
        api.get(`/items/${id}`),
        api.get(`/analysis/items/${id}`),
        api.get("/categories"),
      ])
      item.val = itemData
      history.val = historyData || []
      categories.val = indexBy(cats || [], (c: Category) => c.categoryId)
    } catch (err) {
      console.error("Failed to load item:", err)
    } finally {
      loading.val = false
    }
  }

  load()

  // ... chart init unchanged ...

  return div({ class: "item-detail-page" },
    () => loading.val
      ? div({ class: "loading" }, "Loading...")
      : !item.val
        ? div("Item not found")
        : div(
            // Breadcrumb
            div({ class: "breadcrumb" },
              a({ href: "#/items", onclick: (e: Event) => { e.preventDefault(); navigate("/items") } }, "Items"),
              span({ class: "separator" }, "›"),
              span({ class: "current" }, item.val.name),
            ),
            // Header
            div({ class: "page-header" },
              h1(item.val.name),
              button({ onclick: () => navigate("/items") }, "Back"),
            ),
            // Info card — now includes category
            div({ class: "item-info card" },
              p({ class: "muted" }, `Normalized: ${item.val.normalized}`),
              p(item.val.aliases.length > 0
                ? `Aliases: ${item.val.aliases.join(", ")}`
                : span({ class: "muted" }, "No aliases")),
              // NEW: category badge
              p({},
                span({ class: "muted" }, "Category: "),
                span({ class: "category-badge" },
                  categories.val[item.val.categoryId]?.name || "Uncategorized",
                ),
              ),
            ),
            // Stats — use formatMoney
            () => {
              const stats = getPriceStats()
              return stats ? div({ class: "price-stats" },
                div({ class: "stat-card card" },
                  p({ class: "stat-label" }, "Latest"),
                  p({ class: "stat-value money" }, formatMoney(stats.latest * 100)),
                ),
                // ... same for avg, min, max
              ) : ""
            },
            // Chart
            div({ class: "chart-container card" },
              h2("Price History"),
              canvas({ id: "price-chart" }),
            ),
            // Purchase history table — add responsive-table class + data-labels
            history.val.length > 0
              ? div({ class: "purchase-history card" },
                  h2("Purchase History"),
                  div({ class: "items-table-wrapper" },
                    table({ class: "responsive-table" },
                      tr(th("Date"), th("Price")),
                      ...history.val.map(h =>
                        tr(
                          td({ "data-label": "Date" }, formatDate(new Date(h.date).getTime() / 1000)),
                          td({ "data-label": "Price", class: "money" }, `$${h.price.toFixed(2)}`),
                        )
                      ),
                    ),
                  ),
                )
              : "",
          ),
  )
}
```

## Important: price data type mismatch

The `/api/analysis/items/{id}` endpoint returns `PricePoint` with `date: string` and `price: number` (in dollars, not cents). The existing code uses `$${h.price.toFixed(2)}` which assumes dollars. **The new `formatMoney` helper takes cents.** So either:
- (a) Update the analysis endpoint to return cents.
- (b) Multiply by 100 in the page before passing to `formatMoney`.
- (c) Add a `formatDollars` helper too.

**Recommend (b)** for this ticket — page-level conversion. Mention the inconsistency in Decisions log. Backend change is out of scope.

## Acceptance criteria

- [ ] Category name shown as a badge.
- [ ] Breadcrumb `Items › {name}` present and clickable.
- [ ] All money values use `formatMoney` (consistent format across pages).
- [ ] Purchase history dates use `formatDate`.
- [ ] Stats cards use money formatting.
- [ ] Table is mobile-responsive.
- [ ] `mise run build_client` passes.

## Open questions (brainstorm in fresh session)

- **Inline category editor:** The item page could let the user change the category directly. **Defer — out of scope for UX overhaul.**
- **Aliases editor:** Same — could add inline editing. **Defer.**
- **Date format from analysis endpoint:** The endpoint returns ISO string. Convert to unix seconds to use `formatDate`? Or pass to `Intl.DateTimeFormat` directly? **Recommend: convert to unix seconds** (consistent with the rest of the app).
- **Price history chart with zero data points:** Currently shows the chart canvas with no data. Add a friendly "No purchases yet" state? **Recommend yes** if easy.

## Verification commands

```bash
mise run build_client
```

Manual: visit `/items/{id}` for a few items, verify category name shows, breadcrumb works, money formatting consistent.

## Decisions log

_(Append decisions made during implementation. Format: `- YYYY-MM-DD: <decision> — <reason>`)_
