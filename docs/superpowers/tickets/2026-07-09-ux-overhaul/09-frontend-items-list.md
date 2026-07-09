# Ticket 09 — Frontend: Items list page overhaul

**Type:** Frontend (page)
**Files:** `client/pages/items.ts`
**Depends on:** Tickets 04, 05
**Blocks:** —

## Goal

Show item names (already shown — good), but replace the raw `categoryId` with the category name, show purchase count + total spent, add search and sort.

## Current state

`client/pages/items.ts` (full file, ~60 lines):
- Fetches `/api/items`
- Table: name, `categoryId.toString()`, aliases joined, "View" button
- No search, no sort, no purchase stats

## What the new page should do

### Layout
- Page header: `h1("Items")` + search input
- Skeleton rows while loading
- Empty state: "No items yet — upload a receipt to get started"
- Table columns:
  - Name (clickable → `/items/{id}`)
  - Category (name, not ID)
  - Aliases (truncated with `title` attribute for full list)
  - Purchases (count of times this item appeared in receipts)
  - Total spent (sum of `totalPriceCents` across all receipts)
  - Actions (View button)

### Data
- Fetch `/api/items` (existing) — items already have `name`
- Fetch `/api/categories` (existing) — build `id → name` map for the category column
- For purchase counts and totals: server has `/api/analysis/items/{id}` (existing). Either:
  - (a) Fetch for all items (N+1) — bad.
  - (b) Add a new endpoint `/api/items/with-stats` (backend work — out of scope here, can add as a follow-up).
  - (c) Skip the columns for now and add a future ticket.

**Recommend (c) for this ticket** — focus on the readability fix (category names, search, empty state) and add stats as a follow-up. Document in the Decisions log.

## Implementation sketch

```ts
import van from "vanjs-core"
import { api, navigate } from "../main"
import { indexBy } from "../utils"

interface Item {
  itemId: number
  name: string
  categoryId: number
  merchantId: number
  normalized: string
  aliases: string[]
}

interface Category {
  categoryId: number
  name: string
}

const ItemsPage = () => {
  const items = van.state<Item[]>([])
  const categories = van.state<Record<string, Category>>({})
  const loading = van.state(true)
  const search = van.state("")

  const load = async () => {
    loading.val = true
    try {
      const [i, c] = await Promise.all([api.get("/items"), api.get("/categories")])
      items.val = i || []
      categories.val = indexBy(c || [], (x: Category) => x.categoryId)
    } catch (err) {
      console.error("Failed to load items:", err)
    } finally {
      loading.val = false
    }
  }

  load()

  const filtered = () => {
    const s = search.val.toLowerCase()
    if (!s) return items.val
    return items.val.filter(i =>
      i.name.toLowerCase().includes(s) ||
      i.aliases.some(a => a.toLowerCase().includes(s))
    )
  }

  return div({ class: "items-page" },
    div({ class: "page-header" },
      h1("Items"),
    ),
    div({ class: "filter-bar" },
      input({
        type: "search",
        class: "search-input",
        placeholder: "Search items or aliases…",
        value: search,
        oninput: (e: Event) => search.val = (e.target as HTMLInputElement).value,
      }),
    ),
    () => loading.val
      ? /* skeleton rows */
      : items.val.length === 0
        ? /* empty state */
        : filtered().length === 0
          ? /* "no matches" state */
          : div({ class: "items-table-wrapper" },
              table({ class: "responsive-table" },
                tr(th("Name"), th("Category"), th("Aliases"), th("Actions")),
                ...filtered().map(item =>
                  tr(
                    td({ "data-label": "Name" },
                      a({
                        href: `#/items/${item.itemId}`,
                        class: "item-name-link",
                        onclick: (e: Event) => { e.preventDefault(); navigate(`/items/${item.itemId}`) },
                      }, item.name),
                    ),
                    td({ "data-label": "Category" },
                      span({ class: "category-badge" }, categories.val[item.categoryId]?.name || "Uncategorized"),
                    ),
                    td({ "data-label": "Aliases" },
                      item.aliases.length > 0
                        ? span({ title: item.aliases.join(", ") },
                            item.aliases.length > 3
                              ? `${item.aliases.slice(0, 3).join(", ")} +${item.aliases.length - 3} more`
                              : item.aliases.join(", "))
                        : span({ class: "muted" }, "—"),
                    ),
                    td({ "data-label": "Actions" },
                      button({ onclick: () => navigate(`/items/${item.itemId}`) }, "View"),
                    ),
                  )
                ),
              ),
            ),
  )
}

export default ItemsPage
```

## Acceptance criteria

- [ ] Category column shows category name (e.g. "Produce"), not the numeric ID.
- [ ] Item name is a clickable link to `/items/{id}`.
- [ ] Aliases are truncated with a tooltip showing the full list.
- [ ] Search box filters by item name OR alias (case-insensitive).
- [ ] Empty state with a clear message when there are no items.
- [ ] "No matches" state when search returns nothing.
- [ ] Skeleton rows on initial load.
- [ ] Mobile responsive table (`.responsive-table`).
- [ ] `mise run build_client` passes.

## Open questions (brainstorm in fresh session)

- **Sort order:** Alphabetical? By purchase count? By date added? **Recommend: alphabetical by name** (predictable, easy to scan).
- **Purchase stats columns:** Add now or defer? See context — **defer for now**. Add follow-up ticket.
- **Category filter:** Add a category dropdown? **Defer.**
- **Pagination:** At what count? **Defer until >200 items.**
- **Items with no category:** Items in the system might reference a deleted category. Show "Uncategorized" badge.
- **Click on row vs click on name:** Should the whole row be clickable? Or just the name? **Recommend: just the name** (less surprising).

## Verification commands

```bash
mise run build_client
```

Manual: visit `/items`, check that categories show names, search works.

## Decisions log

_(Append decisions made during implementation. Format: `- YYYY-MM-DD: <decision> — <reason>`)_
