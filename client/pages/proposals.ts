import van from "vanjs-core"
import { api, navigate } from "../main"

const { div, h1, h2, table, tr, td, th, button, select, option } = van.tags

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

const ProposalForm = (proposal: Proposal, onApproved: () => void) => {
  const choices = van.state<Record<number, string>>({})

  const handleChoice = (index: number, choice: string) => {
    choices.val = { ...choices.val, [index]: choice }
  }

  const handleApprove = async () => {
    try {
      await api.post(`/proposals/${proposal.proposalId}/approve`, {
        choices: choices.val,
      })
      onApproved()
    } catch (err) {
      console.error("Failed to approve proposal:", err)
    }
  }

  return div({ class: "proposal-form card" },
    h2(`Proposal from ${proposal.merchant}`),
    table(
      tr(
        th("Item"),
        th("Qty"),
        th("Price"),
        th("Confidence"),
        th("Action"),
      ),
      ...proposal.items.map((item, index) =>
        tr(
          td(item.parsedName),
          td(item.quantity.toString()),
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
    ),
    button({ onclick: handleApprove }, "Approve Receipt"),
  )
}

const ProposalsPage = () => {
  const proposals = van.state<Proposal[]>([])
  const loading = van.state(true)

  const loadProposals = async () => {
    loading.val = true
    try {
      const data = await api.get("/proposals")
      proposals.val = data || []
    } catch (err) {
      console.error("Failed to load proposals:", err)
    }
    loading.val = false
  }

  loadProposals()

  const handleApproved = () => {
    loadProposals()
    navigate("/receipts")
  }

  return div({ class: "proposals-page" },
    h1("Pending Proposals"),
    () => loading.val
      ? div("Loading...")
      : proposals.val.length === 0
        ? div("No pending proposals")
        : proposals.val.map(p => ProposalForm(p, handleApproved)),
  )
}

export default ProposalsPage
