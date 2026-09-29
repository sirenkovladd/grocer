import { createSignal, For, Show } from "solid-js"
import QueryBoundary from "../components/QueryBoundary.tsx"
import ReceiptCard, { type EnrichedReceiptSummary } from "../components/receipt-card.tsx"
import { createQuery } from "../lib/query.ts"
import { api, navigate } from "../lib/api.ts"
import { formatMoney, formatRelativeDate } from "../utils.ts"

interface ProposalItem {
  parsedName: string
  quantity: number
  unitPriceCents: number
  matchedItemId: string
  categoryId: string
  isNewCategory: boolean
  userChoice: string
}

interface Proposal {
  proposalId: string
  ownerId: string
  merchant: string
  date: number
  photoUrl: string
  items: ProposalItem[]
  totalCents: number
  status: string
}

function StatusBadge(props: { status: string }) {
  const classes: Record<string, string> = {
    parsing: "badge-parsing",
    pending: "badge-pending",
    failed: "badge-failed",
    approved: "badge-approved",
  }
  return <span class={`badge ${classes[props.status] || ""}`}>{props.status}</span>
}

function SkeletonProposalCard() {
  return (
    <div class="proposal-form card">
      <div class="skeleton-line skeleton-merchant" />
      <div class="skeleton-line skeleton-date" />
    </div>
  )
}

function SkeletonReceiptRow() {
  return (
    <div class="skeleton-row">
      <div class="skeleton-cell skeleton-cell-lg" />
      <div class="skeleton-cell skeleton-cell-md" />
      <div class="skeleton-cell skeleton-cell-sm" />
    </div>
  )
}

function ProposalCard(props: { proposal: Proposal; onAction: () => void }) {
  const [deleting, setDeleting] = createSignal(false)
  const p = () => props.proposal

  const handleRetry = async () => {
    try {
      await api.post(`/proposals/${p().proposalId}/reparse`, {})
      props.onAction()
    } catch (err) {
      console.error("Failed to retry proposal:", err)
    }
  }

  const handleDelete = async () => {
    if (!confirm("Delete this proposal?")) return
    setDeleting(true)
    try {
      await api.delete(`/proposals/${p().proposalId}`)
      props.onAction()
    } catch (err) {
      console.error("Failed to delete proposal:", err)
    } finally {
      setDeleting(false)
    }
  }

  const items = () => p().items || []
  // Recent dates show relative ("3 days ago"); older ones switch to
  // absolute formatDate via formatRelativeDate's internal threshold.
  const dateStr = () => (p().date ? formatRelativeDate(p().date) : "")
  const total = () => formatMoney(p().totalCents)

  if (p().status === "parsing") {
    return (
      <div class="proposal-form card">
        <div class="card-header">
          <h3>Parsing receipt...</h3>
          <StatusBadge status="parsing" />
        </div>
        <div class="parsing-indicator">
          <div class="spinner" />
          <span>{`${items().length} items found so far`}</span>
        </div>
        <div class="card-actions">
          <button onClick={() => navigate(`/proposals/${p().proposalId}`)}>Watch Progress</button>
          <button onClick={handleDelete} disabled={deleting()} class="btn-danger">
            Delete
          </button>
        </div>
      </div>
    )
  }

  if (p().status === "failed") {
    return (
      <div class="proposal-form card">
        <div class="card-header">
          <h3>Parse Failed</h3>
          <StatusBadge status="failed" />
        </div>
        <p>An error occurred while parsing this receipt</p>
        <div class="card-actions">
          <button onClick={handleRetry} class="retry-btn">
            Retry
          </button>
          <button onClick={() => navigate(`/proposals/${p().proposalId}`)} class="btn-secondary">
            View
          </button>
          <button onClick={handleDelete} disabled={deleting()} class="btn-danger">
            Delete
          </button>
        </div>
      </div>
    )
  }

  // Default: pending
  return (
    <div class="proposal-form card">
      <div class="card-header">
        <h3>{p().merchant || "Unknown"}</h3>
        <StatusBadge status="pending" />
      </div>
      <div class="card-meta">
        <span class="muted">{dateStr()}</span>
        <span>{`${items().length} items`}</span>
        <span class="money">{total()}</span>
      </div>
      <div class="card-actions">
        <button onClick={() => navigate(`/proposals/${p().proposalId}`)} class="btn-primary">
          View & Edit
        </button>
        <button onClick={handleDelete} disabled={deleting()} class="btn-danger">
          Delete
        </button>
      </div>
    </div>
  )
}

// Compute this-month and last-month totals from the already-loaded
// enriched receipts (no separate /api/analysis call — the list
// endpoint has all the data at family scale).
function monthStats(receipts: EnrichedReceiptSummary[]):
  | { total: number; count: number; avg: number; lastMonthTotal: number | null }
  | null {
  if (receipts.length === 0) return null
  const now = new Date()
  const year = now.getFullYear()
  const month = now.getMonth()
  // last month: rollover Dec → Jan handled by JS Date arithmetic
  const lastMonthDate = new Date(year, month - 1, 1)
  const lastYear = lastMonthDate.getFullYear()
  const lastMonth = lastMonthDate.getMonth()

  let total = 0
  let count = 0
  let lastMonthTotal = 0
  let lastMonthCount = 0
  for (const r of receipts) {
    const d = new Date(r.date * 1000)
    if (d.getFullYear() === year && d.getMonth() === month) {
      total += r.totalCents
      count++
    } else if (d.getFullYear() === lastYear && d.getMonth() === lastMonth) {
      lastMonthTotal += r.totalCents
      lastMonthCount++
    }
  }
  if (count === 0 && lastMonthCount === 0) return null
  const avg = count > 0 ? total / count : 0
  return { total, count, avg, lastMonthTotal: lastMonthCount > 0 ? lastMonthTotal : null }
}

// Format a cents delta as "+12% vs last month". Null when no
// meaningful comparison (no receipts last month).
function deltaPct(current: number, last: number | null): string | null {
  if (last === null || last === 0) return null
  const pct = Math.round(((current - last) / last) * 100)
  if (pct === 0) return "same as last month"
  return `${pct > 0 ? "+" : ""}${pct}% vs last month`
}

export default function HomePage() {
  const { data: proposals, refetch: refetchProposals } = createQuery(async () => {
    const d = await api.get("/proposals")
    return (Array.isArray(d) ? d : []) as Proposal[]
  })
  const { data: receipts } = createQuery(async () => {
    // Enriched endpoint — the receipt cards need merchantName.
    const d = await api.get("/receipts/enriched")
    return (Array.isArray(d) ? d : []) as EnrichedReceiptSummary[]
  })

  // Cap "Recent Receipts" at 10; matches the previous home-page limit.
  const recentReceipts = () => receipts()!.slice(0, 10)

  // Body reads both resources (suspending until ready). Closure
  // component so reads stay inside the boundary.
  const Body = () => {
    const stats = () => monthStats(receipts()!)
    const delta = () => {
      const s = stats()
      return s ? deltaPct(s.total, s.lastMonthTotal) : null
    }
    return (
      <>
        {/* This-month summary. Skipped when there's no data at all
            (the "No receipts yet" empty state below carries it). */}
        <Show when={stats()}>
          <div class="home-stats">
            <div class="stat-card card">
              <p class="stat-label">This Month</p>
              <p class="stat-value money">{formatMoney(stats()!.total)}</p>
              <Show when={delta()}>
                <p class="stat-delta muted">{delta()}</p>
              </Show>
            </div>
            <div class="stat-card card">
              <p class="stat-label">Receipts</p>
              <p class="stat-value">{String(stats()!.count)}</p>
              <p class="stat-delta muted">this month</p>
            </div>
            <div class="stat-card card">
              <p class="stat-label">Average per Trip</p>
              <p class="stat-value money">
                {stats()!.count > 0 ? formatMoney(Math.round(stats()!.avg)) : "—"}
              </p>
              <p class="stat-delta muted">
                {stats()!.count > 0 ? `across ${stats()!.count} receipt${stats()!.count === 1 ? "" : "s"}` : ""}
              </p>
            </div>
          </div>
        </Show>

        <div class="home-section">
          <div class="section-header">
            <h2>Pending Proposals</h2>
            <Show when={proposals()!.length > 0}>
              <span class="section-count">{proposals()!.length}</span>
            </Show>
          </div>
          <Show
            when={proposals()!.length > 0}
            fallback={
              <div class="empty-section">
                <p>No pending proposals</p>
              </div>
            }
          >
            <div class="cards-grid">
              <For each={proposals()!}>
                {(p) => <ProposalCard proposal={p} onAction={refetchProposals} />}
              </For>
            </div>
          </Show>
        </div>

        <div class="home-section">
          <div class="section-header">
            <h2>Recent Receipts</h2>
            <button onClick={() => navigate("/receipts")} class="btn-secondary btn-sm">
              View All
            </button>
          </div>
          <Show
            when={receipts()!.length > 0}
            fallback={
              <div class="empty-section">
                <p>No receipts yet</p>
              </div>
            }
          >
            <div class="cards-grid">
              <For each={recentReceipts()}>{(r) => <ReceiptCard {...r} />}</For>
            </div>
          </Show>
        </div>
      </>
    )
  }

  return (
    <div class="home-page">
      <div class="page-header">
        <h1>Home</h1>
        <button onClick={() => navigate("/receipts/upload")} class="btn-primary">
          Upload Receipt
        </button>
      </div>
      <QueryBoundary
        pending={
          <div class="cards-grid">
            <SkeletonProposalCard />
            <SkeletonProposalCard />
            <SkeletonProposalCard />
            <SkeletonReceiptRow />
            <SkeletonReceiptRow />
            <SkeletonReceiptRow />
          </div>
        }
      >
        <Body />
      </QueryBoundary>
    </div>
  )
}
