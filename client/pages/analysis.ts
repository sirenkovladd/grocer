import van from "vanjs-core"
import { api, navigate } from "../main"
import { Chart, registerables } from "chart.js"
import DateRange from "../components/date-range"
import { toCsv, downloadFile, formatMoney, formatDate } from "../utils"

Chart.register(...registerables)

const { div, h1, h2, h3, p, canvas, select, option, button, span, a } = van.tags

interface EnrichedReceipt {
  receiptId: string
  merchantId: string
  merchantName: string
  ownerId: string
  ownerName: string
  date: number
  itemCount: number
  totalCents: number
}

// Group key for a "trip": a single person visiting a single
// merchant on a single local day. Two receipts from the same
// person at the same store on the same day are one trip (the
// family stopped twice, the LLM parsed two photos, but it's
// one shopping visit). Time-of-day is intentionally ignored.
const tripKey = (r: EnrichedReceipt): string => {
  const d = new Date(r.date * 1000)
  const yyyy = d.getFullYear()
  const mm = String(d.getMonth() + 1).padStart(2, "0")
  const dd = String(d.getDate()).padStart(2, "0")
  return `${r.ownerId}|${r.merchantId}|${yyyy}-${mm}-${dd}`
}

interface Trip {
  key: string
  ownerId: string
  ownerName: string
  merchantId: string
  merchantName: string
  date: number
  receipts: EnrichedReceipt[]
  totalCents: number
  itemCount: number
}

// Group an enriched-receipt list into trips. Sorted most-recent
// first; trips with multiple receipts are sorted by their earliest
// receipt's date.
const groupTrips = (receipts: EnrichedReceipt[]): Trip[] => {
  const map = new Map<string, Trip>()
  for (const r of receipts) {
    const key = tripKey(r)
    let trip = map.get(key)
    if (!trip) {
      trip = {
        key,
        ownerId: r.ownerId,
        ownerName: r.ownerName,
        merchantId: r.merchantId,
        merchantName: r.merchantName,
        date: r.date,
        receipts: [],
        totalCents: 0,
        itemCount: 0,
      }
      map.set(key, trip)
    }
    trip.receipts.push(r)
    trip.totalCents += r.totalCents
    trip.itemCount += r.itemCount
    if (r.date < trip.date) trip.date = r.date
  }
  const trips = Array.from(map.values())
  trips.sort((a, b) => b.date - a.date)
  return trips
}

const createLineChart = (canvas: HTMLCanvasElement, data: any) => {
  return new Chart(canvas, {
    type: "line",
    data,
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
          ticks: { color: "#a0a0a0" },
          grid: { color: "#2e2e2e" },
        },
      },
    },
  })
}

const createPieChart = (canvas: HTMLCanvasElement, data: any) => {
  return new Chart(canvas, {
    type: "pie",
    data,
    options: {
      responsive: true,
      plugins: {
        legend: {
          position: "bottom",
          labels: { color: "#e5e5e5" },
        },
      },
    },
  })
}

const createBarChart = (canvas: HTMLCanvasElement, data: any) => {
  return new Chart(canvas, {
    type: "bar",
    data,
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
          ticks: { color: "#a0a0a0" },
          grid: { color: "#2e2e2e" },
        },
      },
    },
  })
}

const AnalysisPage = () => {
  const granularity = van.state("month")
  const from = van.state("")
  const to = van.state("")
  const spendingData = van.state<any[]>([])
  const categoryData = van.state<any[]>([])
  const familyData = van.state<any[]>([])
  // Trips are computed from the enriched receipt list. The fetch
  // is independent of the analysis endpoints; we just reuse the
  // same date range filter so the trips section stays in sync.
  const receipts = van.state<EnrichedReceipt[]>([])
  const loading = van.state(true)
  const loadingReceipts = van.state(false)

  const loadData = async () => {
    loading.val = true
    try {
      const params = new URLSearchParams({ granularity: granularity.val })
      if (from.val) params.set("from", from.val)
      if (to.val) params.set("to", to.val)

      const [spending, categories, family] = await Promise.all([
        api.get(`/analysis/spending?${params}`),
        api.get(`/analysis/categories?${params}`),
        api.get(`/analysis/family?${params}`),
      ])
      spendingData.val = spending || []
      categoryData.val = categories || []
      familyData.val = family || []
    } catch (err) {
      console.error("Failed to load analysis:", err)
    }
    loading.val = false
  }

  // Trips come from a separate fetch. We use the date range params
  // so the trips respect the user's filter. Don't await in parallel
  // with loadData() — trips are an additional view and shouldn't
  // block the main charts.
  const loadReceipts = async () => {
    loadingReceipts.val = true
    try {
      const params = new URLSearchParams()
      if (from.val) params.set("from", from.val)
      if (to.val) params.set("to", to.val)
      const data = await api.get(`/receipts/enriched?${params}`)
      receipts.val = Array.isArray(data) ? data : []
    } catch (err) {
      console.error("Failed to load trips:", err)
      receipts.val = []
    } finally {
      loadingReceipts.val = false
    }
  }

  loadData()
  loadReceipts()

  let spendingChart: Chart | null = null
  let categoryChart: Chart | null = null
  let familyChart: Chart | null = null

  const initCharts = () => {
    const spendingCanvas = document.getElementById("spending-chart") as HTMLCanvasElement
    const categoryCanvas = document.getElementById("category-chart") as HTMLCanvasElement
    const familyCanvas = document.getElementById("family-chart") as HTMLCanvasElement

    if (spendingCanvas && spendingData.val.length > 0) {
      if (spendingChart) spendingChart.destroy()
      spendingChart = createLineChart(spendingCanvas, {
        labels: spendingData.val.map((d: any) => d.period),
        datasets: [{
          label: "Spending",
          data: spendingData.val.map((d: any) => d.total),
          borderColor: "#3b82f6",
          backgroundColor: "rgba(59, 130, 246, 0.1)",
          fill: true,
        }],
      })
    }

    if (categoryCanvas && categoryData.val.length > 0) {
      if (categoryChart) categoryChart.destroy()
      categoryChart = createPieChart(categoryCanvas, {
        labels: categoryData.val.map((d: any) => d.name),
        datasets: [{
          label: "Spending by Category",
          data: categoryData.val.map((d: any) => d.total),
          backgroundColor: [
            "#3b82f6", "#22c55e", "#eab308", "#ef4444",
            "#8b5cf6", "#ec4899", "#14b8a6", "#f97316",
            "#6366f1", "#84cc16", "#14b8a6", "#f43f5e",
          ],
        }],
      })
    }

    if (familyCanvas && familyData.val.length > 0) {
      if (familyChart) familyChart.destroy()
      familyChart = createBarChart(familyCanvas, {
        labels: familyData.val.map((d: any) => d.name),
        datasets: [{
          label: "Spending by Member",
          data: familyData.val.map((d: any) => d.total),
          backgroundColor: ["#3b82f6", "#22c55e", "#eab308", "#ef4444"],
        }],
      })
    }
  }

  setTimeout(initCharts, 100)

  const handleFilterChange = () => {
    loadData()
    loadReceipts()
    setTimeout(initCharts, 100)
  }

  // CSV export. Each button serializes the already-loaded chart data
  // and triggers a browser download. No new API call — the data is
  // exactly what's on screen.
  //
  // The data from the analysis endpoints is in dollars (not cents,
  // unlike the receipt DTOs). We round to 2 decimals for the CSV so
  // it matches what the user sees on the chart.
  const exportSpendingCsv = () => {
    if (spendingData.val.length === 0) return
    const csv = toCsv(
      ["Period", "Total ($)"],
      spendingData.val.map((d: any) => [d.period, d.total.toFixed(2)]),
    )
    const dateTag = `${from.val || "all"}_to_${to.val || "now"}`
    downloadFile(`grocer-spending-${dateTag}.csv`, csv, "text/csv")
  }

  const exportCategoriesCsv = () => {
    if (categoryData.val.length === 0) return
    const csv = toCsv(
      ["Category ID", "Category", "Total ($)"],
      categoryData.val.map((d: any) => [d.categoryId, d.name, d.total.toFixed(2)]),
    )
    const dateTag = `${from.val || "all"}_to_${to.val || "now"}`
    downloadFile(`grocer-categories-${dateTag}.csv`, csv, "text/csv")
  }

  const exportFamilyCsv = () => {
    if (familyData.val.length === 0) return
    const csv = toCsv(
      ["User ID", "Member", "Total ($)"],
      familyData.val.map((d: any) => [d.userId, d.name, d.total.toFixed(2)]),
    )
    const dateTag = `${from.val || "all"}_to_${to.val || "now"}`
    downloadFile(`grocer-family-${dateTag}.csv`, csv, "text/csv")
  }

  return div({ class: "analysis-page" },
    div({ class: "page-header" },
      h1("Analysis"),
      select({
        value: granularity,
        onchange: (e: Event) => {
          granularity.val = (e.target as HTMLSelectElement).value
          handleFilterChange()
        },
      },
        option({ value: "day" }, "Daily"),
        option({ value: "week" }, "Weekly"),
        option({ value: "month" }, "Monthly"),
      ),
    ),
    DateRange({ from, to, onChange: handleFilterChange }),

    // Export buttons — one per chart, disabled when there's no data.
    // A small grouped button row keeps them visually together.
    div({ class: "export-buttons" },
      button({
        class: "btn-sm btn-secondary",
        onclick: exportSpendingCsv,
        disabled: () => spendingData.val.length === 0 || loading.val,
      }, "Export spending CSV"),
      button({
        class: "btn-sm btn-secondary",
        onclick: exportCategoriesCsv,
        disabled: () => categoryData.val.length === 0 || loading.val,
      }, "Export categories CSV"),
      button({
        class: "btn-sm btn-secondary",
        onclick: exportFamilyCsv,
        disabled: () => familyData.val.length === 0 || loading.val,
      }, "Export family CSV"),
    ),

    () => loading.val
      ? div({ class: "loading" }, "Loading...")
      : (spendingData.val.length === 0 &&
         categoryData.val.length === 0 &&
         familyData.val.length === 0)
        ? div({ class: "empty-state" },
            h3("No data in the selected range"),
            p("Upload a receipt, or widen the date range above."),
            button({ onclick: () => navigate("/receipts/upload") },
              "Upload a receipt"),
          )
        : div({ class: "charts-grid" },
            div({ class: "chart-container card" },
              h2("Spending Over Time"),
              canvas({ id: "spending-chart" }),
            ),
            div({ class: "chart-container card" },
              h2("Category Breakdown"),
              canvas({ id: "category-chart" }),
            ),
            div({ class: "chart-container card" },
              h2("Family Member Spending"),
              canvas({ id: "family-chart" }),
            ),
          ),

    // Trips section — one card per (owner × merchant × day)
    // group, computed client-side from the enriched receipt list.
    // Respects the same from/to date filter as the charts above.
    div({ class: "trips-section" },
      h2("Trips"),
      () => {
        if (loadingReceipts.val) {
          return div({ class: "muted" }, "Loading…")
        }
        const trips = groupTrips(receipts.val)
        if (trips.length === 0) {
          return div({ class: "empty-state" },
            p("No trips in the selected date range."),
          )
        }
        return div({ class: "trips-grid" },
          ...trips.map(trip =>
            div({ class: "trip-card card", key: trip.key },
              div({ class: "trip-header" },
                span({ class: "trip-merchant" }, trip.merchantName),
                span({ class: "trip-date muted" }, formatDate(trip.date)),
              ),
              div({ class: "trip-meta" },
                span(`by ${trip.ownerName || "Unknown"}`),
                trip.receipts.length > 1
                  ? span({ class: "trip-multi" }, `${trip.receipts.length} receipts`)
                  : "",
                span(`${trip.itemCount} items`),
                span({ class: "money" }, formatMoney(trip.totalCents)),
              ),
              div({ class: "trip-receipts" },
                ...trip.receipts.map(rcpt =>
                  a({
                    href: `#/receipts/${rcpt.receiptId}`,
                    class: "trip-receipt-link",
                    onclick: (e: Event) => {
                      e.preventDefault()
                      navigate(`/receipts/${rcpt.receiptId}`)
                    },
                  }, `View ${rcpt.receiptId.slice(-6)}`),
                ),
              ),
            ),
          ),
        )
      },
    ),
  )
}

export default AnalysisPage
