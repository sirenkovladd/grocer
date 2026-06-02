import van from "vanjs-core"
import { api, navigate } from "../main"

const { div, h1, h2, table, tr, td, th, button, select, option, span, p } = van.tags

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

const ProposalCard = (proposal: Proposal, onAction: () => void) => {
  const choices = van.state<Record<number, string>>({})
  const approving = van.state(false)

  const handleChoice = (index: number, choice: string) => {
    choices.val = { ...choices.val, [index]: choice }
  }

  const handleApprove = async () => {
    approving.val = true
    try {
      await api.post(`/proposals/${proposal.proposalId}/approve`, {
        choices: choices.val,
      })
      onAction()
    } catch (err) {
      console.error("Failed to approve proposal:", err)
    } finally {
      approving.val = false
    }
  }

  const handleRetry = async () => {
    try {
      await api.post(`/proposals/${proposal.proposalId}/reparse`, {})
      onAction()
    } catch (err) {
      console.error("Failed to retry proposal:", err)
    }
  }

  const handleWatch = () => {
    navigate(`/proposals/${proposal.proposalId}`)
  }

  const items = proposal.items || []

  if (proposal.status === "parsing") {
    return div({ class: "proposal-form card" },
      div({ class: "card-header" },
        h2("Parsing receipt..."),
        statusBadge("parsing"),
      ),
      div({ class: "parsing-indicator" },
        div({ class: "spinner" }),
        span(`${items.length} items found so far`),
      ),
      button({ onclick: handleWatch }, "Watch Progress"),
    )
  }

  if (proposal.status === "failed") {
    return div({ class: "proposal-form card" },
      div({ class: "card-header" },
        h2("Parse Failed"),
        statusBadge("failed"),
      ),
      p("An error occurred while parsing this receipt"),
      button({ onclick: handleRetry, class: "retry-btn" }, "Retry"),
    )
  }

  // Default: pending
  return div({ class: "proposal-form card" },
    div({ class: "card-header" },
      h2(`Proposal from ${proposal.merchant || "Unknown"}`),
      statusBadge("pending"),
    ),
    items.length > 0 ? table(
      tr(th("Item"), th("Qty"), th("Price"), th("Confidence"), th("Action")),
      ...items.map((item, index) =>
        tr(
          td(item.parsedName),
          td(String(item.quantity)),
          td(`$${(item.unitPriceCents / 100).toFixed(2)}`),
          td(`${(item.confidence * 100).toFixed(0)}%`),
          td(
            item.confidence >= 0.99
              ? "Auto-matched"
              : item.confidence > 0.80
                ? select(
                    { onchange: (e: Event) => handleChoice(index, (e.target as HTMLSelectElement).value) },
                    option({ value: "" }, "Choose..."),
                    option({ value: "existing" }, "Use existing"),
                    option({ value: "new" }, "Create new"),
                  )
                : "New item"
          ),
        )
      ),
    ) : p("No items"),
    div({ class: "proposal-summary" },
      p(`Total: $${(proposal.totalCents / 100).toFixed(2)}`),
      p(`Date: ${new Date(proposal.date * 1000).toLocaleDateString()}`),
    ),
    button({
      onclick: handleApprove,
      disabled: approving,
      class: "approve-btn",
    }, () => approving.val ? "Approving..." : "Approve Receipt"),
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
    h1("Proposals"),
    renderContent,
  )
}

export default ProposalsPage
