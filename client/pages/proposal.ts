import van from "vanjs-core"
import { api, navigate } from "../main"

const { div, h1, h2, table, tr, td, th, button, select, option, img, p, span } = van.tags

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
        throw new Error("Failed to connect")
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
                  td(it.quantity.toString()),
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

  const renderPending = () => {
    const pr = proposal.val!
    return div({ class: "proposal-detail-page" },
      div({ class: "page-header" },
        h1(`Proposal from ${pr.merchant}`),
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
          table(
            tr(th("Item"), th("Qty"), th("Price"), th("Confidence"), th("Action")),
            ...streamingItems.val.map((item, index) =>
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
          div({ class: "proposal-summary" },
            p(`Total: $${(pr.totalCents / 100).toFixed(2)}`),
            p(`Date: ${new Date(pr.date * 1000).toLocaleDateString()}`),
          ),
          () => error.val ? p({ class: "error" }, error.val) : "",
          button({
            onclick: handleApprove,
            disabled: approving,
            class: "approve-btn",
          }, approving.val ? "Approving..." : "Approve Receipt"),
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
      button({
        onclick: handleRetry,
        class: "retry-btn",
      }, "Retry Parsing"),
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
