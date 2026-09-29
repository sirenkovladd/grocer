import { createEffect, createSignal, onCleanup, For, Show, Switch, Match } from "solid-js"
import { useParams } from "@solidjs/router"
import ZoomableImageView from "../components/zoomable-image.tsx"
import { api, navigate } from "../lib/api.ts"
import { setPageTitle } from "../utils.ts"
import { fetchPhotoUrl } from "../photos.ts"

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

// renderItemName returns the proposal item's parsed name as a clickable
// link to the matched catalog item when one exists (auto-match or
// suggestion — both benefit from verification before approving).
// Opens in a new tab so the proposal stays in context. Plain text
// when unmatched. The "↗" hint marks matched rows at a glance.
function renderItemName(item: ProposalItem) {
  if (!item.matchedItemId) {
    return item.parsedName
  }
  return (
    <span class="item-name-with-match">
      <a
        href={`#/items/${item.matchedItemId}`}
        target="_blank"
        rel="noopener"
        class="item-name-link"
        title={`Opens existing item #${item.matchedItemId} in a new tab`}
      >
        {item.parsedName}
      </a>
      <span class="matched-hint" title="Matches existing catalog item">
        {" "}
        ↗
      </span>
    </span>
  )
}

interface ProposalItem {
  parsedName: string
  quantity: number
  unitPriceCents: number
  totalPriceCents?: number
  // The server's `,string` JSON tag sends IDs as strings. Empty string
  // (or absent) means no match — the auto-matcher either didn't find
  // one or didn't pass the OCR-confidence gate.
  matchedItemId?: string
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

export default function ProposalDetailPage() {
  const params = useParams()
  const [proposal, setProposal] = createSignal<Proposal | null>(null)
  const [status, setStatus] = createSignal("loading")
  const [streamingItems, setStreamingItems] = createSignal<ProposalItem[]>([])
  const [progressMsg, setProgressMsg] = createSignal("")
  const [choices, setChoices] = createSignal<Record<number, string>>({})
  const [approving, setApproving] = createSignal(false)
  const [error, setError] = createSignal("")
  const [editingIndex, setEditingIndex] = createSignal(-1)
  const [editName, setEditName] = createSignal("")
  const [editQty, setEditQty] = createSignal("")
  const [editPrice, setEditPrice] = createSignal("")
  const [photoSrc, setPhotoSrc] = createSignal("")
  const [showOcrDetails, setShowOcrDetails] = createSignal(false)
  const [tomlInput, setTomlInput] = createSignal("")
  const [applyingExternal, setApplyingExternal] = createSignal(false)
  const [applyError, setApplyError] = createSignal("")
  const [copyConfirm, setCopyConfirm] = createSignal("")
  // recentDelete holds the most recently deleted item for 5s undo.
  // The delete commits to the backend immediately; undo re-POSTs.
  const [recentDelete, setRecentDelete] = createSignal<{ item: ProposalItem; index: number } | null>(
    null,
  )
  const [addingItem, setAddingItem] = createSignal(false)
  let copyConfirmTimer: ReturnType<typeof setTimeout> | null = null
  let deleteSnackbarTimer: ReturnType<typeof setTimeout> | null = null
  onCleanup(() => {
    if (copyConfirmTimer) clearTimeout(copyConfirmTimer)
    if (deleteSnackbarTimer) clearTimeout(deleteSnackbarTimer)
  })

  const resetStream = () => {
    setProposal(null)
    setStatus("loading")
    setStreamingItems([])
    setProgressMsg("")
    setError("")
    setEditingIndex(-1)
    setPhotoSrc("")
  }

  const fetchSSE = async (id: string | undefined, signal: AbortSignal) => {
    if (!id) return
    try {
      const response = await fetch(`/api/proposals/${id}/stream`, {
        credentials: "same-origin",
        signal,
      })

      if (!response.ok) {
        const body = await response.text()
        let msg = `HTTP ${response.status}`
        try {
          const parsed = JSON.parse(body)
          if (parsed.error) msg = parsed.error
        } catch {
          // keep HTTP fallback
        }
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
              setProposal(data)
              setStatus(data.status)
              if (data.items) {
                setStreamingItems(data.items)
              }
              if (data.status === "failed") {
                setError(data.error || "Parse failed")
              }
              // Any in-progress status means "still parsing".
              if (!isInProgressStatus(data.status)) {
                return
              }
            } else if (eventType === "progress") {
              setProgressMsg(data.message || "")
            } else if (eventType === "ocr_done") {
              // OCR completed; one-line status update.
              setProgressMsg(data.message || "Read receipt")
            } else if (eventType === "item") {
              if (data.item) {
                setStreamingItems((prev) => [...prev, data.item])
              }
            } else if (eventType === "done") {
              setProposal(data.proposal)
              setStatus("pending")
              setStreamingItems(data.proposal?.items || streamingItems())
              return
            } else if (eventType === "error") {
              setStatus("failed")
              setError(data.message || "Parse failed")
              return
            }
          } catch (parseErr) {
            console.warn("SSE parse error:", parseErr)
          }
        }
      }
    } catch (err: any) {
      if (err.name === "AbortError") return
      setError(err instanceof Error ? err.message : "Connection failed")
      setStatus("failed")
    }
  }

  // (Re)start the stream on mount and whenever the route id changes
  // (component is reused across /proposals/:id). Abort torn down on
  // cleanup. Signal writes happen in async continuations (unowned),
  // never synchronously in the tracking scope.
  createEffect(() => {
    const id = params.id
    resetStream()
    const controller = new AbortController()
    onCleanup(() => controller.abort())
    void fetchSSE(id, controller.signal)
  })

  // Tab title tracks proposal state as the SSE stream pushes updates.
  createEffect(() => {
    const pr = proposal()
    const s = status()
    if (s === "pending" && pr) {
      setPageTitle(pr.merchant || "Proposal")
    } else if (s === "failed") {
      setPageTitle("Parse failed")
    } else if (s === "approved") {
      setPageTitle("Proposal approved")
    } else if (s === "loading") {
      setPageTitle("Loading proposal…")
    } else {
      setPageTitle("Parsing receipt…")
    }
  })

  // Load the zoomable photo once the proposal carries a URL.
  // 1200px 'large' variant — the original can be 5MB+.
  createEffect(() => {
    const pr = proposal()
    const s = status()
    if (!pr?.photoUrl || photoSrc()) return
    if (s !== "pending" && s !== "failed") return
    let cancelled = false
    fetchPhotoUrl(pr.proposalId, "large")
      .then((url) => {
        if (!cancelled) setPhotoSrc(url)
      })
      .catch(() => {
        if (!cancelled) setPhotoSrc("")
      })
    onCleanup(() => {
      cancelled = true
    })
  })

  const handleChoice = (index: number, choice: string) => {
    setChoices((prev) => ({ ...prev, [index]: choice }))
  }

  const startEdit = (index: number) => {
    const item = streamingItems()[index]
    if (!item) return
    setEditingIndex(index)
    setEditName(item.parsedName)
    setEditQty(String(item.quantity))
    // The user edits the total price (what they actually paid), not the
    // per-unit price. The server recomputes unitPriceCents from total / qty.
    const totalCents =
      item.totalPriceCents && item.totalPriceCents > 0 ? item.totalPriceCents : item.unitPriceCents
    setEditPrice((totalCents / 100).toFixed(2))
  }

  const cancelEdit = () => {
    setEditingIndex(-1)
  }

  const saveEdit = async () => {
    const index = editingIndex()
    const id = params.id
    if (index < 0 || !id) return

    try {
      // quantity is a float on the wire — weighted items (e.g. 0.92 kg
      // of bananas) need the decimal preserved. parseInt would silently
      // turn 0.92 into 0, and the `|| 1` guard would then send 1.
      const parsedQty = parseFloat(editQty())
      const updated = await api.patch(`/proposals/${id}/items/${index}`, {
        parsedName: editName(),
        quantity: !isNaN(parsedQty) && parsedQty > 0 ? parsedQty : 1,
        totalPriceCents: Math.round(parseFloat(editPrice()) * 100) || 0,
      })

      // Update local state
      setStreamingItems((prev) => {
        const next = [...prev]
        next[index] = updated
        return next
      })
      setEditingIndex(-1)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to save")
    }
  }

  const handleApprove = async () => {
    const pr = proposal()
    if (!pr) return
    setApproving(true)
    setError("")
    try {
      await api.post(`/proposals/${pr.proposalId}/approve`, {
        choices: choices(),
      })
      navigate("/receipts")
    } catch (err) {
      setError(err instanceof Error ? err.message : "Approval failed")
    } finally {
      setApproving(false)
    }
  }

  // handleReparse kicks off a reparse with one of three engines:
  //   - "full": OCR (if configured) + LLM from text. Default.
  //   - "llm_text": skip OCR, reuse existing OcrMarkdown, call LLM.
  //   - "llm_image": skip OCR, send photo to LLM directly.
  const handleReparse = async (engine: "full" | "llm_text" | "llm_image") => {
    const id = params.id
    if (!id) return
    setError("")
    setStatus("loading")
    setStreamingItems([])
    setProgressMsg("")
    setPhotoSrc("")
    try {
      await api.post(`/proposals/${id}/reparse`, { engine })
      setStatus("parsing")
      const controller = new AbortController()
      // Note: a route-id change aborts via the stream effect's own
      // controller; this one-off stream ends on done/error/failed.
      void fetchSSE(id, controller.signal)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Reparse failed")
      setStatus("failed")
    }
  }

  // handleAddItem creates an empty ProposalItem on the backend,
  // appends it locally, and opens the inline editor immediately.
  const handleAddItem = async () => {
    const id = params.id
    if (!id) return
    setAddingItem(true)
    setError("")
    try {
      const newItem = await api.post(`/proposals/${id}/items`, {})
      setStreamingItems((prev) => [...prev, newItem])
      startEdit(streamingItems().length - 1)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Add failed")
    } finally {
      setAddingItem(false)
    }
  }

  // handleDeleteItem removes the item backend + locally, then shows a
  // 5s undo snackbar. Undo re-POSTs the captured data.
  const handleDeleteItem = async (index: number) => {
    const id = params.id
    if (!id) return
    const item = streamingItems()[index]
    if (!item) return
    try {
      await api.delete(`/proposals/${id}/items/${index}`)
      setStreamingItems((prev) => {
        const next = [...prev]
        next.splice(index, 1)
        return next
      })
      // If we were editing this item, exit edit mode.
      if (editingIndex() === index) setEditingIndex(-1)
      // New delete gets a full 5s window.
      if (deleteSnackbarTimer) clearTimeout(deleteSnackbarTimer)
      setRecentDelete({ item, index })
      deleteSnackbarTimer = setTimeout(() => {
        setRecentDelete(null)
        deleteSnackbarTimer = null
      }, 5000)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Delete failed")
    }
  }

  const handleUndoDelete = async () => {
    const id = params.id
    const rd = recentDelete()
    if (!id || !rd) return
    if (deleteSnackbarTimer) clearTimeout(deleteSnackbarTimer)
    deleteSnackbarTimer = null
    try {
      const restored = await api.post(`/proposals/${id}/items`, {
        parsedName: rd.item.parsedName,
        quantity: rd.item.quantity,
        unitPriceCents: rd.item.unitPriceCents,
        totalPriceCents: rd.item.totalPriceCents,
      })
      // Insert at the original index if still in range, else append.
      setStreamingItems((prev) => {
        const next = [...prev]
        next.splice(Math.min(rd.index, next.length), 0, restored)
        return next
      })
      setRecentDelete(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Undo failed")
    }
  }

  const handleApplyExternal = async () => {
    const id = params.id
    if (!id) return
    const content = tomlInput().trim()
    if (!content) {
      setApplyError("Paste TOML or JSON first")
      return
    }
    setApplyingExternal(true)
    setApplyError("")
    try {
      const updated = await api.post(`/proposals/${id}/apply-external`, { content })
      setProposal(updated)
      setStreamingItems(updated.items || [])
      setStatus("pending")
      setTomlInput("")
    } catch (err) {
      setApplyError(err instanceof Error ? err.message : "Apply failed")
    } finally {
      setApplyingExternal(false)
    }
  }

  // flashCopyConfirm shows a temporary "Copied" message. Auto-clears
  // after 2s; rapid re-clicks reset the timer.
  const flashCopyConfirm = (label: string) => {
    setCopyConfirm(label)
    if (copyConfirmTimer) clearTimeout(copyConfirmTimer)
    copyConfirmTimer = setTimeout(() => {
      setCopyConfirm("")
    }, 2000)
  }

  const handleCopy = async (text: string, label: string) => {
    const ok = await copyToClipboard(text)
    if (ok) flashCopyConfirm(`Copied ${label}`)
  }

  // choices ride along on approve (server auto-match populates
  // userChoice; explicit per-row choice UI is a future item).

  return (
    <div class="proposal-detail-wrapper">
      <Switch>
        <Match when={status() === "loading"}>
          <div>Loading...</div>
        </Match>
        <Match when={isInProgressStatus(status())}>
          <ParsingView
            progressMsg={progressMsg()}
            items={streamingItems()}
            onBack={() => navigate("/")}
          />
        </Match>
        <Match when={status() === "pending"}>
          <PendingView
            proposal={proposal()!}
            items={streamingItems()}
            photoSrc={photoSrc()}
            error={error()}
            approving={approving()}
            addingItem={addingItem()}
            editingIndex={editingIndex()}
            editName={editName()}
            editQty={editQty()}
            editPrice={editPrice()}
            onEditName={setEditName}
            onEditQty={setEditQty}
            onEditPrice={setEditPrice}
            onStartEdit={startEdit}
            onCancelEdit={cancelEdit}
            onSaveEdit={saveEdit}
            onDeleteItem={handleDeleteItem}
            onApprove={handleApprove}
            onBack={() => navigate("/")}
            onAddItem={handleAddItem}
          />
        </Match>
        <Match when={status() === "failed"}>
          <div class="proposal-failed">
            <div class="page-header">
              <h1>Parse Failed</h1>
              <button onClick={() => navigate("/")}>Back</button>
            </div>
            <div class="failed-content">
              <p class="error">{error() || "An error occurred while parsing the receipt"}</p>
              <Show when={proposal()?.photoUrl && photoSrc()}>
                <div class="proposal-photo-wrapper">
                  <div class="proposal-photo">
                    <ZoomableImageView src={photoSrc()} alt="Receipt" />
                  </div>
                  <div class="photo-actions">
                    <a href={photoSrc()} target="_blank" rel="noopener">
                      Open in new tab
                    </a>
                  </div>
                </div>
              </Show>
            </div>
            <div class="card-actions">
              <button onClick={() => handleReparse("full")} class="btn-primary">
                Retry Parsing
              </button>
              <button
                onClick={() => {
                  if (confirm("Delete this proposal?")) {
                    api.delete(`/proposals/${params.id}`).then(() => navigate("/"))
                  }
                }}
                class="btn-danger"
              >
                Delete
              </button>
            </div>
          </div>
        </Match>
        <Match when={status() === "approved"}>
          <div>
            <p>This proposal has been approved.</p>
            <button onClick={() => navigate("/receipts")}>View Receipts</button>
          </div>
        </Match>
        <Match when={true}>
          <div>Unknown state</div>
        </Match>
      </Switch>

      <ToolsPanel
        status={status()}
        ocr={proposal()?.ocrMarkdown || ""}
        showOcrDetails={showOcrDetails()}
        onToggleOcr={() => setShowOcrDetails((v) => !v)}
        copyConfirm={copyConfirm()}
        tomlInput={tomlInput()}
        onTomlInput={setTomlInput}
        applyError={applyError()}
        applyingExternal={applyingExternal()}
        onCopy={handleCopy}
        onReparse={handleReparse}
        onApplyExternal={handleApplyExternal}
      />

      {/* Delete-undo snackbar: fixed bottom, 5s window. */}
      <Show when={recentDelete()}>
        <div class="delete-snackbar">
          <span>Deleted "{recentDelete()!.item.parsedName || "item"}"</span>
          <button class="delete-snackbar-undo" onClick={handleUndoDelete}>
            Undo
          </button>
        </div>
      </Show>
    </div>
  )
}

function ParsingView(props: { progressMsg: string; items: ProposalItem[]; onBack: () => void }) {
  return (
    <div class="proposal-parsing">
      <div class="page-header">
        <h1>Parsing Receipt...</h1>
        <button onClick={props.onBack}>Back</button>
      </div>
      <div class="parsing-progress">
        <div class="skeleton-header">
          <div class="skeleton-line skeleton-merchant" />
          <div class="skeleton-line skeleton-date" />
          <div class="skeleton-line skeleton-total" />
        </div>
        <Show when={props.progressMsg}>
          <p class="progress-msg">{props.progressMsg}</p>
        </Show>
        <Show
          when={props.items.length > 0}
          fallback={
            <div class="parsing-placeholder">
              <div class="spinner" />
              <p>Waiting for items...</p>
            </div>
          }
        >
          <div class="streaming-items">
            <h2>Items ({props.items.length})</h2>
            <table>
              <tr>
                <th>Item</th>
                <th>Qty</th>
                <th>Price</th>
              </tr>
              <For each={props.items}>
                {(it) => (
                  <tr>
                    <td>{renderItemName(it)}</td>
                    <td>{String(it.quantity)}</td>
                    <td>{`$${(it.unitPriceCents / 100).toFixed(2)}`}</td>
                  </tr>
                )}
              </For>
            </table>
          </div>
        </Show>
      </div>
    </div>
  )
}

function PendingView(props: {
  proposal: Proposal
  items: ProposalItem[]
  photoSrc: string
  error: string
  approving: boolean
  addingItem: boolean
  editingIndex: number
  editName: string
  editQty: string
  editPrice: string
  onEditName: (v: string) => void
  onEditQty: (v: string) => void
  onEditPrice: (v: string) => void
  onStartEdit: (index: number) => void
  onCancelEdit: () => void
  onSaveEdit: () => void
  onDeleteItem: (index: number) => void
  onApprove: () => void
  onBack: () => void
  onAddItem: () => void
}) {
  const pr = () => props.proposal
  return (
    <div class="proposal-detail-page">
      <div class="page-header">
        <h1>{`${pr().merchant || "Receipt"}`}</h1>
        <button onClick={props.onBack}>Back</button>
      </div>
      <div class="proposal-layout">
        <div class="proposal-photo-wrapper">
          <div class="proposal-photo">
            <Show when={props.photoSrc} fallback={<p>No photo available</p>}>
              {(src) => <ZoomableImageView src={src()} alt="Receipt" />}
            </Show>
          </div>
          <Show when={props.photoSrc}>
            <div class="photo-actions">
              <a href={props.photoSrc} target="_blank" rel="noopener">
                Open in new tab
              </a>
            </div>
          </Show>
        </div>
        <div class="proposal-items">
          <h2>Items</h2>
          <div class="items-table-wrapper">
            <table>
              <tr>
                <th>Item</th>
                <th>Qty</th>
                <th>Price</th>
                <th>Action</th>
              </tr>
              <For each={props.items}>
                {(item, index) =>
                  // Single-row edit mode. Inputs bind the shared edit
                  // signals directly (one row editable at a time), so
                  // typing never recreates the <input> — no focus loss,
                  // no rawVal-style workaround needed.
                  props.editingIndex === index() ? (
                    <tr class="editing-row">
                      <td class="item-name-cell">
                        <input
                          type="text"
                          value={props.editName}
                          onInput={(e) => props.onEditName(e.currentTarget.value)}
                          class="edit-input"
                        />
                        {/* Keep the matched-item link beside the input
                            so the catalog entry under replacement stays
                            one click away. */}
                        <Show when={item.matchedItemId}>
                          <a
                            href={`#/items/${item.matchedItemId}`}
                            target="_blank"
                            rel="noopener"
                            class="matched-hint-link"
                            title={`Opens existing item #${item.matchedItemId} in a new tab`}
                          >
                            ↗ existing item
                          </a>
                        </Show>
                      </td>
                      <td>
                        <input
                          type="number"
                          value={props.editQty}
                          onInput={(e) => props.onEditQty(e.currentTarget.value)}
                          class="edit-input edit-qty"
                          // Weighted quantities (< 1, e.g. 0.92 kg)
                          // must not trip HTML5 number validation.
                          min="0"
                          step="0.01"
                        />
                      </td>
                      <td>
                        <input
                          type="number"
                          value={props.editPrice}
                          onInput={(e) => props.onEditPrice(e.currentTarget.value)}
                          class="edit-input edit-price"
                          min="0"
                          step="0.01"
                        />
                      </td>
                      <td class="edit-actions">
                        <button onClick={props.onSaveEdit} class="btn-sm btn-primary">
                          Save
                        </button>
                        <button onClick={props.onCancelEdit} class="btn-sm btn-secondary">
                          Cancel
                        </button>
                      </td>
                    </tr>
                  ) : (
                    // Normal display row. Weighted items show the line
                    // total as the main price, unit price as subtitle.
                    <ItemRow
                      item={item}
                      onEdit={() => props.onStartEdit(index())}
                      onDelete={() => props.onDeleteItem(index())}
                    />
                  )
                }
              </For>
            </table>
          </div>
          <button
            class="btn-secondary add-item-btn"
            disabled={props.addingItem}
            onClick={props.onAddItem}
          >
            {props.addingItem ? "Adding…" : "+ Add item"}
          </button>
          <div class="proposal-summary">
            <p>Total: {`$${(pr().totalCents / 100).toFixed(2)}`}</p>
            <p>Date: {pr().date ? new Date(pr().date * 1000).toLocaleString() : "Unknown"}</p>
          </div>
          <Show when={props.error}>
            <p class="error">{props.error}</p>
          </Show>
          <button
            onClick={props.onApprove}
            disabled={props.approving}
            class="approve-btn btn-primary"
          >
            {props.approving ? "Approving..." : "Approve Receipt"}
          </button>
        </div>
      </div>
    </div>
  )
}

function ItemRow(props: { item: ProposalItem; onEdit: () => void; onDelete: () => void }) {
  const item = () => props.item
  // Weighted: unit_price (per-kg) differs from the printed line total.
  const totalCents = () =>
    item().totalPriceCents && item().totalPriceCents! > 0
      ? item().totalPriceCents!
      : item().unitPriceCents
  const isWeighted = () => item().unitPriceCents !== totalCents() && item().quantity !== 1
  return (
    <tr>
      <td class="item-name-cell">
        {renderItemName(item())}
        <Show when={isWeighted()}>
          <div class="item-unit-price">@ {`$${(item().unitPriceCents / 100).toFixed(2)}`}/unit</div>
        </Show>
      </td>
      <td>{String(item().quantity)}</td>
      <td>{`$${(totalCents() / 100).toFixed(2)}`}</td>
      <td class="row-actions">
        <button onClick={props.onEdit} class="btn-sm btn-secondary">
          Edit
        </button>
        <button onClick={props.onDelete} class="btn-sm btn-danger" title="Delete item">
          ×
        </button>
      </td>
    </tr>
  )
}

function ToolsPanel(props: {
  status: string
  ocr: string
  showOcrDetails: boolean
  onToggleOcr: () => void
  copyConfirm: string
  tomlInput: string
  onTomlInput: (v: string) => void
  applyError: string
  applyingExternal: boolean
  onCopy: (text: string, label: string) => void
  onReparse: (engine: "full" | "llm_text" | "llm_image") => void
  onApplyExternal: () => void
}) {
  const reparseDisabled = () => props.status === "loading" || props.status === "parsing"
  return (
    <div class="tools-panel">
      <h2>Reparse & Override</h2>

      <div class="tools-section">
        <h3>Reparse</h3>
        <div class="reparse-buttons">
          <button
            class="btn-secondary"
            disabled={reparseDisabled()}
            onClick={() => props.onReparse("full")}
          >
            Full (OCR + LLM)
          </button>
          <button
            class="btn-secondary"
            disabled={reparseDisabled() || !props.ocr}
            title={!props.ocr ? "No OCR result to reuse; use Full or LLM (image)" : ""}
            onClick={() => props.onReparse("llm_text")}
          >
            LLM (existing OCR)
          </button>
          <button
            class="btn-secondary"
            disabled={reparseDisabled()}
            onClick={() => props.onReparse("llm_image")}
          >
            LLM (image)
          </button>
        </div>
      </div>

      <div class="tools-section">
        <h3>OCR details</h3>
        <button class="btn-secondary btn-sm" onClick={props.onToggleOcr}>
          {props.showOcrDetails ? "Hide OCR details" : "Show OCR details"}
        </button>
        <Show when={props.showOcrDetails}>
          <div class="ocr-details">
            <Show
              when={props.ocr}
              fallback={<p class="tools-empty">No OCR result yet.</p>}
            >
              <pre class="ocr-markdown">{props.ocr}</pre>
            </Show>
            <Show when={props.ocr}>
              <div class="copy-buttons">
                <button class="btn-secondary btn-sm" onClick={() => props.onCopy(props.ocr, "OCR")}>
                  Copy OCR
                </button>
              </div>
            </Show>
            <Show when={props.copyConfirm === "Copied OCR"}>
              <span class="copy-confirm">Copied</span>
            </Show>
          </div>
        </Show>
      </div>

      <div class="tools-section">
        <h3>External LLM</h3>
        <p class="tools-hint">
          Send the receipt image to ChatGPT/Claude with the prompt below, then paste the TOML
          response here.
        </p>
        <div class="copy-buttons">
          <button class="btn-secondary btn-sm" onClick={() => props.onCopy(TOML_SCHEMA, "schema")}>
            Copy schema
          </button>
          <button class="btn-secondary btn-sm" onClick={() => props.onCopy(LLM_PROMPT, "prompt")}>
            Copy prompt to LLM
          </button>
          <Show when={props.copyConfirm.startsWith("Copied ")}>
            <span class="copy-confirm">✓</span>
          </Show>
        </div>
        <textarea
          class="external-llm-input"
          placeholder="Paste TOML or JSON here…"
          value={props.tomlInput}
          onInput={(e) => props.onTomlInput(e.currentTarget.value)}
        />
        <Show when={props.applyError}>
          <p class="error">{props.applyError}</p>
        </Show>
        <button
          class="btn-primary"
          disabled={props.applyingExternal}
          onClick={props.onApplyExternal}
        >
          {props.applyingExternal ? "Applying…" : "Apply response"}
        </button>
      </div>
    </div>
  )
}
