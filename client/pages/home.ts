import van from "vanjs-core"
import { api, navigate } from "../main"

const { div, h1, h2, button, span, p, h3 } = van.tags

interface Receipt {
  receiptId: number
  merchantId: number
  ownerId: number
  date: number
  photoUrl: string
  items: { itemId: number; quantity: number; unitPriceCents: number }[]
  totalCents: number
}

interface ProposalItem {
  parsedName: string
  quantity: number
  unitPriceCents: number
  matchedItemId: number
  confidence: number
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

const ReceiptCard = (receipt: Receipt) => {
  const date = new Date(receipt.date * 1000)
  const dateStr = date.toLocaleDateString()

  return div({ class: "receipt-card card", onclick: () => navigate(`/receipts/${receipt.receiptId}`) },
    div({ class: "receipt-header" },
      h3(`Receipt #${receipt.receiptId}`),
      span({ class: "receipt-date" }, dateStr),
    ),
    div({ class: "receipt-body" },
      p(`${receipt.items.length} items`),
      p({ class: "receipt-total" }, `$${(receipt.totalCents / 100).toFixed(2)}`),
    ),
  )
}

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
  const total = `$${(proposal.totalCents / 100).toFixed(2)}`
  const dateStr = proposal.date ? new Date(proposal.date * 1000).toLocaleDateString() : ""

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
      h3(`${proposal.merchant || "Unknown"}`),
      statusBadge("pending"),
    ),
    div({ class: "card-meta" },
      span(dateStr),
      span(`${itemCount} items`),
      span(total),
    ),
    div({ class: "card-actions" },
      button({ onclick: () => navigate(`/proposals/${proposal.proposalId}`), class: "btn-primary" }, "View & Edit"),
      button({ onclick: handleDelete, disabled: deleting, class: "btn-danger" }, "Delete"),
    ),
  )
}

const HomePage = () => {
  const proposals = van.state<Proposal[]>([])
  const receipts = van.state<Receipt[]>([])
  const loadingProposals = van.state(true)
  const loadingReceipts = van.state(true)

  const loadProposals = async () => {
    loadingProposals.val = true
    try {
      const data = await api.get("/proposals")
      proposals.val = Array.isArray(data) ? data : []
    } catch (err) {
      console.error("Failed to load proposals:", err)
      proposals.val = []
    }
    loadingProposals.val = false
  }

  const loadReceipts = async () => {
    loadingReceipts.val = true
    try {
      const data = await api.get("/receipts")
      receipts.val = data || []
    } catch (err) {
      console.error("Failed to load receipts:", err)
    }
    loadingReceipts.val = false
  }

  loadProposals()
  loadReceipts()

  const handleProposalAction = () => {
    loadProposals()
  }

  return div({ class: "home-page" },
    div({ class: "page-header" },
      h1("Home"),
      button({ onclick: () => navigate("/receipts/upload"), class: "btn-primary" }, "Upload Receipt"),
    ),

    // Proposals section
    div({ class: "home-section" },
      div({ class: "section-header" },
        h2("Pending Proposals"),
        () => proposals.val.length > 0
          ? span({ class: "section-count" }, proposals.val.length)
          : span(),
      ),
      () => loadingProposals.val
        ? div({ class: "loading" }, "Loading...")
        : proposals.val.length === 0
          ? div({ class: "empty-section" }, p("No pending proposals"))
          : div({ class: "cards-grid" },
              ...proposals.val.map(p => ProposalCard(p, handleProposalAction))
            ),
    ),

    // Receipts section
    div({ class: "home-section" },
      div({ class: "section-header" },
        h2("Recent Receipts"),
        button({ onclick: () => navigate("/receipts"), class: "btn-secondary btn-sm" }, "View All"),
      ),
      () => loadingReceipts.val
        ? div({ class: "loading" }, "Loading...")
        : receipts.val.length === 0
          ? div({ class: "empty-section" }, p("No receipts yet"))
          : div({ class: "cards-grid" },
              ...receipts.val.slice(0, 10).map(r => ReceiptCard(r))
            ),
    ),
  )
}

export default HomePage
