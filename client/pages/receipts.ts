import van from "vanjs-core"
import { ReceiptCard, type EnrichedReceiptSummary } from "../components/receipt-card"
import { api, navigate } from "../main"
import { indexBy, parseHashQuery } from "../utils"

const { div, h1, h3, p, button, input, select, option } = van.tags

interface Merchant {
  merchantId: string
  name: string
}

interface User {
  userId: string
  name: string
}

const SkeletonRow = () =>
  div({ class: "skeleton-row" },
    div({ class: "skeleton-cell skeleton-cell-lg" }),
    div({ class: "skeleton-cell skeleton-cell-md" }),
    div({ class: "skeleton-cell skeleton-cell-sm" }),
  )

const ReceiptsPage = () => {
  const receipts = van.state<EnrichedReceiptSummary[]>([])
  const merchants = van.state<Record<string, Merchant>>({})
  const users = van.state<Record<string, User>>({})
  const loading = van.state(true)
  const error = van.state<string | null>(null)

  // Filters
  const search = van.state("")
  const from = van.state("")
  const to = van.state("")
  const ownerFilter = van.state("")
  const merchantFilter = van.state("")

  // The item filter is a deep-link filter: it can only be set via the
  // URL (?item=...) and is read on mount. There is no UI to set it
  // (the only entry point is the item detail page). The server filters
  // by item ID, so we don't need to pass itemIds down with each
  // receipt. We do, however, need the item's name to render the
  // banner — fetched lazily from /api/items/{id} when the filter is
  // active.
  const itemFilter = van.state<string>("")
  const itemFilterName = van.state<string>("")

  const loadData = async () => {
    loading.val = true
    error.val = null
    try {
      // If the URL has ?item=..., pass it to the server. The client-
      // side filters (search, from, to, owner, merchant) apply on top
      // of the server-filtered list. Server-side filtering is used
      // here so the client doesn't have to download every receipt
      // just to find the few that contain a specific item.
      const params = new URLSearchParams()
      if (itemFilter.val) params.set("item", itemFilter.val)
      const query = params.toString()
      const url = "/receipts/enriched" + (query ? `?${query}` : "")

      const [r, m, u] = await Promise.all([
        api.get(url),
        api.get("/merchants"),
        api.get("/users"),
      ])
      receipts.val = Array.isArray(r) ? r : []
      merchants.val = indexBy(Array.isArray(m) ? m : [], (x: Merchant) => x.merchantId)
      users.val = indexBy(Array.isArray(u) ? u : [], (x: User) => x.userId)
    } catch (err) {
      console.error("Failed to load receipts:", err)
      error.val = (err as Error).message || "Failed to load receipts"
    } finally {
      loading.val = false
    }
  }

  // Read the item filter from the URL on mount. parseHashQuery
  // returns a plain object from the query string portion of the hash
  // route. The item ID is sent as a string (uint64 precision safety).
  const urlItem = parseHashQuery(window.location.hash.slice(1) || "/").item
  if (urlItem) {
    itemFilter.val = urlItem
    // Fetch the item name for the banner. Fire-and-forget; the
    // banner shows just the ID until the name arrives.
    api.get(`/items/${urlItem}`)
      .then((it: any) => { if (it?.name) itemFilterName.val = it.name })
      .catch(() => { /* leave name empty, banner falls back to ID */ })
  }

  loadData()

  // Filter — pure function called inside the reactive render. Re-runs
  // when any of the filter states or `receipts` change.
  //
  // Date parsing: `new Date("2024-05-29")` is UTC midnight by JS spec,
  // but the user picked the date in their LOCAL time. Append
  // "T00:00:00" so the parser treats it as local midnight — the filter
  // boundary matches what the user sees in the date input. The same
  // pattern is used in item-detail.ts and merchants.ts for the
  // analysis endpoint's "2006-01-02" strings.
  const filtered = (): EnrichedReceiptSummary[] => {
    const all = receipts.val
    const s = search.val.trim().toLowerCase()
    const fromSecs = from.val ? new Date(from.val + "T00:00:00").getTime() / 1000 : null
    const toSecs = to.val ? new Date(to.val + "T23:59:59").getTime() / 1000 : null
    const ownerId = ownerFilter.val
    const merchantId = merchantFilter.val

    return all.filter(r => {
      if (s && !r.merchantName.toLowerCase().includes(s)) return false
      if (fromSecs !== null && r.date < fromSecs) return false
      if (toSecs !== null && r.date > toSecs) return false
      if (ownerId && r.ownerId !== ownerId) return false
      if (merchantId && r.merchantId !== merchantId) return false
      return true
    })
  }

  return div({ class: "receipts-page" },
    div({ class: "page-header" },
      h1("Receipts"),
      button({ onclick: () => navigate("/receipts/upload") }, "Upload Receipt"),
    ),

    // "Filtered by item" banner. Shown when the URL has ?item=...
    // (e.g. when the user clicked "View receipts" on the item detail
    // page). The clear button strips the query param from the URL
    // and reloads the page so the server filter is dropped.
    () => itemFilter.val
      ? div({ class: "filter-banner" },
          span({},
            "Showing receipts containing ",
            span({ class: "filter-banner-value" },
              itemFilterName.val || `#${itemFilter.val}`,
            ),
          ),
          button({
            class: "btn-sm btn-secondary",
            onclick: () => navigate("/receipts"),
          }, "Clear filter"),
        )
      : "",

    // Filter bar — with dynamic options
    div({ class: "filter-bar" },
      input({
        type: "search",
        class: "search-input",
        placeholder: "Search merchant…",
        value: search,
        oninput: (e: Event) => {
          search.val = (e.target as HTMLInputElement).value
        },
      }),
      input({
        type: "date",
        value: from,
        oninput: (e: Event) => {
          from.val = (e.target as HTMLInputElement).value
        },
        "aria-label": "From date",
      }),
      input({
        type: "date",
        value: to,
        oninput: (e: Event) => {
          to.val = (e.target as HTMLInputElement).value
        },
        "aria-label": "To date",
      }),
      // The entire <select> is wrapped in a function-child so that
      // reading `users.val` / `merchants.val` is scoped to this
      // function-child, not the surrounding App function-child.
      // Without this, VanJS captures the reads as dependencies of
      // App, pushing the App binding into users._bindings /
      // merchants._bindings. When loadData() later sets those
      // states, App re-evaluates, calls ReceiptsPage() again,
      // creates new state objects, and loops forever.
      //
      // The function-child must return a single element (not an
      // array) — see https://vanjs.org/tutorial#api-tags
      () => select({
        value: ownerFilter,
        onchange: (e: Event) => {
          ownerFilter.val = (e.target as HTMLSelectElement).value
        },
        "aria-label": "Filter by owner",
      },
        option({ value: "" }, "All owners"),
        ...Object.values(users.val)
          .slice()
          .sort((a, b) => a.name.localeCompare(b.name))
          .map((u: User) =>
            option({ value: u.userId }, u.name),
          ),
      ),
      () => select({
        value: merchantFilter,
        onchange: (e: Event) => {
          merchantFilter.val = (e.target as HTMLSelectElement).value
        },
        "aria-label": "Filter by merchant",
      },
        option({ value: "" }, "All merchants"),
        ...Object.values(merchants.val)
          .slice()
          .sort((a, b) => a.name.localeCompare(b.name))
          .map((m: Merchant) =>
            option({ value: m.merchantId }, m.name),
          ),
      ),
    ),

    // Body — reactive on loading, error, and filtered list.
    () => {
      if (error.val) {
        return div({ class: "empty-state" },
          h3("Couldn't load receipts"),
          p(error.val),
          button({ onclick: loadData }, "Try Again"),
        )
      }
      if (loading.val) {
        return div({ class: "receipts-skeleton" },
          SkeletonRow(),
          SkeletonRow(),
          SkeletonRow(),
          SkeletonRow(),
          SkeletonRow(),
        )
      }
      const list = filtered()
      if (list.length === 0) {
        const hasFilters = !!(search.val || from.val || to.val || ownerFilter.val || merchantFilter.val)
        return div({ class: "empty-state" },
          h3(hasFilters ? "No receipts match your filters" : "No receipts yet"),
          p(hasFilters
            ? "Try adjusting or clearing your filters."
            : "Upload your first receipt to get started."),
          !hasFilters
            ? button({ onclick: () => navigate("/receipts/upload") }, "Upload your first receipt")
            : button({ onclick: () => {
                search.val = ""; from.val = ""; to.val = ""
                ownerFilter.val = ""; merchantFilter.val = ""
              } }, "Clear filters"),
        )
      }
      return div({ class: "receipts-grid" },
        ...list.map(r => ReceiptCard(r)),
      )
    },
  )
}

export default ReceiptsPage
