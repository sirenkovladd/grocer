import van from "vanjs-core"
import { api, navigate } from "../main"

const { div, h1, h2, table, tr, td, th, button, select, option, img, p } = van.tags

interface ProposalItem {
  parsedName: string
  quantity: number
  unitPrice: number
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
  total: number
  status: string
}

const ProposalDetailPage = () => {
  const proposal = van.state<Proposal | null>(null)
  const loading = van.state(true)
  const choices = van.state<Record<number, string>>({})
  const approving = van.state(false)
  const error = van.state("")

  const loadProposal = async () => {
    const id = window.location.hash.split("/").pop()
    if (!id) return

    loading.val = true
    try {
      const data = await api.get(`/proposals/${id}`)
      proposal.val = data
    } catch (err) {
      console.error("Failed to load proposal:", err)
    }
    loading.val = false
  }

  loadProposal()

  const handleChoice = (index: number, choice: string) => {
    choices.val = { ...choices.val, [index]: choice }
  }

  const handleApprove = async () => {
    if (!proposal.val) return

    approving.val = true
    error.val = ""

    try {
      await api.post(`/proposals/${proposal.val.proposalId}/approve`, {
        choices: choices.val,
      })
      navigate("/receipts")
    } catch (err) {
      error.val = err instanceof Error ? err.message : "Approval failed"
    } finally {
      approving.val = false
    }
  }

  return div({ class: "proposal-detail-page" },
    () => loading.val
      ? div("Loading...")
      : !proposal.val
        ? div("Proposal not found")
        : div(
            div({ class: "page-header" },
              h1(`Proposal from ${proposal.val.merchant}`),
              button({ onclick: () => navigate("/proposals") }, "Back"),
            ),
            div({ class: "proposal-layout" },
              div({ class: "proposal-photo" },
                proposal.val.photoUrl
                  ? img({ src: `/api/photos/${proposal.val.proposalId}`, alt: "Receipt" })
                  : p("No photo available"),
              ),
              div({ class: "proposal-items" },
                h2("Items"),
                table(
                  tr(
                    th("Item"),
                    th("Qty"),
                    th("Price"),
                    th("Confidence"),
                    th("Action"),
                  ),
                  ...proposal.val.items.map((item, index) =>
                    tr(
                      td(item.parsedName),
                      td(item.quantity.toString()),
                      td(`$${item.unitPrice.toFixed(2)}`),
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
                div({ class: "proposal-summary" },
                  p(`Total: $${proposal.val.total.toFixed(2)}`),
                  p(`Date: ${new Date(proposal.val.date * 1000).toLocaleDateString()}`),
                ),
                () => error.val ? p({ class: "error" }, error.val) : "",
                button({
                  onclick: handleApprove,
                  disabled: approving,
                  class: "approve-btn",
                }, approving.val ? "Approving..." : "Approve Receipt"),
              ),
            ),
          ),
  )
}

export default ProposalDetailPage
