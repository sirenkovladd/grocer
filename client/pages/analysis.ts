import van from "vanjs-core"
import { api } from "../main"
import { Chart, registerables } from "chart.js"
import DateRange from "../components/date-range"
import { toCsv, downloadFile } from "../utils"

Chart.register(...registerables)

const { div, h1, h2, canvas, select, option, button } = van.tags

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
  const loading = van.state(true)

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

  loadData()

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
  )
}

export default AnalysisPage
