import van from "vanjs-core"
import { api, navigate } from "../main"

// TOML template the "Copy schema" button copies. Field names match the
// backend's userInputReceipt struct 1:1 (see internal/llm/llm_user_input.go).
const TOML_SCHEMA = `# Receipt parser output
merchant = "store name as printed on the receipt"
date = "YYYY-MM-DD"
total = 25.99

[[items]]
name = "item name as printed on the receipt"
quantity = 1
unit_price = 2.99
total_price = 2.99
`

// Self-contained prompt the "Copy prompt to LLM" button copies. The user
// pastes this into ChatGPT/Claude along with the receipt image, then
// pastes the response back into the "Apply external" textarea. Rules are
// kept aligned with the server-side buildReceiptPrompt so external and
// auto parses follow the same constraints.
const LLM_PROMPT = `You are a grocery receipt parser. I will attach a photo of a receipt.
Extract the contents in TOML format. Output ONLY the TOML — no commentary, no code fences, no explanation.

Schema:
merchant = "store name as printed on the receipt"
date = "YYYY-MM-DD"
total = 25.99

[[items]]
name = "item name as printed on the receipt"
quantity = 1
unit_price = 2.99
total_price = 2.99

Rules (must follow):

PRICE EXTRACTION (most important):
- For non-weighted items (quantity 1), copy the printed price EXACTLY as it appears on the receipt into BOTH unit_price and total_price. If the receipt says $8.45, output $8.45 (not $8.44, not $8.4, not $8.5).
- For weighted items, the printed line total (the number on the same line as the item name) goes in total_price. Copy it exactly. The per-kg/lb number from the next line goes in unit_price. Example: "BANANAS 1.72" then "0.875 kg @ $1.96/kg" → quantity 0.875, unit_price 1.96, total_price 1.72.

ATTACHED LINES (consume into the preceding item, do NOT output as separate items):
- "Card $X.XX Save -Y" / "Save -$Y" / "Coupon -$Y" → discount on preceding item. Reduce that item's total_price by Y.
- "*DEPOSIT", "*RECYCLE FEE", "*ENV FEE", "*BOTTLE DEPOSIT" → price adder on preceding item. ADD to total_price.
- "0.875 kg @ $1.96/kg" or "$1.96/lb" → unit-price info for preceding item, NOT a separate item.

EXCLUDE entirely (do not emit):
- "Sub Total", "Subtotal", "Tax", "GST", "PST", "HST", "Total", "Balance Due", "Credit", "Cash", "Change", "Payment", "VISA", "MASTERCARD", "DEBIT".
- Card numbers (e.g. "XXXXX6431"), transaction IDs, "TYPE: Purchase", "ACCT:", "REF#", "AUTHOR#", "AID:", "APPROVED", "NO SIGNATURE".
- Loyalty / rewards, store numbers, addresses, phone numbers.

GENERAL:
- quantity can be a decimal for weighted items (e.g. 0.875 for 875g).
- If unsure about a line, skip it rather than guess.
- Return ONLY the TOML.
`

// copyToClipboard copies text and resolves with true on success. Falls back
// to a synchronous textarea-select trick if the async Clipboard API isn't
// available (insecure context, old browsers).
const copyToClipboard = async (text: string): Promise<boolean> => {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      // fall through to legacy path
    }
  }
  try {
    const ta = document.createElement("textarea")
    ta.value = text
    ta.style.position = "fixed"
    ta.style.opacity = "0"
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand("copy")
    document.body.removeChild(ta)
    return ok
  } catch {
    return false
  }
}

const { div, h1, h2, h3, table, tr, td, th, button, select, option, img, p, span, input, a, pre, textarea } = van.tags

// Zoomable image component with pinch/scroll support
import { fetchPhotoUrl } from "../photos"
import { ZoomableImage } from "../components/zoomable-image"

interface ProposalItem {
  parsedName: string
  quantity: number
  unitPriceCents: number
  totalPriceCents?: number
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
  ocrMarkdown?: string
}

// Mirror of the Go-side IsInProgressStatus helper. When status is any of
// these, the SSE stream is still pushing events and the page should wait.
const inProgressStatuses = new Set(["uploaded", "parsed_ocr", "parsed_llm", "parsing"])
const isInProgressStatus = (s: string) => inProgressStatuses.has(s)

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
  const showOcrDetails = van.state(false)
  const tomlInput = van.state("")
  const applyingExternal = van.state(false)
  const applyError = van.state("")
  const copyConfirm = van.state("")
  // recentDelete holds the most recently deleted item so the user can
  // undo within a 5s window. The delete is committed to the backend
  // immediately; undo re-POSTs the item to put it back. We don't try to
  // track multiple pending deletes — the snackbar shows the most recent
  // and Undo restores only that one.
  const recentDelete = van.state<{ item: ProposalItem; index: number } | null>(null)
  const addingItem = van.state(false)
  let abortController: AbortController | null = null
  let copyConfirmTimer: ReturnType<typeof setTimeout> | null = null
  let deleteSnackbarTimer: ReturnType<typeof setTimeout> | null = null

  const id = window.location.hash.split("/").pop()

  const fetchSSE = async () => {
    if (!id) return

    abortController?.abort()
    abortController = new AbortController()

    try {
      const response = await fetch(`/api/proposals/${id}/stream`, {
        credentials: "same-origin",
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
    // The user edits the total price (what they actually paid), not the
    // per-unit price. The server recomputes unitPriceCents from total / qty.
    const totalCents = item.totalPriceCents && item.totalPriceCents > 0
      ? item.totalPriceCents
      : item.unitPriceCents
    editPrice.val = (totalCents / 100).toFixed(2)
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
        totalPriceCents: Math.round(parseFloat(editPrice.val) * 100) || 0,
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

  // handleReparse kicks off a reparse with one of three engines:
  //   - "full": OCR (if configured) + LLM from text. Default.
  //   - "llm_text": skip OCR, reuse existing OcrMarkdown, call LLM.
  //   - "llm_image": skip OCR, send photo to LLM directly.
  const handleReparse = async (engine: "full" | "llm_text" | "llm_image") => {
    if (!id) return
    error.val = ""
    status.val = "loading"
    streamingItems.val = []
    progressMsg.val = ""

    try {
      await api.post(`/proposals/${id}/reparse`, { engine })
      status.val = "parsing"
      fetchSSE()
    } catch (err) {
      error.val = err instanceof Error ? err.message : "Reparse failed"
      status.val = "failed"
    }
  }

  // handleAddItem creates a new empty ProposalItem on the backend, appends
  // it to the local array, and immediately opens the inline editor for
  // it so the user can fill in the values without a second click.
  const handleAddItem = async () => {
    if (!id) return
    addingItem.val = true
    error.val = ""
    try {
      const newItem = await api.post(`/proposals/${id}/items`, {})
      streamingItems.val = [...streamingItems.val, newItem]
      const newIndex = streamingItems.val.length - 1
      startEdit(newIndex)
    } catch (err) {
      error.val = err instanceof Error ? err.message : "Add failed"
    } finally {
      addingItem.val = false
    }
  }

  // handleDeleteItem removes an item from the backend and local state
  // immediately, then shows a 5s undo snackbar. Undo re-POSTs the item
  // with its captured data; if the user doesn't undo, the item is gone
  // for good after 5s.
  const handleDeleteItem = async (index: number) => {
    if (!id) return
    const item = streamingItems.val[index]
    if (!item) return
    try {
      await api.delete(`/proposals/${id}/items/${index}`)
      const items = [...streamingItems.val]
      items.splice(index, 1)
      streamingItems.val = items
      // If we were editing this item, exit edit mode.
      if (editingIndex.val === index) editingIndex.val = -1
      // Show the snackbar. Cancel any previous snackbar timer so the new
      // delete gets a full 5s window.
      if (deleteSnackbarTimer) clearTimeout(deleteSnackbarTimer)
      recentDelete.val = { item, index }
      deleteSnackbarTimer = setTimeout(() => {
        recentDelete.val = null
        deleteSnackbarTimer = null
      }, 5000)
    } catch (err) {
      error.val = err instanceof Error ? err.message : "Delete failed"
    }
  }

  const handleUndoDelete = async () => {
    if (!id || !recentDelete.val) return
    const { item, index } = recentDelete.val
    if (deleteSnackbarTimer) clearTimeout(deleteSnackbarTimer)
    deleteSnackbarTimer = null
    try {
      const restored = await api.post(`/proposals/${id}/items`, {
        parsedName: item.parsedName,
        quantity: item.quantity,
        unitPriceCents: item.unitPriceCents,
        totalPriceCents: item.totalPriceCents,
      })
      // Insert at the original index if still in range, else append.
      const items = [...streamingItems.val]
      const insertAt = Math.min(index, items.length)
      items.splice(insertAt, 0, restored)
      streamingItems.val = items
      recentDelete.val = null
    } catch (err) {
      error.val = err instanceof Error ? err.message : "Undo failed"
    }
  }

  const handleApplyExternal = async () => {
    if (!id) return
    const content = tomlInput.val.trim()
    if (!content) {
      applyError.val = "Paste TOML or JSON first"
      return
    }
    applyingExternal.val = true
    applyError.val = ""
    try {
      const updated = await api.post(`/proposals/${id}/apply-external`, { content })
      proposal.val = updated
      streamingItems.val = updated.items || []
      status.val = "pending"
      tomlInput.val = ""
    } catch (err) {
      applyError.val = err instanceof Error ? err.message : "Apply failed"
    } finally {
      applyingExternal.val = false
    }
  }

  // flashCopyConfirm shows a temporary "Copied" message next to a copy
  // button. Auto-clears after 2s; rapid re-clicks reset the timer.
  const flashCopyConfirm = (label: string) => {
    copyConfirm.val = label
    if (copyConfirmTimer) clearTimeout(copyConfirmTimer)
    copyConfirmTimer = setTimeout(() => { copyConfirm.val = "" }, 2000)
  }

  const handleCopy = async (text: string, label: string) => {
    const ok = await copyToClipboard(text)
    if (ok) flashCopyConfirm(`Copied ${label}`)
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
    // For weighted items, unit_price (e.g. per-kg) differs from total_price
    // (the line total as printed on the receipt). Show the total as the main
    // price and the unit price as a small subtitle.
    const totalCents = item.totalPriceCents && item.totalPriceCents > 0
      ? item.totalPriceCents
      : item.unitPriceCents
    const isWeighted = item.unitPriceCents !== totalCents && item.quantity !== 1
    return tr(
      td({ class: "item-name-cell" },
        item.parsedName,
        () => isWeighted
          ? div({ class: "item-unit-price" }, `@ $${(item.unitPriceCents / 100).toFixed(2)}/unit`)
          : "",
      ),
      td(String(item.quantity)),
      td(`$${(totalCents / 100).toFixed(2)}`),
      td({ class: "row-actions" },
        button({ onclick: () => startEdit(index), class: "btn-sm btn-secondary" }, "Edit"),
        button({
          onclick: () => handleDeleteItem(index),
          class: "btn-sm btn-danger",
          title: "Delete item",
        }, "×"),
      ),
    )
  }

  const loadPhoto = async (receiptId: number) => {
    try {
      // Use the 1200px 'large' variant — the original can be 5MB+
      // and the user can still pinch/scroll-zoom the rendered
      // image to inspect detail.
      photoSrc.val = await fetchPhotoUrl(receiptId, "large")
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
          button({
            class: "btn-secondary add-item-btn",
            disabled: addingItem.val,
            onclick: handleAddItem,
          }, () => addingItem.val ? "Adding…" : "+ Add item"),
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
        button({ onclick: () => handleReparse("full"), class: "btn-primary" }, "Retry Parsing"),
        button({ onclick: () => {
          if (confirm("Delete this proposal?")) {
            api.delete(`/proposals/${id}`).then(() => navigate("/"))
          }
        }, class: "btn-danger" }, "Delete"),
      ),
    ),
  )
}

  // renderDeleteSnackbar shows a fixed-position snackbar at the bottom
  // of the screen when an item has just been deleted. The user has 5
  // seconds to click Undo. The delete is already committed to the
  // backend; undo re-POSTs the item.
  const renderDeleteSnackbar = () => {
    if (!recentDelete.val) return ""
    const itemName = recentDelete.val.item.parsedName || "item"
    return div({ class: "delete-snackbar" },
      span(`Deleted "${itemName}"`),
      button({
        class: "delete-snackbar-undo",
        onclick: handleUndoDelete,
      }, "Undo"),
    )
  }

  // renderToolsPanel is the always-visible "Reparse & Override" section.
  // It re-renders as a function-child so state reads bind to this context
  // (not to the App router binding — see AGENTS.md "VanJS Gotchas").
  const renderToolsPanel = () => {
    const reparseDisabled = status.val === "loading" || status.val === "parsing"
    const ocr = proposal.val?.ocrMarkdown || ""
    return div({ class: "tools-panel" },
      h2("Reparse & Override"),

      div({ class: "tools-section" },
        h3("Reparse"),
        div({ class: "reparse-buttons" },
          button({
            class: "btn-secondary",
            disabled: reparseDisabled,
            onclick: () => handleReparse("full"),
          }, "Full (OCR + LLM)"),
          button({
            class: "btn-secondary",
            disabled: reparseDisabled || !ocr,
            title: !ocr ? "No OCR result to reuse; use Full or LLM (image)" : "",
            onclick: () => handleReparse("llm_text"),
          }, "LLM (existing OCR)"),
          button({
            class: "btn-secondary",
            disabled: reparseDisabled,
            onclick: () => handleReparse("llm_image"),
          }, "LLM (image)"),
        ),
      ),

      div({ class: "tools-section" },
        h3("OCR details"),
        button({
          class: "btn-secondary btn-sm",
          onclick: () => { showOcrDetails.val = !showOcrDetails.val },
        }, () => showOcrDetails.val ? "Hide OCR details" : "Show OCR details"),
        () => showOcrDetails.val
          ? div({ class: "ocr-details" },
              ocr
                ? pre({ class: "ocr-markdown" }, ocr)
                : p({ class: "tools-empty" }, "No OCR result yet."),
              ocr
                ? div({ class: "copy-buttons" },
                    button({
                      class: "btn-secondary btn-sm",
                      onclick: () => handleCopy(ocr, "OCR"),
                    }, "Copy OCR"),
                  )
                : "",
              () => copyConfirm.val === "Copied OCR"
                ? span({ class: "copy-confirm" }, "Copied")
                : "",
            )
          : "",
      ),

      div({ class: "tools-section" },
        h3("External LLM"),
        p({ class: "tools-hint" }, "Send the receipt image to ChatGPT/Claude with the prompt below, then paste the TOML response here."),
        div({ class: "copy-buttons" },
          button({
            class: "btn-secondary btn-sm",
            onclick: () => handleCopy(TOML_SCHEMA, "schema"),
          }, "Copy schema"),
          button({
            class: "btn-secondary btn-sm",
            onclick: () => handleCopy(LLM_PROMPT, "prompt"),
          }, "Copy prompt to LLM"),
          () => copyConfirm.val.startsWith("Copied ")
            ? span({ class: "copy-confirm" }, "✓")
            : "",
        ),
        textarea({
          class: "external-llm-input",
          placeholder: "Paste TOML or JSON here…",
          value: tomlInput.val,
          oninput: (e: Event) => { tomlInput.val = (e.target as HTMLTextAreaElement).value },
        }),
        () => applyError.val
          ? p({ class: "error" }, applyError.val)
          : "",
        button({
          class: "btn-primary",
          disabled: applyingExternal.val,
          onclick: handleApplyExternal,
        }, () => applyingExternal.val ? "Applying…" : "Apply response"),
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
    renderToolsPanel,
    renderDeleteSnackbar,
  )
}

export default ProposalDetailPage
