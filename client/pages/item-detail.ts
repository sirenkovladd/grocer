import van from "vanjs-core"
import { api, navigate } from "../main"
import { formatDate, formatMoney, indexBy } from "../utils"
import { Chart, registerables } from "chart.js"

Chart.register(...registerables)

const { div, h1, h2, a, span, canvas, table, tr, td, th, button, p } = van.tags

// ID fields are `string` (uint64 precision safety). See ticket 04
// decisions log.
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
// Page-level conversion handles both warts. A backend fix to align
// with the rest of the API is a future ticket.
interface PricePoint {
  date: string
  price: number
}

const SkeletonItem = () =>
  tr(
    td(div({ class: "skeleton-cell skeleton-cell-md" })),
    td(div({ class: "skeleton-cell skeleton-cell-sm" })),
  )

const ItemDetailPage = () => {
  const item = van.state<Item | null>(null)
  const history = van.state<PricePoint[]>([])
  const categories = van.state<Record<string, Category>>({})
  const loading = van.state(true)
  const error = van.state<string | null>(null)

  const loadData = async () => {
    const id = window.location.hash.split("/").pop()
    if (!id) return

    loading.val = true
    error.val = null
    try {
      const [itemData, historyData, cats] = await Promise.all([
        api.get(`/items/${id}`),
        api.get(`/analysis/items/${id}`),
        api.get("/categories"),
      ])
      item.val = itemData
      history.val = Array.isArray(historyData) ? historyData : []
      categories.val = indexBy(Array.isArray(cats) ? cats : [], (c: Category) => c.categoryId)
    } catch (err) {
      console.error("Failed to load item:", err)
      error.val = (err as Error).message || "Failed to load item"
    } finally {
      loading.val = false
    }
  }

  loadData()

  let priceChart: Chart | null = null

  // The chart must run after the canvas is in the DOM. The existing
  // setTimeout(100ms) hack is fragile but works for the typical render
  // path. A proper integration with VanJS reactive subscriptions
  // would be a refactor.
  const initChart = () => {
    const canvas = document.getElementById("price-chart") as HTMLCanvasElement | null
    if (!canvas || history.val.length === 0) return

    if (priceChart) priceChart.destroy()
    priceChart = new Chart(canvas, {
      type: "line",
      data: {
        labels: history.val.map(h => h.date),
        datasets: [{
          label: "Price",
          // The chart's Y-axis is in dollars; formatMoney uses cents.
          // We keep the chart data in dollars and format the Y-axis
          // tick labels manually (no need to multiply).
          data: history.val.map(h => h.price),
          borderColor: "#3b82f6",
          backgroundColor: "rgba(59, 130, 246, 0.1)",
          fill: true,
          tension: 0.3,
        }],
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

  setTimeout(initChart, 100)

  // Calculate price stats. `prices` is in dollars (see PricePoint
  // note above); we multiply by 100 when calling formatMoney.
  const getPriceStats = () => {
    if (history.val.length === 0) return null

    const prices = history.val.map(h => h.price)
    const min = Math.min(...prices)
    const max = Math.max(...prices)
    const avg = prices.reduce((a, b) => a + b, 0) / prices.length
    const latest = history.val[history.val.length - 1].price
    const trend = history.val.length > 1
      ? latest > history.val[0].price ? "up" : latest < history.val[0].price ? "down" : "stable"
      : "stable"

    return { min, max, avg, latest, trend }
  }

  return div({ class: "item-detail-page" },
    () => {
      if (loading.val) {
        return div(
          div({ class: "skeleton-header" },
            div({ class: "skeleton-line skeleton-merchant" }),
            div({ class: "skeleton-line skeleton-date" }),
          ),
          p({ class: "muted" }, "Loading…"),
        )
      }
      if (error.val) {
        return div({ class: "empty-state" },
          h2("Couldn't load item"),
          p(error.val),
          button({ onclick: loadData }, "Try Again"),
        )
      }
      if (!item.val) {
        return div({ class: "empty-state" },
          h2("Item not found"),
          button({ onclick: () => navigate("/items") }, "Back to items"),
        )
      }

      const it = item.val
      const catName = categories.val[it.categoryId]?.name || "Uncategorized"
      const stats = getPriceStats()

      return div(
        // Breadcrumb
        div({ class: "breadcrumb" },
          a({
            href: "#/items",
            onclick: (e: Event) => { e.preventDefault(); navigate("/items") },
          }, "Items"),
          span({ class: "separator" }, "›"),
          span({ class: "current" }, it.name),
        ),

        // Header
        div({ class: "page-header" },
          h1(it.name),
          button({ onclick: () => navigate("/items") }, "Back"),
        ),

        // Info card with category badge
        div({ class: "item-info card" },
          p({},
            span({ class: "muted" }, "Category: "),
            span({ class: "category-badge" }, catName),
          ),
          p({ class: "muted" }, `Normalized: ${it.normalized}`),
          p({},
            span({ class: "muted" }, "Aliases: "),
            it.aliases.length > 0 ? it.aliases.join(", ") : span({ class: "muted" }, "None"),
          ),
        ),

        // Stats
        stats
          ? div({ class: "price-stats" },
              div({ class: "stat-card card" },
                p({ class: "stat-label" }, "Latest Price"),
                p({ class: "stat-value money" }, formatMoney(stats.latest * 100)),
              ),
              div({ class: "stat-card card" },
                p({ class: "stat-label" }, "Average Price"),
                p({ class: "stat-value money" }, formatMoney(stats.avg * 100)),
              ),
              div({ class: "stat-card card" },
                p({ class: "stat-label" }, "Min Price"),
                p({ class: "stat-value money" }, formatMoney(stats.min * 100)),
              ),
              div({ class: "stat-card card" },
                p({ class: "stat-label" }, "Max Price"),
                p({ class: "stat-value money" }, formatMoney(stats.max * 100)),
              ),
              div({ class: "stat-card card" },
                p({ class: "stat-label" }, "Trend"),
                p({ class: `stat-value trend-${stats.trend}` }, stats.trend),
              ),
            )
          : "",

        // Chart
        div({ class: "chart-container card" },
          h2("Price History"),
          history.val.length > 0
            ? canvas({ id: "price-chart" })
            : div({ class: "empty-state" },
                p("No purchases yet — chart will appear after the first receipt."),
                button({ onclick: () => navigate("/receipts/upload") },
                  "Upload a receipt"),
              ),
        ),

        // Purchase history table
        history.val.length > 0
          ? div({ class: "purchase-history card" },
              h2("Purchase History"),
              div({ class: "items-table-wrapper" },
                table({ class: "responsive-table" },
                  tr(th("Date"), th({ class: "money" }, "Price")),
                  ...history.val.map(h => {
                    // Analysis endpoint returns date as "2006-01-02"
                    // (no time, no timezone). Parse as local midnight
                    // so formatDate displays the same day the user
                    // expects — not yesterday in negative-UTC zones.
                    const unixSecs = Math.floor(new Date(h.date + "T00:00:00").getTime() / 1000)
                    return tr(
                      td({ "data-label": "Date" }, formatDate(unixSecs)),
                      td({ "data-label": "Price", class: "money" }, formatMoney(h.price * 100)),
                    )
                  }),
                ),
              ),
            )
          : "",
      )
    },
  )
}

export default ItemDetailPage
