import { createEffect, createSignal, onCleanup, For, Show } from "solid-js"
import { A } from "@solidjs/router"
import { Chart, registerables } from "chart.js"
import DateRange from "../components/date-range.tsx"
import QueryBoundary from "../components/QueryBoundary.tsx"
import { createQuery } from "../lib/query.ts"
import { api, navigate } from "../lib/api.ts"
import { toCsv, downloadFile, formatMoney, formatDateTime } from "../utils.ts"

Chart.register(...registerables)

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
// person at the same store on the same day are one trip. Time-of-day
// is intentionally ignored.
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

// Group an enriched-receipt list into trips, most-recent first.
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

const PIE_COLORS = [
  "#3b82f6", "#22c55e", "#eab308", "#ef4444",
  "#8b5cf6", "#ec4899", "#14b8a6", "#f97316",
  "#6366f1", "#84cc16", "#14b8a6", "#f43f5e",
]

interface SpendingPoint {
  period: string
  total: number
}
interface CategoryPoint {
  categoryId: string | number
  name: string
  total: number
}
interface FamilyPoint {
  userId: string | number
  name: string
  total: number
}

export default function AnalysisPage() {
  const [granularity, setGranularity] = createSignal("month")
  const [from, setFrom] = createSignal("")
  const [to, setTo] = createSignal("")

  // Main trio. The fetcher reads the filter signals, so changing
  // granularity / from / to refetches automatically — no
  // handleFilterChange needed. Errors suspend to the boundary's
  // retry card (the original logged and showed empties; retry is
  // strictly more useful and matches every other page).
  const { data } = createQuery(async () => {
    const params = new URLSearchParams({ granularity: granularity() })
    if (from()) params.set("from", from())
    if (to()) params.set("to", to())
    const [spending, categories, family] = await Promise.all([
      api.get(`/analysis/spending?${params}`),
      api.get(`/analysis/categories?${params}`),
      api.get(`/analysis/family?${params}`),
    ])
    return {
      spending: (spending || []) as SpendingPoint[],
      categories: (categories || []) as CategoryPoint[],
      family: (family || []) as FamilyPoint[],
    }
  })

  // Full category list (with parentId) for the root-only pie filter.
  // Independent fetch with silent fallback: failure shows every
  // category instead of blocking the page.
  const [catLookup, setCatLookup] = createSignal<{ categoryId: string; parentId: string | null }[]>([])
  createEffect(() => {
    api
      .get("/categories")
      .then((d) => setCatLookup(Array.isArray(d) ? d : []))
      .catch(() => setCatLookup([]))
  })

  // Trips from the enriched receipt list, respecting from/to.
  // Independent of the main trio; failures show an empty section.
  const [receipts, setReceipts] = createSignal<EnrichedReceipt[]>([])
  const [loadingReceipts, setLoadingReceipts] = createSignal(false)
  createEffect(() => {
    const params = new URLSearchParams()
    if (from()) params.set("from", from())
    if (to()) params.set("to", to())
    const query = params.toString()
    setLoadingReceipts(true)
    api
      .get(`/receipts/enriched${query ? `?${query}` : ""}`)
      .then((d) => setReceipts(Array.isArray(d) ? d : []))
      .catch(() => setReceipts([]))
      .finally(() => setLoadingReceipts(false))
  })

  // Root-only Category Breakdown. IDs normalized to strings: the
  // analysis endpoint serializes categoryId as a JSON number while
  // /categories uses strings — without normalization the Set lookup
  // never matches and the filter removes everything. While the
  // lookup hasn't loaded, return unfiltered data so the chart still
  // renders on first paint.
  const filteredCategories = (): CategoryPoint[] => {
    const lookup = catLookup()
    const cats = data.state === "ready" ? data()!.categories : []
    if (lookup.length === 0) return cats
    const rootIds = new Set(lookup.filter((c) => !c.parentId).map((c) => String(c.categoryId)))
    return cats.filter((d) => rootIds.has(String(d.categoryId)))
  }

  // Charts. One effect, guarded by resource state: builds on first
  // ready, rebuilds on refetch/remount, picks up the late-arriving
  // category lookup via tracking. Destroy-before-rebuild +
  // onCleanup cover param/filter churn. (Read-first shape: the 2.0
  // compute→apply split lands on the same line.)
  let spendingEl: HTMLCanvasElement | undefined
  let categoryEl: HTMLCanvasElement | undefined
  let familyEl: HTMLCanvasElement | undefined
  let spendingChart: Chart | null = null
  let categoryChart: Chart | null = null
  let familyChart: Chart | null = null
  const destroyCharts = () => {
    spendingChart?.destroy()
    categoryChart?.destroy()
    familyChart?.destroy()
    spendingChart = categoryChart = familyChart = null
  }
  onCleanup(destroyCharts)

  createEffect(() => {
    if (data.state !== "ready") return
    const d = data()!
    const filtered = filteredCategories()

    if (spendingEl && d.spending.length > 0) {
      spendingChart?.destroy()
      spendingChart = new Chart(spendingEl, {
        type: "line",
        data: {
          labels: d.spending.map((p) => p.period),
          datasets: [
            {
              label: "Spending",
              data: d.spending.map((p) => p.total),
              borderColor: "#3b82f6",
              backgroundColor: "rgba(59, 130, 246, 0.1)",
              fill: true,
            },
          ],
        },
        options: {
          responsive: true,
          plugins: { legend: { labels: { color: "#e5e5e5" } } },
          scales: {
            x: { ticks: { color: "#a0a0a0" }, grid: { color: "#2e2e2e" } },
            y: { ticks: { color: "#a0a0a0" }, grid: { color: "#2e2e2e" } },
          },
        },
      })
    } else {
      spendingChart?.destroy()
      spendingChart = null
    }

    if (categoryEl && filtered.length > 0) {
      categoryChart?.destroy()
      categoryChart = new Chart(categoryEl, {
        type: "pie",
        data: {
          labels: filtered.map((p) => p.name),
          datasets: [
            {
              label: "Spending by Category",
              data: filtered.map((p) => p.total),
              backgroundColor: PIE_COLORS,
            },
          ],
        },
        options: {
          responsive: true,
          plugins: { legend: { position: "bottom", labels: { color: "#e5e5e5" } } },
        },
      })
    } else {
      categoryChart?.destroy()
      categoryChart = null
    }

    if (familyEl && d.family.length > 0) {
      familyChart?.destroy()
      familyChart = new Chart(familyEl, {
        type: "bar",
        data: {
          labels: d.family.map((p) => p.name),
          datasets: [
            {
              label: "Spending by Member",
              data: d.family.map((p) => p.total),
              backgroundColor: ["#3b82f6", "#22c55e", "#eab308", "#ef4444"],
            },
          ],
        },
        options: {
          responsive: true,
          plugins: { legend: { labels: { color: "#e5e5e5" } } },
          scales: {
            x: { ticks: { color: "#a0a0a0" }, grid: { color: "#2e2e2e" } },
            y: { ticks: { color: "#a0a0a0" }, grid: { color: "#2e2e2e" } },
          },
        },
      })
    } else {
      familyChart?.destroy()
      familyChart = null
    }
  })

  // CSV exports serialize the on-screen data (dollars, 2 decimals —
  // matching the charts). No new API call.
  const dateTag = () => `${from() || "all"}_to_${to() || "now"}`
  const exportSpendingCsv = () => {
    const d = data.state === "ready" ? data()!.spending : []
    if (d.length === 0) return
    downloadFile(
      `grocer-spending-${dateTag()}.csv`,
      toCsv(
        ["Period", "Total ($)"],
        d.map((p) => [p.period, p.total.toFixed(2)]),
      ),
      "text/csv",
    )
  }
  const exportCategoriesCsv = () => {
    const f = filteredCategories()
    if (f.length === 0) return
    downloadFile(
      `grocer-categories-${dateTag()}.csv`,
      toCsv(
        ["Category ID", "Category", "Total ($)"],
        f.map((p) => [String(p.categoryId), p.name, p.total.toFixed(2)]),
      ),
      "text/csv",
    )
  }
  const exportFamilyCsv = () => {
    const d = data.state === "ready" ? data()!.family : []
    if (d.length === 0) return
    downloadFile(
      `grocer-family-${dateTag()}.csv`,
      toCsv(
        ["User ID", "Member", "Total ($)"],
        d.map((p) => [String(p.userId), p.name, p.total.toFixed(2)]),
      ),
      "text/csv",
    )
  }

  // Charts section reads the resource (suspending until ready).
  // Closure component so reads stay inside the boundary.
  const ChartsSection = () => {
    const d = () => data() ?? { spending: [], categories: [], family: [] }
    const empty =
      () => d().spending.length === 0 && d().categories.length === 0 && d().family.length === 0
    return (
      <Show
        when={!empty()}
        fallback={
          <div class="empty-state">
            <h3>No data in the selected range</h3>
            <p>Upload a receipt, or widen the date range above.</p>
            <button onClick={() => navigate("/receipts/upload")}>Upload a receipt</button>
          </div>
        }
      >
        <div class="charts-grid">
          <div class="chart-container card">
            <h2>Spending Over Time</h2>
            <canvas ref={spendingEl} id="spending-chart" />
          </div>
          <div class="chart-container card">
            <h2>Category Breakdown</h2>
            <canvas ref={categoryEl} id="category-chart" />
          </div>
          <div class="chart-container card">
            <h2>Family Member Spending</h2>
            <canvas ref={familyEl} id="family-chart" />
          </div>
        </div>
      </Show>
    )
  }

  return (
    <div class="analysis-page">
      <div class="page-header">
        <h1>Analysis</h1>
        <select
          onChange={(e) => setGranularity(e.currentTarget.value)}
          aria-label="Granularity"
        >
          <option value="day" selected={granularity() === "day"}>
            Daily
          </option>
          <option value="week" selected={granularity() === "week"}>
            Weekly
          </option>
          <option value="month" selected={granularity() === "month"}>
            Monthly
          </option>
        </select>
      </div>
      <DateRange from={from} setFrom={setFrom} to={to} setTo={setTo} />

      <div class="export-buttons">
        <button
          class="btn-sm btn-secondary"
          onClick={exportSpendingCsv}
          disabled={data.state !== "ready" || data()!.spending.length === 0}
        >
          Export spending CSV
        </button>
        <button
          class="btn-sm btn-secondary"
          onClick={exportCategoriesCsv}
          disabled={data.state !== "ready" || filteredCategories().length === 0}
        >
          Export categories CSV
        </button>
        <button
          class="btn-sm btn-secondary"
          onClick={exportFamilyCsv}
          disabled={data.state !== "ready" || data()!.family.length === 0}
        >
          Export family CSV
        </button>
      </div>

      <QueryBoundary pending={<div class="loading">Loading...</div>}>
        <ChartsSection />
      </QueryBoundary>

      {/* Trips: (owner × merchant × day) groups from the enriched
          list. Own loading state, outside the charts boundary. */}
      <div class="trips-section">
        <h2>Trips</h2>
        <Show
          when={!loadingReceipts()}
          fallback={<div class="muted">Loading…</div>}
        >
          <Show
            when={groupTrips(receipts()).length > 0}
            fallback={
              <div class="empty-state">
                <p>No trips in the selected date range.</p>
              </div>
            }
          >
            <div class="trips-grid">
              <For each={groupTrips(receipts())}>
                {(trip) => (
                  <div class="trip-card card">
                    <div class="trip-header">
                      <span class="trip-merchant">{trip.merchantName}</span>
                      <span class="trip-date muted">{formatDateTime(trip.date)}</span>
                    </div>
                    <div class="trip-meta">
                      <span>by {trip.ownerName || "Unknown"}</span>
                      <Show when={trip.receipts.length > 1}>
                        <span class="trip-multi">{`${trip.receipts.length} receipts`}</span>
                      </Show>
                      <span>{`${trip.itemCount} items`}</span>
                      <span class="money">{formatMoney(trip.totalCents)}</span>
                    </div>
                    <div class="trip-receipts">
                      <For each={trip.receipts}>
                        {(rcpt) => (
                          <A
                            href={`/receipts/${rcpt.receiptId}`}
                            class="trip-receipt-link"
                          >{`View ${rcpt.receiptId.slice(-6)}`}</A>
                        )}
                      </For>
                    </div>
                  </div>
                )}
              </For>
            </div>
          </Show>
        </Show>
      </div>
    </div>
  )
}
