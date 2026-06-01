import van from "vanjs-core"
import { api } from "../main"
import { Chart, registerables } from "chart.js"

Chart.register(...registerables)

const { div, h1, h2, canvas, select, option } = van.tags

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
  const spendingData = van.state<any[]>([])
  const categoryData = van.state<any[]>([])
  const familyData = van.state<any[]>([])
  const loading = van.state(true)

  const loadData = async () => {
    loading.val = true
    try {
      const [spending, categories, family] = await Promise.all([
        api.get(`/analysis/spending?granularity=${granularity.val}`),
        api.get("/analysis/categories"),
        api.get("/analysis/family"),
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

  // Charts will be initialized after DOM is ready
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
          backgroundColor: "#3b82f6",
        }],
      })
    }
  }

  // Initialize charts after render
  setTimeout(initCharts, 100)

  return div({ class: "analysis-page" },
    div({ class: "page-header" },
      h1("Analysis"),
      select({
        value: granularity,
        onchange: (e: Event) => {
          granularity.val = (e.target as HTMLSelectElement).value
          loadData()
          setTimeout(initCharts, 100)
        },
      },
        option({ value: "day" }, "Daily"),
        option({ value: "week" }, "Weekly"),
        option({ value: "month" }, "Monthly"),
      ),
    ),
    () => loading.val
      ? div("Loading...")
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
