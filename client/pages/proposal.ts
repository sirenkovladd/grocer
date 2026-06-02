import van from "vanjs-core"
import { api, navigate } from "../main"

const { div, h1, h2, h3, table, tr, td, th, button, select, option, img, p, span, input } = van.tags

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

const ProposalDetailPage = () => {
  const proposal = van.state<Proposal | null>(null)
  const status = van.state<string>("loading")
  const streamingItems = van.state<ProposalItem[]>([])
  const progressMsg = van.state("")
  const choices = van.state<Record<number, string>>({})
  const approving = van.state(false)
  const error = van.state("")
  const editingIndex = van.state<number>(-1)
  const editName = van.state("")
  const editQty = van.state("")
  const editPrice = van.state("")
  let abortController: AbortController | null = null

  const id = window.location.hash.split("/").pop()

  const fetchSSE = async () => {
    if (!id) return

    abortController?.abort()
    abortController = new AbortController()

    const token = localStorage.getItem("token")
    try {
      const response = await fetch(`/api/proposals/${id}/stream`, {
        headers: { "Authorization": `Bearer ${token}` },
        signal: abortController.signal,
      })

      if (!response.ok) {
        const body = await response.text()
        let msg = `HTTP ${response.status}`
        try {
          const parsed = JSON.parse(body)
          if (parsed.error) msg = parsed.error
        } catch {}
        throw new Error(msg)
      }

      const reader = response.body!.getReader()
      const decoder = new TextDecoder()
      let buffer = ""

      while (true) {
        const { done, value } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })
        const parts = buffer.split("\n\n")
        buffer = parts.pop()!

        for (const part of parts) {
          let eventType = ""
          let dataStr = ""
          for (const line of part.split("\n")) {
            if (line.startsWith("event: ")) {
              eventType = line.slice(7)
            } else if (line.startsWith("data: ")) {
              dataStr = line.slice(6)
            }
          }
          if (!eventType || !dataStr) continue

          try {
            const data = JSON.parse(dataStr)

            if (eventType === "snapshot") {
              proposal.val = data
              status.val = data.status
              if (data.items) {
                streamingItems.val = data.items
              }
              if (data.status !== "parsing") {
                return
              }
            } else if (eventType === "progress") {
              progressMsg.val = data.message || ""
            } else if (eventType === "item") {
              if (data.item) {
                streamingItems.val = [...streamingItems.val, data.item]
              }
            } else if (eventType === "done") {
              proposal.val = data.proposal
              status.val = "pending"
              streamingItems.val = data.proposal?.items || streamingItems.val
              return
            } else if (eventType === "error") {
              status.val = "failed"
              error.val = data.message || "Parse failed"
              return
            }
          } catch (parseErr) {
            console.warn("SSE parse error:", parseErr)
          }
        }
      }
    } catch (err: any) {
      if (err.name === "AbortError") return
      error.val = err instanceof Error ? err.message : "Connection failed"
      status.val = "failed"
    }
  }

  fetchSSE()

  const handleChoice = (index: number, choice: string) => {
    choices.val = { ...choices.val, [index]: choice }
  }

  const startEdit = (index: number) => {
    const item = streamingItems.val[index]
    if (!item) return
    editingIndex.val = index
    editName.val = item.parsedName
    editQty.val = String(item.quantity)
    editPrice.val = (item.unitPriceCents / 100).toFixed(2)
  }

  const cancelEdit = () => {
    editingIndex.val = -1
  }

  const saveEdit = async () => {
    const index = editingIndex.val
    if (index < 0 || !id) return

    try {
      const updated = await api.patch(`/proposals/${id}/items/${index}`, {
        parsedName: editName.val,
        quantity: parseInt(editQty.val) || 1,
        unitPriceCents: Math.round(parseFloat(editPrice.val) * 100) || 0,
      })

      // Update local state
      const items = [...streamingItems.val]
      items[index] = updated
      streamingItems.val = items
      editingIndex.val = -1
    } catch (err) {
      error.val = err instanceof Error ? err.message : "Failed to save"
    }
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

  const handleRetry = async () => {
    if (!id) return
    error.val = ""
    status.val = "loading"
    streamingItems.val = []
    progressMsg.val = ""

    try {
      await api.post(`/proposals/${id}/reparse`, {})
      status.val = "parsing"
      fetchSSE()
    } catch (err) {
      error.val = err instanceof Error ? err.message : "Retry failed"
      status.val = "failed"
    }
  }

  const renderParsing = () => div({ class: "proposal-parsing" },
    div({ class: "page-header" },
      h1("Parsing Receipt..."),
      button({ onclick: () => navigate("/proposals") }, "Back"),
    ),
    div({ class: "parsing-progress" },
      div({ class: "skeleton-header" },
        div({ class: "skeleton-line skeleton-merchant" }),
        div({ class: "skeleton-line skeleton-date" }),
        div({ class: "skeleton-line skeleton-total" }),
      ),
      () => progressMsg.val ? p({ class: "progress-msg" }, progressMsg.val) : "",
      () => streamingItems.val.length > 0
        ? div({ class: "streaming-items" },
            h2(`Items (${streamingItems.val.length})`),
            table(
              tr(th("Item"), th("Qty"), th("Price")),
              ...streamingItems.val.map((it) =>
                tr(
                  td(it.parsedName),
                  td(String(it.quantity)),
                  td(`$${(it.unitPriceCents / 100).toFixed(2)}`),
                )
              ),
            ),
          )
        : div({ class: "parsing-placeholder" },
            div({ class: "spinner" }),
            p("Waiting for items..."),
          ),
    ),
  )

  const renderItemRow = (item: ProposalItem, index: number) => {
    // If this row is being edited
    if (editingIndex.val === index) {
      return tr({ class: "editing-row" },
        td(input({
          type: "text",
          value: editName.val,
          oninput: (e: Event) => { editName.val = (e.target as HTMLInputElement).value },
          class: "edit-input",
        })),
        td(input({
          type: "number",
          value: editQty.val,
          oninput: (e: Event) => { editQty.val = (e.target as HTMLInputElement).value },
          class: "edit-input edit-qty",
          min: "1",
        })),
        td(input({
          type: "number",
          value: editPrice.val,
          oninput: (e: Event) => { editPrice.val = (e.target as HTMLInputElement).value },
          class: "edit-input edit-price",
          min: "0",
          step: "0.01",
        })),
        td(`${(item.confidence * 100).toFixed(0)}%`),
        td({ class: "edit-actions" },
          button({ onclick: saveEdit, class: "btn-sm btn-primary" }, "Save"),
          button({ onclick: cancelEdit, class: "btn-sm btn-secondary" }, "Cancel"),
        ),
      )
    }

    // Normal display row
    return tr(
      td(item.parsedName),
      td(String(item.quantity)),
      td(`$${(item.unitPriceCents / 100).toFixed(2)}`),
      td(`${(item.confidence * 100).toFixed(0)}%`),
      td(
        button({ onclick: () => startEdit(index), class: "btn-sm btn-secondary" }, "Edit"),
        item.confidence >= 0.99
          ? span({ class: "match-badge" }, "Auto")
          : item.confidence > 0.80
            ? select(
                { onchange: (e: Event) => handleChoice(index, (e.target as HTMLSelectElement).value) },
                option({ value: "" }, "Choose..."),
                option({ value: "existing" }, "Use existing"),
                option({ value: "new" }, "Create new"),
              )
            : ""
      ),
    )
  }

  const renderPending = () => {
    const pr = proposal.val!
    return div({ class: "proposal-detail-page" },
      div({ class: "page-header" },
        h1(`${pr.merchant || "Receipt"}`),
        button({ onclick: () => navigate("/proposals") }, "Back"),
      ),
      div({ class: "proposal-layout" },
        div({ class: "proposal-photo" },
          pr.photoUrl
            ? img({ src: `/api/photos/${pr.proposalId}`, alt: "Receipt" })
            : p("No photo available"),
        ),
        div({ class: "proposal-items" },
          h2("Items"),
          div({ class: "items-table-wrapper" },
            table(
              tr(th("Item"), th("Qty"), th("Price"), th("Confidence"), th("Action")),
              ...streamingItems.val.map((item, index) => renderItemRow(item, index)),
            ),
          ),
          div({ class: "proposal-summary" },
            p(`Total: $${(pr.totalCents / 100).toFixed(2)}`),
            p(`Date: ${pr.date ? new Date(pr.date * 1000).toLocaleDateString() : "Unknown"}`),
          ),
          () => error.val ? p({ class: "error" }, error.val) : "",
          button({
            onclick: handleApprove,
            disabled: approving,
            class: "approve-btn btn-primary",
          }, () => approving.val ? "Approving..." : "Approve Receipt"),
        ),
      ),
    )
  }

  const renderFailed = () => div({ class: "proposal-failed" },
    div({ class: "page-header" },
      h1("Parse Failed"),
      button({ onclick: () => navigate("/proposals") }, "Back"),
    ),
    div({ class: "failed-content" },
      p({ class: "error" }, error.val || "An error occurred while parsing the receipt"),
      proposal.val?.photoUrl
        ? div({ class: "proposal-photo" },
            img({ src: `/api/photos/${proposal.val.proposalId}`, alt: "Receipt" }),
          )
        : "",
      div({ class: "card-actions" },
        button({ onclick: handleRetry, class: "btn-primary" }, "Retry Parsing"),
        button({ onclick: () => {
          if (confirm("Delete this proposal?")) {
            api.delete(`/proposals/${id}`).then(() => navigate("/proposals"))
          }
        }, class: "btn-danger" }, "Delete"),
      ),
    ),
  )

  return div({ class: "proposal-detail-wrapper" },
    () => {
      switch (status.val) {
        case "loading":
          return div("Loading...")
        case "parsing":
          return renderParsing()
        case "pending":
          return renderPending()
        case "failed":
          return renderFailed()
        case "approved":
          return div(
            p("This proposal has been approved."),
            button({ onclick: () => navigate("/receipts") }, "View Receipts"),
          )
        default:
          return div("Unknown state")
      }
    },
  )
}

export default ProposalDetailPage
