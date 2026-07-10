import van from "vanjs-core"
import { api, idStr, navigate } from "../main"
import { formatDateTime, formatMoney, formatQuantity, shortId } from "../utils"
import { fetchPhotoUrl, revokePhotoUrl } from "../photos"
import { ZoomableImage } from "../components/zoomable-image"

const { div, h1, h2, a, span, table, tr, td, th, button, p, img, input, select, option } = van.tags

// ID fields are `string` per the project's ticket 04 decision: backend
// serializes uint64 as JSON strings, and values exceed
// Number.MAX_SAFE_INTEGER. See ticket 04 decisions log.
interface EnrichedReceiptItem {
  itemId: string
  name: string
  categoryId: string
  categoryName: string
  quantity: number
  unitPriceCents: number
  totalPriceCents: number
}

interface EnrichedReceipt {
  receiptId: string
  merchantId: string
  merchantName: string
  ownerId: string
  ownerName: string // loaded but not displayed in this view per UX overhaul plan
  date: number
  photoUrl?: string
  items: EnrichedReceiptItem[]
  totalCents: number
}

interface Merchant {
  merchantId: string
  name: string
}

interface CatalogItem {
  itemId: string
  name: string
}

const SkeletonItem = () =>
  tr(
    td(div({ class: "skeleton-cell skeleton-cell-md" })),
    td(div({ class: "skeleton-cell skeleton-cell-sm" })),
    td(div({ class: "skeleton-cell skeleton-cell-sm" })),
    td(div({ class: "skeleton-cell skeleton-cell-sm" })),
    td(div({ class: "skeleton-cell skeleton-cell-sm" })),
  )

const Breadcrumb = (merchantName: string, receiptId: string) =>
  div({ class: "breadcrumb" },
    a({
      href: "#/receipts",
      onclick: (e: Event) => { e.preventDefault(); navigate("/receipts") },
    }, "Receipts"),
    span({ class: "separator" }, "›"),
    span({ class: "current" }, merchantName || `Receipt #${shortId(receiptId)}`),
  )

// Convert Unix seconds (UTC) to a YYYY-MM-DDTHH:MM string in the
// user's local timezone. Used as the value of <input type="datetime-local">
// in the edit form, which always interprets its value in the user's
// local timezone (no Z suffix) — so we round-trip through the local
// Date methods to get the same wall-clock the server stored in the
// user's tz.
//
// The server now anchors LLM/OCR dates to noon in the user's timezone
// (see internal/llm/base.go parseDateInTimezone) so the calendar
// date is correct in every timezone. This helper preserves the
// time-of-day the user (or the LLM) picked.
const unixToDateTimeInput = (unixSecs: number): string => {
  const d = new Date(unixSecs * 1000)
  const yyyy = d.getFullYear()
  const mm = String(d.getMonth() + 1).padStart(2, "0")
  const dd = String(d.getDate()).padStart(2, "0")
  const hh = String(d.getHours()).padStart(2, "0")
  const mi = String(d.getMinutes()).padStart(2, "0")
  return `${yyyy}-${mm}-${dd}T${hh}:${mi}`
}

// Inverse of unixToDateTimeInput — parse the YYYY-MM-DDTHH:MM string
// from a datetime-local input as a wall-clock in the user's local
// timezone and return Unix seconds. Appends ":00" seconds so the
// second component is always present (the input strips them).
const dateTimeInputToUnix = (yyyyMmDdThhMm: string): number => {
  return Math.floor(new Date(yyyyMmDdThhMm + ":00").getTime() / 1000)
}

const ReceiptDetailPage = () => {
  const receipt = van.state<EnrichedReceipt | null>(null)
  const photoSrc = van.state<string>("")
  const loading = van.state(true)
  const error = van.state<string | null>(null)
  let currentReceiptId: string | null = null

  // Edit mode state.
  const isEditing = van.state(false)
  const editMerchantId = van.state("")
  const editDate = van.state("")
  const editTotalDollars = van.state("")
  // Per-item edit state, keyed by item index. Stored as VanJS state
  // objects so the inputs are reactive.
  const editItemItemId = van.state<Record<number, string>>({})
  const editItemQty = van.state<Record<number, string>>({})
  const editItemPriceDollars = van.state<Record<number, string>>({})

  const savingEdit = van.state(false)
  const editError = van.state<string | null>(null)
  // Lookups loaded lazily on first edit. We avoid preloading because
  // the item catalog can be large; the receipt detail page is mostly
  // viewed in read mode.
  const merchants = van.state<Merchant[]>([])
  const items = van.state<CatalogItem[]>([])
  const loadingEditData = van.state(false)

  // Parse the receipt ID from the hash. Robust to:
  //   - trailing slashes (`#/receipts/123/` → "123")
  //   - the `/enriched` API suffix leaking into the URL
  //     (`#/receipts/123/enriched` → "123")
  //   - 404 paths returning `{"error": "..."}` JSON
  const parseIdFromHash = (): string => {
    const segments = window.location.hash.split("/").filter(Boolean)
    for (const seg of segments) {
      if (/^\d+$/.test(seg)) return seg
    }
    return ""
  }

  const id = parseIdFromHash()

  const load = async () => {
    if (!id) return
    loading.val = true
    error.val = null
    try {
      const data = await api.get(`/receipts/${id}/enriched`)
      receipt.val = data
      loading.val = false

      if (currentReceiptId && currentReceiptId !== data.receiptId) {
        revokePhotoUrl(currentReceiptId)
        photoSrc.val = ""
      }
      currentReceiptId = data.receiptId

      if (data?.photoUrl) {
        // Full original resolution. The image is displayed scaled
        // down to fit the column via CSS, and ZoomableImage lets
        // the user pinch/scroll to zoom into the original detail
        // (useful for verifying a printed price or item name).
        fetchPhotoUrl(data.receiptId, "full")
          .then(url => { photoSrc.val = url })
          .catch(err => {
            console.warn("Failed to load photo:", err)
            photoSrc.val = ""
          })
      }
    } catch (err) {
      console.error("Failed to load receipt:", err)
      error.val = (err as Error).message || "Failed to load receipt"
      loading.val = false
    }
  }

  load()

  // Enter edit mode — populate the edit-* state from the current
  // receipt and lazily load the merchant and item lookups for the
  // dropdowns. Idempotent: if data is already loaded, the API calls
  // are skipped.
  const enterEdit = async () => {
    const r = receipt.val
    if (!r) return

    isEditing.val = true
    editMerchantId.val = r.merchantId
    editDate.val = unixToDateTimeInput(r.date)
    editTotalDollars.val = (r.totalCents / 100).toFixed(2)
    editError.val = null

    // Seed per-item edit state from current values.
    const ids: Record<number, string> = {}
    const qtys: Record<number, string> = {}
    const prices: Record<number, string> = {}
    r.items.forEach((it, i) => {
      ids[i] = it.itemId
      qtys[i] = String(it.quantity)
      prices[i] = (it.unitPriceCents / 100).toFixed(2)
    })
    editItemItemId.val = ids
    editItemQty.val = qtys
    editItemPriceDollars.val = prices

    // Load lookups only once.
    if (merchants.val.length === 0 || items.val.length === 0) {
      loadingEditData.val = true
      try {
        const [m, i] = await Promise.all([
          api.get("/merchants"),
          api.get("/items"),
        ])
        merchants.val = Array.isArray(m) ? m : []
        items.val = Array.isArray(i) ? i : []
      } catch (err) {
        console.error("Failed to load edit data:", err)
        editError.val = (err as Error).message || "Failed to load edit data"
      } finally {
        loadingEditData.val = false
      }
    }
  }

  const cancelEdit = () => {
    isEditing.val = false
    editError.val = null
  }

  // Re-open the current receipt as a fresh proposal. Destructive
  // (the source receipt is deleted server-side; the user re-approves
  // the new proposal to recreate it), so we prompt for confirmation
  // and disable both the Edit and Re-open buttons while in flight.
  const reopening = van.state(false)
  const handleReopen = async () => {
    const r = receipt.val
    if (!r) return
    if (!confirm(
      `Re-open this receipt as a proposal?\n\n` +
      `The current receipt will be deleted and a new proposal created with the same items. ` +
      `After you re-approve, a new receipt will be committed.`,
    )) {
      return
    }
    reopening.val = true
    try {
      const result = await api.post(`/receipts/${r.receiptId}/reopen`, {})
      const newId = (result as any).id
      navigate(`/proposals/${newId}`)
    } catch (err) {
      alert(`Re-open failed: ${(err as Error).message}`)
    } finally {
      reopening.val = false
    }
  }

  const saveEdit = async () => {
    const r = receipt.val
    if (!r) return

    // Validate before sending — fail fast on bad input rather than
    // sending a half-broken batch of PATCH calls.
    const totalCents = Math.round(parseFloat(editTotalDollars.val) * 100)
    if (isNaN(totalCents) || totalCents < 0) {
      editError.val = "Total must be a non-negative number"
      return
    }
    const dateUnix = dateTimeInputToUnix(editDate.val)
    if (isNaN(dateUnix)) {
      editError.val = "Invalid date"
      return
    }

    savingEdit.val = true
    editError.val = null
    try {
      // 1. Update header (merchant, date, total).
      await api.patch(`/receipts/${r.receiptId}`, {
        merchantId: idStr(editMerchantId.val),
        date: dateUnix,
        totalCents,
      })

      // 2. Update each item that changed. We PATCH every row even
      // when nothing changed, because identifying "changed" rows
      // is more code than the round-trip; the backend is cheap.
      for (let i = 0; i < r.items.length; i++) {
        const qty = parseFloat(editItemQty.val[i] ?? "")
        const priceCents = Math.round(parseFloat(editItemPriceDollars.val[i] ?? "") * 100)
        if (isNaN(qty) || qty <= 0) {
          throw new Error(`Row ${i + 1}: quantity must be a positive number`)
        }
        if (isNaN(priceCents) || priceCents < 0) {
          throw new Error(`Row ${i + 1}: unit price must be a non-negative number`)
        }
        await api.patch(`/receipts/${r.receiptId}/items/${i}`, {
          itemId: idStr(editItemItemId.val[i]),
          quantity: qty,
          unitPriceCents: priceCents,
        })
      }

      // 3. Reload the enriched receipt to pick up the new state and
      // re-derive per-line totals on the server.
      await load()
      isEditing.val = false
    } catch (err) {
      editError.val = (err as Error).message || "Save failed"
    } finally {
      savingEdit.val = false
    }
  }

  return div({ class: "receipt-detail-page" },
    () => {
      if (loading.val) {
        return div(
          div({ class: "skeleton-header" },
            div({ class: "skeleton-line skeleton-merchant" }),
            div({ class: "skeleton-line skeleton-date" }),
          ),
          p({ class: "muted" }, "Loading…"),
        )
      }
      if (error.val) {
        return div({ class: "empty-state" },
          h2("Couldn't load receipt"),
          p(error.val),
          button({ onclick: load }, "Try Again"),
        )
      }
      if (!receipt.val) {
        return div({ class: "empty-state" },
          h2("Receipt not found"),
          button({ onclick: () => navigate("/receipts") }, "Back to receipts"),
        )
      }

      const r = receipt.val
      const hasItems = r.items && r.items.length > 0
      const sortedMerchants = [...merchants.val].sort((a, b) => a.name.localeCompare(b.name))
      const sortedItems = [...items.val].sort((a, b) => a.name.localeCompare(b.name))

      return div(
        Breadcrumb(r.merchantName, r.receiptId),

        div({ class: "page-header" },
          isEditing.val
            // Edit mode header: editable fields.
            ? div({ class: "edit-header" },
                div({ class: "edit-field" },
                  span({ class: "edit-label" }, "Merchant"),
                  select({
                    value: editMerchantId,
                    onchange: (e: Event) => {
                      editMerchantId.val = (e.target as HTMLSelectElement).value
                    },
                    disabled: savingEdit,
                    class: "edit-input",
                    "aria-label": "Merchant",
                  },
                    ...sortedMerchants.map(m =>
                      option({ value: m.merchantId }, m.name),
                    ),
                  ),
                ),
                div({ class: "edit-field" },
                  span({ class: "edit-label" }, "Date"),
                  input({
                    type: "datetime-local",
                    value: editDate,
                    oninput: (e: Event) => {
                      editDate.val = (e.target as HTMLInputElement).value
                    },
                    disabled: savingEdit,
                    class: "edit-input",
                    "aria-label": "Date and time",
                  }),
                ),
                div({ class: "edit-field" },
                  span({ class: "edit-label" }, "Total ($)"),
                  input({
                    type: "number",
                    step: "0.01",
                    min: "0",
                    value: editTotalDollars,
                    oninput: (e: Event) => {
                      editTotalDollars.val = (e.target as HTMLInputElement).value
                    },
                    disabled: savingEdit,
                    class: "edit-input money",
                    "aria-label": "Total in dollars",
                  }),
                ),
              )
            // Read mode header: title + meta.
            : div(
              h1(r.merchantName || `Receipt #${shortId(r.receiptId)}`),
              div({ class: "page-header-meta" },
                span({ class: "muted" }, formatDateTime(r.date)),
                span({ class: "money" }, formatMoney(r.totalCents)),
              ),
            ),
          isEditing.val
            ? div({ class: "page-header-actions" },
                button({
                  onclick: saveEdit,
                  disabled: savingEdit || loadingEditData.val,
                  class: "btn-primary",
                }, () => savingEdit.val ? "Saving…" : "Save"),
                button({
                  onclick: cancelEdit,
                  disabled: savingEdit,
                  class: "btn-secondary",
                }, "Cancel"),
              )
            : div({ class: "page-header-actions" },
                button({
                  onclick: enterEdit,
                  class: "btn-secondary",
                  disabled: () => reopening.val,
                }, "Edit"),
                button({
                  onclick: handleReopen,
                  class: "btn-secondary",
                  disabled: () => reopening.val,
                }, () => reopening.val ? "Re-opening…" : "Re-open as Proposal"),
                button({
                  onclick: () => navigate("/receipts"),
                  class: "btn-secondary",
                }, "Back"),
              ),
        ),

        // Edit-mode error display (sits between the header and the
        // items table so it's visible without scrolling).
        isEditing.val && editError.val
          ? p({ class: "error" }, editError.val)
          : "",

        // Two-column layout: photo on the left, items + header on
        // the right. Receipts are always vertical, so the photo
        // column is narrower and the image is rendered with
        // object-fit: contain to show the whole receipt. The
        // photo is the full original resolution and supports
        // pinch/scroll zoom via ZoomableImage. Stacks on mobile.
        div({ class: "receipt-detail-layout" },
          // Photo column
          () => photoSrc.val
            ? div({ class: "receipt-photo" },
                ZoomableImage(() => photoSrc.val, "Receipt"),
                p({ class: "receipt-photo-hint muted" }, "Scroll or pinch to zoom"),
              )
            : "",

          // Items column
          div({ class: "receipt-items-column" },
            h2("Items"),
            hasItems
              ? div({ class: "items-table-wrapper" },
              table({ class: "responsive-table" },
                tr(
                  th("Item"),
                  th("Category"),
                  th("Qty"),
                  th({ class: "money" }, "Unit Price"),
                  th({ class: "money" }, "Total"),
                ),
                ...r.items.map((item, index) => {
                  if (isEditing.val) {
                    // Edit-mode row: item dropdown + numeric inputs.
                    //
                    // IMPORTANT: each row is wrapped in a function-child
                    // so the state reads inside it (the per-row values,
                    // the dynamic sortedItems.map children) create a
                    // LOCAL dependency context. Without this wrapper,
                    // those reads leak into the surrounding page-level
                    // function-child context, and VanJS treats
                    // `value: () => editItemItemId.val[index]` as a
                    // one-time getter (functions are not bindings) so
                    // every row ends up showing the first item's value.
                    // See AGENTS.md 'VanJS Gotchas'.
                    return (() => {
                      // Read the current item once at the top of the
                      // function-child so both the <select value> and
                      // the <option selected> use the same source.
                      //
                      // The <option selected> attribute is the real
                      // fix for the bug: setting select.value before
                      // its <option> children are appended doesn't
                      // work, because the browser records the value
                      // at element-creation time and doesn't re-check
                      // it when options are added. Marking the
                      // matching option as selected makes the browser
                      // respect the choice regardless of the order of
                      // attribute/child setup.
                      const currentItemId = editItemItemId.val[index] ?? ""
                      return tr({ class: "editing-row" },
                      td({ "data-label": "Item" },
                        select({
                          value: currentItemId,
                          onchange: (e: Event) => {
                            const v = (e.target as HTMLSelectElement).value
                            editItemItemId.val = { ...editItemItemId.val, [index]: v }
                          },
                          disabled: savingEdit,
                          class: "edit-input",
                          "aria-label": `Item for row ${index + 1}`,
                        },
                          ...sortedItems.map(it =>
                            option({
                              value: it.itemId,
                              selected: it.itemId === currentItemId,
                            }, it.name),
                          ),
                        ),
                      ),
                      td({ "data-label": "Category" },
                        span({ class: "category-badge" }, item.categoryName || "Uncategorized"),
                      ),
                      td({ "data-label": "Qty" },
                        input({
                          type: "number",
                          step: "0.001",
                          min: "0",
                          value: editItemQty.val[index] ?? "",
                          oninput: (e: Event) => {
                            const v = (e.target as HTMLInputElement).value
                            editItemQty.val = { ...editItemQty.val, [index]: v }
                          },
                          disabled: savingEdit,
                          class: "edit-input edit-input-num",
                          "aria-label": `Quantity for row ${index + 1}`,
                        }),
                      ),
                      td({ "data-label": "Unit Price" },
                        input({
                          type: "number",
                          step: "0.01",
                          min: "0",
                          value: editItemPriceDollars.val[index] ?? "",
                          oninput: (e: Event) => {
                            const v = (e.target as HTMLInputElement).value
                            editItemPriceDollars.val = { ...editItemPriceDollars.val, [index]: v }
                          },
                          disabled: savingEdit,
                          class: "edit-input edit-input-num money",
                          "aria-label": `Unit price for row ${index + 1}`,
                        }),
                      ),
                      td({ "data-label": "Total", class: "money muted" },
                        // Live-computed preview as the user types. Uses
                        // qty * price in cents, rounded.
                        () => {
                          const q = parseFloat(editItemQty.val[index] ?? "0")
                          const p = parseFloat(editItemPriceDollars.val[index] ?? "0")
                          if (isNaN(q) || isNaN(p)) return "—"
                          return formatMoney(Math.round(q * p * 100))
                        },
                      ),
                      )
                    })()
                  }
                  // Read-mode row (unchanged from previous behavior).
                  const isWeighted =
                    item.quantity !== 1 &&
                    item.unitPriceCents !== item.totalPriceCents
                  return tr(
                    td({ "data-label": "Item" },
                      a({
                        href: `#/items/${item.itemId}`,
                        class: "item-name-link",
                        onclick: (e: Event) => {
                          e.preventDefault()
                          navigate(`/items/${item.itemId}`)
                        },
                      }, item.name),
                    ),
                    td({ "data-label": "Category" },
                      span({ class: "category-badge" }, item.categoryName || "Uncategorized"),
                    ),
                    td({ "data-label": "Qty" },
                      formatQuantity(item.quantity),
                      isWeighted
                        ? div({ class: "item-unit-price" }, `@ ${formatMoney(item.unitPriceCents)}/unit`)
                        : "",
                    ),
                    td({ "data-label": "Unit Price", class: "money muted" }, formatMoney(item.unitPriceCents)),
                    td({ "data-label": "Total", class: "money" }, formatMoney(item.totalPriceCents)),
                  )
                }),
              ),
            )
          : div({ class: "empty-state" },
              p("No items on this receipt."),
            ),
          ),  // close .receipt-items-column
        ),  // close .receipt-detail-layout
      )
    },
  )
}

export default ReceiptDetailPage
