import van from "vanjs-core"
import { api, navigate } from "../main"
import { indexBy } from "../utils"

const { div, h1, h2, h3, p, input, table, tr, td, th, button, span, a } = van.tags

// ID fields are `string` (uint64 precision safety). See ticket 04
// decisions log. Migrating other pages that still use `number` is a
// follow-up.
interface Item {
  itemId: string
  name: string
  categoryId: string
  merchantId: string
  normalized: string
  aliases: string[]
}

interface Category {
  categoryId: string
  name: string
}

const SkeletonRow = () =>
  div({ class: "skeleton-row" },
    div({ class: "skeleton-cell skeleton-cell-lg" }),
    div({ class: "skeleton-cell skeleton-cell-md" }),
    div({ class: "skeleton-cell skeleton-cell-md" }),
  )

const formatAliases = (aliases: string[]) => {
  if (aliases.length === 0) {
    return span({ class: "muted" }, "—")
  }
  const MAX = 3
  const shown = aliases.slice(0, MAX).join(", ")
  const extra = aliases.length - MAX
  const text = extra > 0 ? `${shown} +${extra} more` : shown
  return span({ title: aliases.join(", ") }, text)
}

const ItemsPage = () => {
  const items = van.state<Item[]>([])
  const categories = van.state<Record<string, Category>>({})
  const loading = van.state(true)
  const error = van.state<string | null>(null)
  const search = van.state("")

  const load = async () => {
    loading.val = true
    error.val = null
    try {
      const [i, c] = await Promise.all([
        api.get("/items"),
        api.get("/categories"),
      ])
      items.val = Array.isArray(i) ? i : []
      categories.val = indexBy(Array.isArray(c) ? c : [], (x: Category) => x.categoryId)
    } catch (err) {
      console.error("Failed to load items:", err)
      error.val = (err as Error).message || "Failed to load items"
    } finally {
      loading.val = false
    }
  }

  load()

  // Sort alphabetically by name (case-insensitive). Predictable for
  // scanning; matches ticket 09 recommendation.
  const sorted = (): Item[] => {
    return [...items.val].sort((a, b) =>
      a.name.toLowerCase().localeCompare(b.name.toLowerCase()),
    )
  }

  const filtered = (): Item[] => {
    const s = search.val.trim().toLowerCase()
    if (!s) return sorted()
    return sorted().filter(i =>
      i.name.toLowerCase().includes(s) ||
      i.aliases.some(a => a.toLowerCase().includes(s)),
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
        oninput: (e: Event) => {
          search.val = (e.target as HTMLInputElement).value
        },
      }),
    ),

    () => {
      if (error.val) {
        return div({ class: "empty-state" },
          h3("Couldn't load items"),
          p(error.val),
          button({ onclick: load }, "Try Again"),
        )
      }
      if (loading.val) {
        return div({ class: "items-skeleton" },
          SkeletonRow(), SkeletonRow(), SkeletonRow(),
          SkeletonRow(), SkeletonRow(),
        )
      }
      if (items.val.length === 0) {
        return div({ class: "empty-state" },
          h3("No items yet"),
          p("Upload a receipt to get started."),
          button({ onclick: () => navigate("/receipts/upload") }, "Upload your first receipt"),
        )
      }
      const list = filtered()
      if (list.length === 0) {
        return div({ class: "empty-state" },
          h3("No items match your search"),
          p("Try a different term or clear the search."),
          button({ onclick: () => { search.val = "" } }, "Clear search"),
        )
      }
      return div({ class: "items-table-wrapper" },
        table({ class: "responsive-table" },
          tr(
            th("Name"),
            th("Category"),
            th("Aliases"),
            th({ class: "money" }, "Actions"),
          ),
          ...list.map(item => {
            const catName = categories.val[item.categoryId]?.name || "Uncategorized"
            return tr(
              td({ "data-label": "Name" },
                a({
                  href: `#/items/${item.itemId}`,
                  class: "item-name-link",
                  onclick: (e: Event) => {
                    e.preventDefault()
                    navigate(`/items/${item.itemId}`)
                  },
                }, item.name),
              ),
              td({ "data-label": "Category" },
                span({ class: "category-badge" }, catName),
              ),
              td({ "data-label": "Aliases" }, formatAliases(item.aliases)),
              td({ "data-label": "Actions" },
                button({ onclick: () => navigate(`/items/${item.itemId}`) }, "View"),
              ),
            )
          }),
        ),
      )
    },
  )
}

export default ItemsPage
