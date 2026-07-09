import van from "vanjs-core"
import { api, navigate } from "../main"
import { formatMoney, formatRelativeDate } from "../utils"
import { ReceiptCard, type EnrichedReceiptSummary } from "../components/receipt-card"

const { div, h1, h2, h3, button, span, p } = van.tags

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

const statusBadge = (status: string) => {
  const classes: Record<string, string> = {
    parsing: "badge-parsing",
    pending: "badge-pending",
    failed: "badge-failed",
    approved: "badge-approved",
  }
  return span({ class: `badge ${classes[status] || ""}` }, status)
}

// Skeleton card matching the proposal card footprint.
const SkeletonProposalCard = () =>
  div({ class: "proposal-form card" },
    div({ class: "skeleton-line skeleton-merchant" }),
    div({ class: "skeleton-line skeleton-date" }),
  )

const SkeletonReceiptRow = () =>
  div({ class: "skeleton-row" },
    div({ class: "skeleton-cell skeleton-cell-lg" }),
    div({ class: "skeleton-cell skeleton-cell-md" }),
    div({ class: "skeleton-cell skeleton-cell-sm" }),
  )

const ProposalCard = (proposal: Proposal, onAction: () => void) => {
  const deleting = van.state(false)

  const handleRetry = async () => {
    try {
      await api.post(`/proposals/${proposal.proposalId}/reparse`, {})
      onAction()
    } catch (err) {
      console.error("Failed to retry proposal:", err)
    }
  }

  const handleDelete = async () => {
    if (!confirm("Delete this proposal?")) return
    deleting.val = true
    try {
      await api.delete(`/proposals/${proposal.proposalId}`)
      onAction()
    } catch (err) {
      console.error("Failed to delete proposal:", err)
    } finally {
      deleting.val = false
    }
  }

  const items = proposal.items || []
  const itemCount = items.length
  // Recent dates show relative ("3 days ago"); older ones switch to
  // absolute formatDate via formatRelativeDate's internal threshold.
  const dateStr = proposal.date ? formatRelativeDate(proposal.date) : ""
  const total = formatMoney(proposal.totalCents)

  if (proposal.status === "parsing") {
    return div({ class: "proposal-form card" },
      div({ class: "card-header" },
        h3("Parsing receipt..."),
        statusBadge("parsing"),
      ),
      div({ class: "parsing-indicator" },
        div({ class: "spinner" }),
        span(`${itemCount} items found so far`),
      ),
      div({ class: "card-actions" },
        button({ onclick: () => navigate(`/proposals/${proposal.proposalId}`) }, "Watch Progress"),
        button({ onclick: handleDelete, disabled: deleting, class: "btn-danger" }, "Delete"),
      ),
    )
  }

  if (proposal.status === "failed") {
    return div({ class: "proposal-form card" },
      div({ class: "card-header" },
        h3("Parse Failed"),
        statusBadge("failed"),
      ),
      p("An error occurred while parsing this receipt"),
      div({ class: "card-actions" },
        button({ onclick: handleRetry, class: "retry-btn" }, "Retry"),
        button({ onclick: () => navigate(`/proposals/${proposal.proposalId}`), class: "btn-secondary" }, "View"),
        button({ onclick: handleDelete, disabled: deleting, class: "btn-danger" }, "Delete"),
      ),
    )
  }

  // Default: pending
  return div({ class: "proposal-form card" },
    div({ class: "card-header" },
      h3(proposal.merchant || "Unknown"),
      statusBadge("pending"),
    ),
    div({ class: "card-meta" },
      span({ class: "muted" }, dateStr),
      span(`${itemCount} items`),
      span({ class: "money" }, total),
    ),
    div({ class: "card-actions" },
      button({ onclick: () => navigate(`/proposals/${proposal.proposalId}`), class: "btn-primary" }, "View & Edit"),
      button({ onclick: handleDelete, disabled: deleting, class: "btn-danger" }, "Delete"),
    ),
  )
}

const HomePage = () => {
  const proposals = van.state<Proposal[]>([])
  const receipts = van.state<EnrichedReceiptSummary[]>([])
  const loadingProposals = van.state(true)
  const loadingReceipts = van.state(true)
  const errorProposals = van.state<string | null>(null)
  const errorReceipts = van.state<string | null>(null)

  const loadProposals = async () => {
    loadingProposals.val = true
    errorProposals.val = null
    try {
      const data = await api.get("/proposals")
      proposals.val = Array.isArray(data) ? data : []
    } catch (err) {
      console.error("Failed to load proposals:", err)
      errorProposals.val = (err as Error).message || "Failed to load proposals"
      proposals.val = []
    } finally {
      loadingProposals.val = false
    }
  }

  const loadReceipts = async () => {
    loadingReceipts.val = true
    errorReceipts.val = null
    try {
      // Enriched endpoint (ticket 03) — the receipt cards need
      // merchantName to render meaningfully.
      const data = await api.get("/receipts/enriched")
      receipts.val = Array.isArray(data) ? data : []
    } catch (err) {
      console.error("Failed to load receipts:", err)
      errorReceipts.val = (err as Error).message || "Failed to load receipts"
    } finally {
      loadingReceipts.val = false
    }
  }

  loadProposals()
  loadReceipts()

  const handleProposalAction = () => {
    loadProposals()
  }

  // Cap "Recent Receipts" at 10; matches the previous home-page limit.
  const recentReceipts = () => receipts.val.slice(0, 10)

  // Compute this-month and last-month totals from the already-loaded
  // enriched receipts. We don't make a separate /api/analysis call —
  // the list endpoint has all the data we need at family scale.
  //
  // Returns { total, count, avg, lastMonthTotal } so the UI can show
  // a delta vs last month. lastMonthTotal is null if there were no
  // receipts last month (no meaningful comparison to display).
  const monthStats = (): {
    total: number
    count: number
    avg: number
    lastMonthTotal: number | null
  } | null => {
    if (receipts.val.length === 0) return null
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
    for (const r of receipts.val) {
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
    const lastMonthResult = lastMonthCount > 0 ? lastMonthTotal : null
    return { total, count, avg, lastMonthTotal: lastMonthResult }
  }

  // Format a cents delta as a "+12% vs last month" or "-8% vs last
  // month" string. Returns null when there's no meaningful comparison
  // (no receipts last month).
  const deltaPct = (current: number, last: number | null): string | null => {
    if (last === null || last === 0) return null
    const pct = Math.round(((current - last) / last) * 100)
    if (pct === 0) return "same as last month"
    return `${pct > 0 ? "+" : ""}${pct}% vs last month`
  }

  return div({ class: "home-page" },
    div({ class: "page-header" },
      h1("Home"),
      button({ onclick: () => navigate("/receipts/upload"), class: "btn-primary" }, "Upload Receipt"),
    ),

    // This-month summary stats. Skipped while receipts are loading
    // (we'd just show zeros) and when there's no data at all (the
    // "No receipts yet" empty state in the Recent Receipts section
    // carries the message).
    () => {
      const stats = monthStats()
      if (!stats) return ""
      const delta = deltaPct(stats.total, stats.lastMonthTotal)
      return div({ class: "home-stats" },
        div({ class: "stat-card card" },
          p({ class: "stat-label" }, "This Month"),
          p({ class: "stat-value money" }, formatMoney(stats.total)),
          () => delta ? p({ class: "stat-delta muted" }, delta) : "",
        ),
        div({ class: "stat-card card" },
          p({ class: "stat-label" }, "Receipts"),
          p({ class: "stat-value" }, String(stats.count)),
          p({ class: "stat-delta muted" }, "this month"),
        ),
        div({ class: "stat-card card" },
          p({ class: "stat-label" }, "Average per Trip"),
          p({ class: "stat-value money" }, stats.count > 0 ? formatMoney(Math.round(stats.avg)) : "—"),
          p({ class: "stat-delta muted" }, stats.count > 0 ? `across ${stats.count} receipt${stats.count === 1 ? "" : "s"}` : ""),
        ),
      )
    },

    // Proposals section
    div({ class: "home-section" },
      div({ class: "section-header" },
        h2("Pending Proposals"),
        () => proposals.val.length > 0
          ? span({ class: "section-count" }, proposals.val.length)
          : span(),
      ),
      () => {
        if (errorProposals.val) {
          return div({ class: "empty-state" },
            h3("Couldn't load proposals"),
            p(errorProposals.val),
            button({ onclick: loadProposals }, "Try Again"),
          )
        }
        if (loadingProposals.val) {
          return div({ class: "cards-grid" },
            SkeletonProposalCard(),
            SkeletonProposalCard(),
            SkeletonProposalCard(),
          )
        }
        if (proposals.val.length === 0) {
          return div({ class: "empty-section" }, p("No pending proposals"))
        }
        return div({ class: "cards-grid" },
          ...proposals.val.map(p => ProposalCard(p, handleProposalAction)),
        )
      },
    ),

    // Receipts section
    div({ class: "home-section" },
      div({ class: "section-header" },
        h2("Recent Receipts"),
        button({ onclick: () => navigate("/receipts"), class: "btn-secondary btn-sm" }, "View All"),
      ),
      () => {
        if (errorReceipts.val) {
          return div({ class: "empty-state" },
            h3("Couldn't load receipts"),
            p(errorReceipts.val),
            button({ onclick: loadReceipts }, "Try Again"),
          )
        }
        if (loadingReceipts.val) {
          return div({ class: "cards-grid" },
            SkeletonReceiptRow(),
            SkeletonReceiptRow(),
            SkeletonReceiptRow(),
          )
        }
        if (receipts.val.length === 0) {
          return div({ class: "empty-section" }, p("No receipts yet"))
        }
        return div({ class: "cards-grid" },
          ...recentReceipts().map(r => ReceiptCard(r)),
        )
      },
    ),
  )
}

export default HomePage
