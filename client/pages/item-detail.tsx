import { createEffect, onCleanup, For, Show } from "solid-js"
import { A, useParams } from "@solidjs/router"
import { Chart, registerables } from "chart.js"
import QueryBoundary from "../components/QueryBoundary.tsx"
import { createQuery } from "../lib/query.ts"
import { api, navigate } from "../lib/api.ts"
import { formatDateTime, formatMoney, indexBy, setPageTitle } from "../utils.ts"

Chart.register(...registerables)

// ID fields are `string` (uint64 precision safety).
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

// PricePoint comes from /api/analysis/items/{id}. NOTE: this endpoint
// is inconsistent with the rest of the API:
//   - `date` is a "2006-01-02" string (not Unix seconds)
//   - `price` is in DOLLARS (not cents)
// Page-level conversion handles both warts.
interface PricePoint {
  date: string
  price: number
}

function buildPriceChart(el: HTMLCanvasElement, history: PricePoint[]): Chart {
  return new Chart(el, {
    type: "line",
    data: {
      labels: history.map((h) => h.date),
      datasets: [
        {
          label: "Price",
          // Y-axis is in dollars; formatMoney uses cents. Keep chart
          // data in dollars and format tick labels manually.
          data: history.map((h) => h.price),
          borderColor: "#3b82f6",
          backgroundColor: "rgba(59, 130, 246, 0.1)",
          fill: true,
          tension: 0.3,
        },
      ],
    },
    options: {
      responsive: true,
      plugins: {
        legend: {
          labels: { color: "#e5e5e5" },
        },
      },
      scales: {
        x: {
          ticks: { color: "#a0a0a0" },
          grid: { color: "#2e2e2e" },
        },
        y: {
          ticks: {
            color: "#a0a0a0",
            callback: (value) => `$${Number(value).toFixed(2)}`,
          },
          grid: { color: "#2e2e2e" },
        },
      },
    },
  })
}

// Price chart section. The chart is (re)built in an effect guarded by
// resource state — never reading a pending/errored resource, so no
// suspense throw outside a boundary and no stale chart when the
// route param changes (component is reused across /items/:id).
// Canvas mounts only when there is history, hence the !el guard.
function ChartSection(props: {
  state: () => string
  history: () => PricePoint[] | undefined
}) {
  let el: HTMLCanvasElement | undefined
  let chart: Chart | null = null
  onCleanup(() => chart?.destroy())

  createEffect(() => {
    if (props.state() !== "ready") return
    const h = props.history() ?? []
    if (h.length === 0 || !el) return
    chart?.destroy()
    chart = buildPriceChart(el, h)
  })

  return (
    <div class="chart-container card">
      <h2>Price History</h2>
      <Show
        when={(props.state() === "ready" ? (props.history()?.length ?? 0) : 0) > 0}
        fallback={
          <div class="empty-state">
            <p>No purchases yet — chart will appear after the first receipt.</p>
            <button onClick={() => navigate("/receipts/upload")}>Upload a receipt</button>
          </div>
        }
      >
        <canvas ref={el} id="price-chart" />
      </Show>
    </div>
  )
}

interface ItemData {
  item: Item | null
  history: PricePoint[]
  categories: Record<string, Category>
}

export default function ItemDetailPage() {
  const params = useParams()

  const { data } = createQuery(async (): Promise<ItemData> => {
    const id = params.id
    const [itemData, historyData, cats] = await Promise.all([
      api.get(`/items/${id}`),
      api.get(`/analysis/items/${id}`),
      api.get("/categories"),
    ])
    return {
      item: itemData,
      history: Array.isArray(historyData) ? historyData : [],
      categories: indexBy(Array.isArray(cats) ? cats : [], (c: Category) => c.categoryId),
    }
  })

  // Keep the tab title in sync with what's on screen. Guarded by
  // resource state (never reads a pending/errored resource): "Item"
  // default from the router shows during load only if we say so —
  // here we set explicit loading/not-found titles.
  createEffect(() => {
    if (data.state === "pending" || data.state === "unresolved") {
      setPageTitle("Loading item…")
      return
    }
    if (data.state === "errored" || !data()?.item) {
      setPageTitle("Item not found")
      return
    }
    setPageTitle(data()!.item!.name)
  })

  // Calculate price stats. Prices are in dollars; multiply by 100
  // for formatMoney.
  const stats = () => {
    const history = data()!.history
    if (history.length === 0) return null
    const prices = history.map((h) => h.price)
    const min = Math.min(...prices)
    const max = Math.max(...prices)
    const avg = prices.reduce((a, b) => a + b, 0) / prices.length
    const latest = history[history.length - 1]!.price
    const trend =
      history.length > 1
        ? latest > history[0]!.price
          ? "up"
          : latest < history[0]!.price
            ? "down"
            : "stable"
        : "stable"
    return { min, max, avg, latest, trend }
  }

  // Body reads the resource (suspending until ready). Closure
  // component so data reads stay inside the boundary.
  const Body = () => {
    const d = () => data()!
    const it = () => d().item!
    const catName = () => d().categories[it().categoryId]?.name || "Uncategorized"
    const s = () => stats()
    return (
      <>
        {/* Breadcrumb */}
        <div class="breadcrumb">
          <A href="/items">Items</A>
          <span class="separator">›</span>
          <span class="current">{it().name}</span>
        </div>

        {/* Header */}
        <div class="page-header">
          <h1>{it().name}</h1>
          <button onClick={() => navigate("/items")}>Back</button>
        </div>

        {/* Quick-action: receipts page pre-filtered to this item via
            the ?item= server-side filter. */}
        <div class="item-quick-actions">
          <button class="btn-secondary" onClick={() => navigate(`/receipts?item=${it().itemId}`)}>
            View receipts using this item
          </button>
        </div>

        {/* Info card with category badge */}
        <div class="item-info card">
          <p>
            <span class="muted">Category: </span>
            <span class="category-badge">{catName()}</span>
          </p>
          <p class="muted">Normalized: {it().normalized}</p>
          <p>
            <span class="muted">Aliases: </span>
            {it().aliases.length > 0 ? (
              it().aliases.join(", ")
            ) : (
              <span class="muted">None</span>
            )}
          </p>
        </div>

        {/* Stats */}
        <Show when={s()}>
          <div class="price-stats">
            <div class="stat-card card">
              <p class="stat-label">Latest Price</p>
              <p class="stat-value money">{formatMoney(s()!.latest * 100)}</p>
            </div>
            <div class="stat-card card">
              <p class="stat-label">Average Price</p>
              <p class="stat-value money">{formatMoney(s()!.avg * 100)}</p>
            </div>
            <div class="stat-card card">
              <p class="stat-label">Min Price</p>
              <p class="stat-value money">{formatMoney(s()!.min * 100)}</p>
            </div>
            <div class="stat-card card">
              <p class="stat-label">Max Price</p>
              <p class="stat-value money">{formatMoney(s()!.max * 100)}</p>
            </div>
            <div class="stat-card card">
              <p class="stat-label">Trend</p>
              <p class={`stat-value trend-${s()!.trend}`}>{s()!.trend}</p>
            </div>
          </div>
        </Show>

        <ChartSection state={() => data.state} history={() => data()?.history} />

        {/* Purchase history table */}
        <Show when={d().history.length > 0}>
          <div class="purchase-history card">
            <h2>Purchase History</h2>
            <div class="items-table-wrapper">
              <table class="responsive-table">
                <tr>
                  <th>Date</th>
                  <th class="money">Price</th>
                </tr>
                <For each={d().history}>
                  {(h) => {
                    // Analysis endpoint date is "2006-01-02" (no
                    // time/timezone). Parse as local midnight so the
                    // display matches the user's calendar day.
                    const unixSecs = Math.floor(new Date(h.date + "T00:00:00").getTime() / 1000)
                    return (
                      <tr>
                        <td data-label="Date">{formatDateTime(unixSecs)}</td>
                        <td data-label="Price" class="money">
                          {formatMoney(h.price * 100)}
                        </td>
                      </tr>
                    )
                  }}
                </For>
              </table>
            </div>
          </div>
        </Show>
      </>
    )
  }

  return (
    <div class="item-detail-page">
      <QueryBoundary
        pending={
          <div>
            <div class="skeleton-header">
              <div class="skeleton-line skeleton-merchant" />
              <div class="skeleton-line skeleton-date" />
            </div>
            <p class="muted">Loading…</p>
          </div>
        }
      >
        <Show
          when={data() ? data()!.item : null}
          fallback={
            <div class="empty-state">
              <h2>Item not found</h2>
              <button onClick={() => navigate("/items")}>Back to items</button>
            </div>
          }
        >
          <Body />
        </Show>
      </QueryBoundary>
    </div>
  )
}
