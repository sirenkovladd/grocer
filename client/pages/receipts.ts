import van from "vanjs-core"
import { api, navigate } from "../main"
import { formatDate, formatMoney, indexBy } from "../utils"

const { div, h1, h2, h3, p, button, input, select, option, span } = van.tags

// ID fields are typed as `string` because the backend serializes uint64
// as a JSON string (json:"...,string") and the values exceed
// Number.MAX_SAFE_INTEGER. The previous type used `number` which
// silently loses precision for large IDs (see ticket 04 decisions log).
// Existing pages still use `number`; migrating them is a follow-up.
interface EnrichedReceiptSummary {
  receiptId: string
  merchantId: string
  merchantName: string
  ownerId: string
  ownerName: string // not displayed in this view per UX overhaul plan
  date: number
  itemCount: number
  totalCents: number
  photoUrl?: string
}

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

const ReceiptCard = (r: EnrichedReceiptSummary) =>
  div({ class: "receipt-card card", onclick: () => navigate(`/receipts/${r.receiptId}`) },
    div({ class: "receipt-header" },
      div({ class: "receipt-merchant" }, r.merchantName),
      div({ class: "receipt-date muted" }, formatDate(r.date)),
    ),
    div({ class: "receipt-meta" },
      span(`${r.itemCount} ${r.itemCount === 1 ? "item" : "items"}`),
      span({ class: "money" }, formatMoney(r.totalCents)),
    ),
  )

const ReceiptsPage = () => {
  const receipts = van.state<EnrichedReceiptSummary[]>([])
  const merchants = van.state<Record<string, Merchant>>({})
  const users = van.state<Record<string, User>>({})
  const loading = van.state(true)
  const error = van.state<string | null>(null)

  // Filters — all client-side per the ticket 07 design.
  const search = van.state("")
  const from = van.state("")
  const to = van.state("")
  const ownerFilter = van.state("")
  const merchantFilter = van.state("")

  const loadData = async () => {
    loading.val = true
    error.val = null
    try {
      // Three parallel fetches: enriched receipts, merchants (for
      // merchant-filter dropdown), users (for owner-filter dropdown).
      // Owner names are loaded but not displayed in this view per the
      // UX overhaul plan; they power the dropdown only.
      const [r, m, u] = await Promise.all([
        api.get("/receipts/enriched"),
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

  loadData()

  // Filter — pure function called inside the reactive render. Re-runs
  // when any of the filter states or `receipts` change.
  const filtered = (): EnrichedReceiptSummary[] => {
    const all = receipts.val
    const s = search.val.trim().toLowerCase()
    const fromSecs = from.val ? new Date(from.val).getTime() / 1000 : null
    // Add 86399 seconds to include the entire `to` day.
    const toSecs = to.val ? new Date(to.val).getTime() / 1000 + 86399 : null
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

    // Filter bar — always visible so users can scope down before/after load.
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
      select({
        value: ownerFilter,
        onchange: (e: Event) => {
          ownerFilter.val = (e.target as HTMLSelectElement).value
        },
        "aria-label": "Filter by owner",
      },
        option({ value: "" }, "All owners"),
        ...Object.values(users.val).map((u: User) =>
          option({ value: u.userId }, u.name),
        ),
      ),
      select({
        value: merchantFilter,
        onchange: (e: Event) => {
          merchantFilter.val = (e.target as HTMLSelectElement).value
        },
        "aria-label": "Filter by merchant",
      },
        option({ value: "" }, "All merchants"),
        ...Object.values(merchants.val).map((m: Merchant) =>
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
