import { createSignal, For, Show, Suspense } from "solid-js"
import { createResource } from "solid-js"
import { useLocation } from "@solidjs/router"
import ReceiptCard, { type EnrichedReceiptSummary } from "../components/receipt-card.tsx"
import QueryBoundary from "../components/QueryBoundary.tsx"
import { createQuery } from "../lib/query.ts"
import { api, navigate } from "../lib/api.ts"
import { indexBy, parseHashQuery } from "../utils.ts"

interface Merchant {
  merchantId: string
  name: string
}

interface User {
  userId: string
  name: string
}

function SkeletonRows() {
  return (
    <div class="receipts-skeleton">
      <For each={[0, 1, 2, 3, 4]}>
        {() => (
          <div class="skeleton-row">
            <div class="skeleton-cell skeleton-cell-lg" />
            <div class="skeleton-cell skeleton-cell-md" />
            <div class="skeleton-cell skeleton-cell-sm" />
          </div>
        )}
      </For>
    </div>
  )
}

export default function ReceiptsPage() {
  const location = useLocation()
  const [search, setSearch] = createSignal("")
  const [from, setFrom] = createSignal("")
  const [to, setTo] = createSignal("")
  const [ownerFilter, setOwnerFilter] = createSignal("")
  const [merchantFilter, setMerchantFilter] = createSignal("")

  // Deep-link filter: set only via the URL (?item=..., the only entry
  // point is the item detail page). The server filters by item ID so
  // the client doesn't download every receipt to find the few that
  // contain one item. Read from the router location (reactive, so
  // back/forward navigation refetches); parseHashQuery fallback
  // covers hash shapes the router doesn't split into search.
  const itemParam = (): string => {
    const q = new URLSearchParams(location.search).get("item")
    if (q) return q
    return parseHashQuery(window.location.hash.slice(1) || "/").item ?? ""
  }

  const { data, refetch } = createQuery(async () => {
    // If the URL has ?item=..., pass it to the server. The
    // client-side filters (search, from, to, owner, merchant) apply
    // on top of the server-filtered list.
    const params = new URLSearchParams()
    const item = itemParam()
    if (item) params.set("item", item)
    const query = params.toString()
    const url = "/receipts/enriched" + (query ? `?${query}` : "")

    const [r, m, u] = await Promise.all([
      api.get(url),
      api.get("/merchants"),
      api.get("/users"),
    ])
    return {
      receipts: (Array.isArray(r) ? r : []) as EnrichedReceiptSummary[],
      merchants: indexBy(Array.isArray(m) ? m : [], (x: Merchant) => x.merchantId),
      users: indexBy(Array.isArray(u) ? u : [], (x: User) => x.userId),
    }
  })

  // Item name for the filter banner. Keyed resource: a falsy source
  // never calls the fetcher and stays unresolved (no suspense), so
  // the unfiltered page renders without waiting. The one deliberate
  // direct createResource use — createQuery always fetches.
  const [itemName] = createResource(itemParam, async (id) => {
    if (!id) return ""
    try {
      const it = await api.get(`/items/${id}`)
      return it?.name ?? ""
    } catch {
      return ""
    }
  })

  // Filter — pure function of the loaded data + filter signals.
  // Called in JSX, so it re-runs when any of them change.
  //
  // Date parsing: `new Date("2024-05-29")` is UTC midnight by JS spec,
  // but the user picked the date in their LOCAL time. Append
  // "T00:00:00" so the parser treats it as local midnight.
  const filtered = (): EnrichedReceiptSummary[] => {
    const { receipts: all } = data()!
    const s = search().trim().toLowerCase()
    const fromSecs = from() ? new Date(from() + "T00:00:00").getTime() / 1000 : null
    const toSecs = to() ? new Date(to() + "T23:59:59").getTime() / 1000 : null
    const ownerId = ownerFilter()
    const merchantId = merchantFilter()

    return all.filter((r) => {
      if (s && !r.merchantName.toLowerCase().includes(s)) return false
      if (fromSecs !== null && r.date < fromSecs) return false
      if (toSecs !== null && r.date > toSecs) return false
      if (ownerId && r.ownerId !== ownerId) return false
      if (merchantId && r.merchantId !== merchantId) return false
      return true
    })
  }

  const sortedUsers = (): User[] =>
    Object.values(data()!.users)
      .slice()
      .sort((a, b) => a.name.localeCompare(b.name))

  const sortedMerchants = (): Merchant[] =>
    Object.values(data()!.merchants)
      .slice()
      .sort((a, b) => a.name.localeCompare(b.name))

  const clearFilters = () => {
    setSearch("")
    setFrom("")
    setTo("")
    setOwnerFilter("")
    setMerchantFilter("")
  }

  // Per-option `selected` (not select `value=`): the browser records
  // select.value at element-creation time, before options are
  // appended, so a value prop is silently dropped on mount. Selected
  // attributes on options are respected regardless of append order
  // and stay reactive. The VanJS manual-sel.value workaround is gone.
  return (
    <div class="receipts-page">
      <div class="page-header">
        <h1>Receipts</h1>
        <button onClick={() => navigate("/receipts/upload")}>Upload Receipt</button>
      </div>

      {/* "Filtered by item" banner, shown when the URL has ?item=...
          (user clicked "View receipts" on an item page). Clear strips
          the query and the reactive itemParam refetches unfiltered.
          (Fixes a VanJS bug: the old banner referenced an
          undeclared `span` and crashed whenever the filter was set.) */}
      <Show when={itemParam()}>
        <Suspense>
          <div class="filter-banner">
            <span>
              Showing receipts containing{" "}
              <span class="filter-banner-value">{itemName() || `#${itemParam()}`}</span>
            </span>
            <button class="btn-sm btn-secondary" onClick={() => navigate("/receipts")}>
              Clear filter
            </button>
          </div>
        </Suspense>
      </Show>

      <QueryBoundary pending={<SkeletonRows />}>
        <div class="filter-bar">
          <input
            type="search"
            class="search-input"
            placeholder="Search merchant…"
            value={search()}
            onInput={(e) => setSearch(e.currentTarget.value)}
          />
          <input
            type="date"
            value={from()}
            onInput={(e) => setFrom(e.currentTarget.value)}
            aria-label="From date"
          />
          <input
            type="date"
            value={to()}
            onInput={(e) => setTo(e.currentTarget.value)}
            aria-label="To date"
          />
          <select
            onChange={(e) => setOwnerFilter(e.currentTarget.value)}
            aria-label="Filter by owner"
          >
            <option value="" selected={ownerFilter() === ""}>
              All owners
            </option>
            <For each={sortedUsers()}>
              {(u) => (
                <option value={u.userId} selected={ownerFilter() === u.userId}>
                  {u.name}
                </option>
              )}
            </For>
          </select>
          <select
            onChange={(e) => setMerchantFilter(e.currentTarget.value)}
            aria-label="Filter by merchant"
          >
            <option value="" selected={merchantFilter() === ""}>
              All merchants
            </option>
            <For each={sortedMerchants()}>
              {(m) => (
                <option value={m.merchantId} selected={merchantFilter() === m.merchantId}>
                  {m.name}
                </option>
              )}
            </For>
          </select>
        </div>

        <Show
          when={filtered().length > 0}
          fallback={
            <EmptyState
              hasItemFilter={itemParam() !== ""}
              hasFilters={!!(search() || from() || to() || ownerFilter() || merchantFilter())}
              onClear={clearFilters}
            />
          }
        >
          <div class="receipts-grid">
            <For each={filtered()}>{(r) => <ReceiptCard {...r} />}</For>
          </div>
        </Show>
      </QueryBoundary>
    </div>
  )
}

function EmptyState(props: { hasItemFilter: boolean; hasFilters: boolean; onClear: () => void }) {
  const empty = !props.hasFilters && !props.hasItemFilter
  return (
    <div class="empty-state">
      <h3>
        {props.hasItemFilter
          ? "No receipts contain this item"
          : props.hasFilters
            ? "No receipts match your filters"
            : "No receipts yet"}
      </h3>
      <p>
        {props.hasFilters
          ? "Try adjusting or clearing your filters."
          : empty
            ? "Upload your first receipt to get started."
            : ""}
      </p>
      {props.hasFilters ? (
        <button onClick={props.onClear}>Clear filters</button>
      ) : empty ? (
        <button onClick={() => navigate("/receipts/upload")}>Upload your first receipt</button>
      ) : null}
    </div>
  )
}
