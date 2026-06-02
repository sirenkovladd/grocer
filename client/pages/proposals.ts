import van from "vanjs-core"
import { api, navigate } from "../main"

const { div, h1, h2, table, tr, td, th, button, select, option, span, p } = van.tags

interface ProposalItem {
  parsedName: string
  quantity: number
  unitPriceCents: number
  matchedItemId: number
  categoryId: number
  isNewCategory: boolean
  userChoice: string
}

interface Proposal {
  proposalId: number
  ownerId: number
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

const ProposalCard = (proposal: Proposal, onAction: () => void) => {
  const deleting = van.state(false)

  const handleView = () => {
    navigate(`/proposals/${proposal.proposalId}`)
  }

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
  const total = `$${(proposal.totalCents / 100).toFixed(2)}`
  const dateStr = proposal.date ? new Date(proposal.date * 1000).toLocaleDateString() : ""

  if (proposal.status === "parsing") {
    return div({ class: "proposal-form card" },
      div({ class: "card-header" },
        h2("Parsing receipt..."),
        statusBadge("parsing"),
      ),
      div({ class: "parsing-indicator" },
        div({ class: "spinner" }),
        span(`${itemCount} items found so far`),
      ),
      div({ class: "card-actions" },
        button({ onclick: handleView }, "Watch Progress"),
        button({ onclick: handleDelete, disabled: deleting, class: "btn-danger" }, "Delete"),
      ),
    )
  }

  if (proposal.status === "failed") {
    return div({ class: "proposal-form card" },
      div({ class: "card-header" },
        h2("Parse Failed"),
        statusBadge("failed"),
      ),
      p("An error occurred while parsing this receipt"),
      div({ class: "card-actions" },
        button({ onclick: handleRetry, class: "retry-btn" }, "Retry"),
        button({ onclick: handleView, class: "btn-secondary" }, "View"),
        button({ onclick: handleDelete, disabled: deleting, class: "btn-danger" }, "Delete"),
      ),
    )
  }

  // Default: pending
  return div({ class: "proposal-form card" },
    div({ class: "card-header" },
      h2(`${proposal.merchant || "Unknown"}`),
      statusBadge("pending"),
    ),
    div({ class: "card-meta" },
      span(dateStr),
      span(`${itemCount} items`),
      span(total),
    ),
    div({ class: "card-actions" },
      button({ onclick: handleView, class: "btn-primary" }, "View & Edit"),
      button({ onclick: handleDelete, disabled: deleting, class: "btn-danger" }, "Delete"),
    ),
  )
}

const ProposalsPage = () => {
  const proposals = van.state<Proposal[]>([])
  const loading = van.state(true)

  const loadProposals = async () => {
    loading.val = true
    try {
      const data = await api.get("/proposals")
      proposals.val = Array.isArray(data) ? data : []
    } catch (err) {
      console.error("Failed to load proposals:", err)
      proposals.val = []
    }
    loading.val = false
  }

  loadProposals()

  const handleAction = () => {
    loadProposals()
  }

  const renderContent = () => {
    if (loading.val) return div("Loading...")
    if (proposals.val.length === 0) return div("No active proposals")
    return div(
      ...proposals.val.map(p => ProposalCard(p, handleAction))
    )
  }

  return div({ class: "proposals-page" },
    div({ class: "page-header" },
      h1("Proposals"),
      button({ onclick: () => navigate("/receipts/upload"), class: "btn-primary" }, "Upload New"),
    ),
    renderContent,
  )
}

export default ProposalsPage
