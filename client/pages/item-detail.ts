import van from "vanjs-core"
import { api, navigate } from "../main"
import { Chart, registerables } from "chart.js"

Chart.register(...registerables)

const { div, h1, h2, canvas, table, tr, td, th, button, p } = van.tags

interface Item {
  itemId: number
  name: string
  categoryId: number
  merchantId: number
  normalized: string
  aliases: string[]
}

interface PricePoint {
  date: string
  price: number
}

const ItemDetailPage = () => {
  const item = van.state<Item | null>(null)
  const history = van.state<PricePoint[]>([])
  const loading = van.state(true)

  const loadData = async () => {
    const id = window.location.hash.split("/").pop()
    if (!id) return

    loading.val = true
    try {
      const [itemData, historyData] = await Promise.all([
        api.get(`/items/${id}`),
        api.get(`/analysis/items/${id}`),
      ])
      item.val = itemData
      history.val = historyData || []
    } catch (err) {
      console.error("Failed to load item:", err)
    }
    loading.val = false
  }

  loadData()

  let priceChart: Chart | null = null

  const initChart = () => {
    const canvas = document.getElementById("price-chart") as HTMLCanvasElement
    if (!canvas || history.val.length === 0) return

    if (priceChart) priceChart.destroy()
    priceChart = new Chart(canvas, {
      type: "line",
      data: {
        labels: history.val.map(h => h.date),
        datasets: [{
          label: "Price",
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
              callback: (value) => `$${value}`,
            },
            grid: { color: "#2e2e2e" },
          },
        },
      },
    })
  }

  setTimeout(initChart, 100)

  // Calculate price stats
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
    () => loading.val
      ? div({ class: "loading" }, "Loading...")
      : !item.val
        ? div("Item not found")
        : div(
            div({ class: "page-header" },
              h1(item.val.name),
              button({ onclick: () => navigate("/items") }, "Back"),
            ),
            div({ class: "item-info card" },
              p(`Normalized: ${item.val.normalized}`),
              p(`Aliases: ${item.val.aliases.join(", ") || "None"}`),
            ),
            () => {
              const stats = getPriceStats()
              return stats ? div({ class: "price-stats" },
                div({ class: "stat-card card" },
                  p({ class: "stat-label" }, "Latest Price"),
                  p({ class: "stat-value" }, `$${stats.latest.toFixed(2)}`),
                ),
                div({ class: "stat-card card" },
                  p({ class: "stat-label" }, "Average Price"),
                  p({ class: "stat-value" }, `$${stats.avg.toFixed(2)}`),
                ),
                div({ class: "stat-card card" },
                  p({ class: "stat-label" }, "Min Price"),
                  p({ class: "stat-value" }, `$${stats.min.toFixed(2)}`),
                ),
                div({ class: "stat-card card" },
                  p({ class: "stat-label" }, "Max Price"),
                  p({ class: "stat-value" }, `$${stats.max.toFixed(2)}`),
                ),
                div({ class: "stat-card card" },
                  p({ class: "stat-label" }, "Trend"),
                  p({ class: `stat-value trend-${stats.trend}` }, stats.trend),
                ),
              ) : ""
            },
            div({ class: "chart-container card" },
              h2("Price History"),
              canvas({ id: "price-chart" }),
            ),
            history.val.length > 0
              ? div({ class: "purchase-history card" },
                  h2("Purchase History"),
                  table(
                    tr(th("Date"), th("Price")),
                    ...history.val.map(h =>
                      tr(
                        td(new Date(h.date).toLocaleDateString()),
                        td(`$${h.price.toFixed(2)}`),
                      )
                    ),
                  ),
                )
              : "",
          ),
  )
}

export default ItemDetailPage
