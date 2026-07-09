import van from "vanjs-core"
import { api, navigate } from "../main"

const { div, h1, h2, h3, table, tr, td, th, button, select, option, img, p, span, input, a } = van.tags

// Zoomable image component with pinch/scroll support
const ZoomableImage = (src: () => string, alt: string) => {
  const container = van.tags.div({ class: "zoom-container" })
  const imgEl = van.tags.img({ src: src(), alt, class: "zoom-image" })
  let scale = 1
  let panX = 0
  let panY = 0
  let lastPanX = 0
  let lastPanY = 0
  let isDragging = false
  let lastPinchDist = 0

  const apply = () => {
    imgEl.style.transform = `translate(${panX}px, ${panY}px) scale(${scale})`
    container.style.cursor = scale > 1 ? (isDragging ? "grabbing" : "grab") : "zoom-in"
  }

  container.addEventListener("wheel", (e: WheelEvent) => {
    e.preventDefault()
    const rect = container.getBoundingClientRect()
    const mouseX = e.clientX - rect.left
    const mouseY = e.clientY - rect.top
    const oldScale = scale
    const zoom = e.deltaY < 0 ? 1.15 : 0.87
    scale = Math.min(Math.max(1, scale * zoom), 6)
    if (scale > 1) {
      panX = mouseX - (mouseX - panX) * (scale / oldScale)
      panY = mouseY - (mouseY - panY) * (scale / oldScale)
    } else {
      panX = 0; panY = 0
    }
    apply()
  }, { passive: false })

  container.addEventListener("touchstart", (e: TouchEvent) => {
    if (e.touches.length === 2) {
      e.preventDefault()
      lastPinchDist = Math.hypot(e.touches[0].clientX - e.touches[1].clientX, e.touches[0].clientY - e.touches[1].clientY)
    } else if (e.touches.length === 1 && scale > 1) {
      isDragging = true
      lastPanX = e.touches[0].clientX - panX
      lastPanY = e.touches[0].clientY - panY
    }
  }, { passive: false })

  container.addEventListener("touchmove", (e: TouchEvent) => {
    if (e.touches.length === 2) {
      e.preventDefault()
      const dist = Math.hypot(e.touches[0].clientX - e.touches[1].clientX, e.touches[0].clientY - e.touches[1].clientY)
      scale = Math.min(Math.max(1, scale * (dist / lastPinchDist)), 6)
      lastPinchDist = dist
      if (scale <= 1) { panX = 0; panY = 0 }
      apply()
    } else if (e.touches.length === 1 && isDragging) {
      e.preventDefault()
      panX = e.touches[0].clientX - lastPanX
      panY = e.touches[0].clientY - lastPanY
      apply()
    }
  }, { passive: false })

  container.addEventListener("touchend", () => { isDragging = false; apply() })

  container.addEventListener("mousedown", (e: MouseEvent) => {
    if (scale > 1) {
      isDragging = true
      lastPanX = e.clientX - panX
      lastPanY = e.clientY - panY
      apply()
    }
  })
  container.addEventListener("mousemove", (e: MouseEvent) => {
    if (isDragging) {
      panX = e.clientX - lastPanX
      panY = e.clientY - lastPanY
      apply()
    }
  })
  const stopDrag = () => { isDragging = false; apply() }
  container.addEventListener("mouseup", stopDrag)
  container.addEventListener("mouseleave", stopDrag)

  container.addEventListener("dblclick", () => {
    scale = 1; panX = 0; panY = 0; apply()
  })

  van.add(container, imgEl)
  return container
}

// Fetch photo with auth header and return blob URL
const fetchPhotoUrl = async (receiptId: number): Promise<string> => {
  const token = localStorage.getItem("token")
  const response = await fetch(`/api/photos/${receiptId}`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!response.ok) throw new Error(`HTTP ${response.status}`)
  const blob = await response.blob()
  return URL.createObjectURL(blob)
}

interface ProposalItem {
  parsedName: string
  quantity: number
  unitPriceCents: number
  matchedItemId: number
  categoryId: number
  isNewCategory: boolean
  userChoice: string
  ocrConfidence?: number
  sourceBlockType?: string
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
  ocrMinConfidence?: number
}

// Mirror of the Go-side IsInProgressStatus helper. When status is any of
// these, the SSE stream is still pushing events and the page should wait.
const inProgressStatuses = new Set(["uploaded", "parsed_ocr", "parsed_llm", "parsing"])
const isInProgressStatus = (s: string) => inProgressStatuses.has(s)

// confidenceBadge renders a small colored pill next to an item name showing
// OCR confidence. Returns "" when OCR didn't run for this item (the field
// is left at zero by the parser in that case, see ConfidenceForLine).
const confidenceBadge = (item: ProposalItem) => {
  if (!item.ocrConfidence || item.ocrConfidence <= 0) return ""
  const conf = item.ocrConfidence
  const pct = Math.round(conf * 100)
  const level = conf >= 0.85 ? "high" : conf >= 0.60 ? "medium" : "low"
  const source = item.sourceBlockType ? ` (from ${item.sourceBlockType})` : ""
  const title = `OCR confidence: ${pct}%${source}`
  return span({ class: `item-confidence item-confidence-${level}`, title }, `${pct}%`)
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
  const photoSrc = van.state("")
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
              if (data.status === "failed") {
                error.val = data.error || "Parse failed"
              }
              // Treat any in-progress status as "still parsing" for the UI.
              if (!isInProgressStatus(data.status)) {
                return
              }
            } else if (eventType === "progress") {
              progressMsg.val = data.message || ""
            } else if (eventType === "ocr_done") {
              // OCR completed; show a one-line status update.
              progressMsg.val = data.message || "Read receipt"
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
      button({ onclick: () => navigate("/") }, "Back"),
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
        td({ class: "edit-actions" },
          button({ onclick: saveEdit, class: "btn-sm btn-primary" }, "Save"),
          button({ onclick: cancelEdit, class: "btn-sm btn-secondary" }, "Cancel"),
        ),
      )
    }

    // Normal display row
    return tr(
      td({ class: "item-name-cell" },
        item.parsedName,
        " ",
        confidenceBadge(item),
      ),
      td(String(item.quantity)),
      td(`$${(item.unitPriceCents / 100).toFixed(2)}`),
      td(
        button({ onclick: () => startEdit(index), class: "btn-sm btn-secondary" }, "Edit"),
      ),
    )
  }

  const loadPhoto = async (receiptId: number) => {
    try {
      photoSrc.val = await fetchPhotoUrl(receiptId)
    } catch {
      photoSrc.val = ""
    }
  }

  const renderPending = () => {
    const pr = proposal.val!
    if (pr.photoUrl && !photoSrc.val) loadPhoto(pr.proposalId)
    return div({ class: "proposal-detail-page" },
      div({ class: "page-header" },
        h1(`${pr.merchant || "Receipt"}`),
        button({ onclick: () => navigate("/") }, "Back"),
      ),
      div({ class: "proposal-layout" },
        div({ class: "proposal-photo-wrapper" },
          div({ class: "proposal-photo" },
            () => photoSrc.val
              ? ZoomableImage(() => photoSrc.val, "Receipt")
              : p("No photo available"),
          ),
          () => photoSrc.val
            ? div({ class: "photo-actions" },
                a({ href: photoSrc.val, target: "_blank", rel: "noopener" }, "Open in new tab")
              )
            : "",
        ),
        div({ class: "proposal-items" },
          h2("Items"),
          div({ class: "items-table-wrapper" },
            table(
              tr(th("Item"), th("Qty"), th("Price"), th("Action")),
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

  const renderFailed = () => {
    if (proposal.val?.photoUrl && !photoSrc.val) loadPhoto(proposal.val.proposalId)
    return div({ class: "proposal-failed" },
      div({ class: "page-header" },
        h1("Parse Failed"),
        button({ onclick: () => navigate("/") }, "Back"),
      ),
      div({ class: "failed-content" },
        p({ class: "error" }, error.val || "An error occurred while parsing the receipt"),
        proposal.val?.photoUrl && photoSrc.val
          ? div({ class: "proposal-photo-wrapper" },
              div({ class: "proposal-photo" },
                ZoomableImage(() => photoSrc.val, "Receipt"),
              ),
              div({ class: "photo-actions" },
                a({ href: photoSrc.val, target: "_blank", rel: "noopener" }, "Open in new tab"),
              ),
            )
          : "",
      div({ class: "card-actions" },
        button({ onclick: handleRetry, class: "btn-primary" }, "Retry Parsing"),
        button({ onclick: () => {
          if (confirm("Delete this proposal?")) {
            api.delete(`/proposals/${id}`).then(() => navigate("/"))
          }
        }, class: "btn-danger" }, "Delete"),
      ),
    ),
  )
}

  return div({ class: "proposal-detail-wrapper" },
    () => {
      switch (status.val) {
        case "loading":
          return div("Loading...")
        case "parsing":
        case "uploaded":
        case "parsed_ocr":
        case "parsed_llm":
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
