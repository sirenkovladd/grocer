import { createEffect, createSignal, onCleanup, For, Show } from "solid-js"
import { createStore } from "solid-js/store"
import { A, useParams } from "@solidjs/router"
import QueryBoundary from "../components/QueryBoundary.tsx"
import ZoomableImageView from "../components/zoomable-image.tsx"
import { createQuery } from "../lib/query.ts"
import { api, idStr, navigate } from "../lib/api.ts"
import { formatDateTime, formatMoney, formatQuantity, setPageTitle, shortId } from "../utils.ts"
import { fetchPhotoUrl, revokePhotoUrl } from "../photos.ts"

// ID fields are `string`: backend serializes uint64 as JSON strings,
// values exceed Number.MAX_SAFE_INTEGER.
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

// Convert Unix seconds (UTC) to a YYYY-MM-DDTHH:MM string in the
// user's local timezone for <input type="datetime-local"> (which
// interprets its value in local time, no Z suffix). Round-trips the
// wall-clock the server stored in the user's tz.
const unixToDateTimeInput = (unixSecs: number): string => {
  const d = new Date(unixSecs * 1000)
  const yyyy = d.getFullYear()
  const mm = String(d.getMonth() + 1).padStart(2, "0")
  const dd = String(d.getDate()).padStart(2, "0")
  const hh = String(d.getHours()).padStart(2, "0")
  const mi = String(d.getMinutes()).padStart(2, "0")
  return `${yyyy}-${mm}-${dd}T${hh}:${mi}`
}

// Inverse of unixToDateTimeInput — parse the datetime-local string
// as local wall-clock, return Unix seconds.
const dateTimeInputToUnix = (yyyyMmDdThhMm: string): number => {
  return Math.floor(new Date(yyyyMmDdThhMm + ":00").getTime() / 1000)
}

function SkeletonHeader() {
  return (
    <div>
      <div class="skeleton-header">
        <div class="skeleton-line skeleton-merchant" />
        <div class="skeleton-line skeleton-date" />
      </div>
      <p class="muted">Loading…</p>
    </div>
  )
}

export default function ReceiptDetailPage() {
  const params = useParams()

  const { data, refetch } = createQuery(async () => {
    const r = (await api.get(`/receipts/${params.id}/enriched`)) as EnrichedReceipt
    return r
  })

  // Full-resolution photo URL. Loaded after the receipt resolves;
  // the blob URL is revoked when the page unmounts or the route id
  // changes (component reuse across /receipts/:id).
  const [photoSrc, setPhotoSrc] = createSignal("")
  createEffect(() => {
    const r = data.state === "ready" ? data() : undefined
    if (!r?.photoUrl) return
    let cancelled = false
    // Full original resolution; CSS scales it down and
    // ZoomableImageView provides pinch/scroll zoom for verifying
    // printed prices or item names.
    fetchPhotoUrl(r.receiptId, "full")
      .then((url) => {
        if (!cancelled) setPhotoSrc(url)
      })
      .catch((err) => {
        console.warn("Failed to load photo:", err)
        if (!cancelled) setPhotoSrc("")
      })
    onCleanup(() => {
      cancelled = true
    })
  })
  // Revoke this receipt's cached blob URLs on unmount / id change.
  // revokePhotoUrl drops the cache entries too — revoking the raw
  // object URL alone would leave a dead URL in the photo cache and
  // break the image when navigating back to this receipt.
  onCleanup(() => {
    const r = data.state === "ready" ? data() : undefined
    if (r) revokePhotoUrl(r.receiptId)
  })

  // Keep the tab title in sync with the merchant on screen.
  createEffect(() => {
    if (data.state === "pending" || data.state === "unresolved") {
      setPageTitle("Loading receipt…")
      return
    }
    if (data.state === "errored" || !data()?.receiptId) {
      setPageTitle("Receipt not found")
      return
    }
    const r = data()!
    setPageTitle(r.merchantName || `Receipt #${shortId(r.receiptId)}`)
  })

  // Edit mode state.
  const [isEditing, setIsEditing] = createSignal(false)
  const [editMerchantId, setEditMerchantId] = createSignal("")
  const [editDate, setEditDate] = createSignal("")
  const [editTotalDollars, setEditTotalDollars] = createSignal("")
  // Per-item edit drafts, keyed by row index. A store (not a record
  // of signals): setDrafts(i, key, v) preserves row identity, so
  // inputs bind value={draft.qty} with no focus loss — the VanJS
  // rawVal workaround is unnecessary because Solid never replaces
  // the <input>, it only sets .value.
  const [drafts, setDrafts] = createStore<{ itemId: string; qty: string; price: string }[]>([])
  const [savingEdit, setSavingEdit] = createSignal(false)
  const [editError, setEditError] = createSignal<string | null>(null)
  // Lookups loaded lazily on first edit (item catalog can be large;
  // this page is mostly viewed in read mode).
  const [merchants, setMerchants] = createSignal<Merchant[]>([])
  const [items, setItems] = createSignal<CatalogItem[]>([])
  const [loadingEditData, setLoadingEditData] = createSignal(false)
  const [reopening, setReopening] = createSignal(false)

  createEffect(() => {
    params.id
    setIsEditing(false)
    setEditError(null)
  })

  // Enter edit mode — seed drafts from the receipt, lazily load
  // merchant/item lookups for the dropdowns. Idempotent.
  const enterEdit = async () => {
    const r = data()
    if (!r) return
    setIsEditing(true)
    setEditMerchantId(r.merchantId)
    setEditDate(unixToDateTimeInput(r.date))
    setEditTotalDollars((r.totalCents / 100).toFixed(2))
    setEditError(null)

    setDrafts(
      r.items.map((it) => ({
        itemId: it.itemId,
        qty: String(it.quantity),
        price: (it.unitPriceCents / 100).toFixed(2),
      })),
    )

    if (merchants().length === 0 || items().length === 0) {
      setLoadingEditData(true)
      try {
        const [m, i] = await Promise.all([api.get("/merchants"), api.get("/items")])
        setMerchants(Array.isArray(m) ? m : [])
        setItems(Array.isArray(i) ? i : [])
      } catch (err) {
        console.error("Failed to load edit data:", err)
        setEditError((err as Error).message || "Failed to load edit data")
      } finally {
        setLoadingEditData(false)
      }
    }
  }

  const cancelEdit = () => {
    setIsEditing(false)
    setEditError(null)
  }

  // Re-open the receipt as a fresh proposal. Destructive (source
  // receipt deleted server-side), hence the confirm + in-flight lock
  // on both Edit and Re-open buttons.
  const handleReopen = async () => {
    const r = data()
    if (!r) return
    if (
      !confirm(
        `Re-open this receipt as a proposal?\n\n` +
          `The current receipt will be deleted and a new proposal created with the same items. ` +
          `After you re-approve, a new receipt will be committed.`,
      )
    ) {
      return
    }
    setReopening(true)
    try {
      const result = await api.post(`/receipts/${r.receiptId}/reopen`, {})
      navigate(`/proposals/${(result as any).id}`)
    } catch (err) {
      alert(`Re-open failed: ${(err as Error).message}`)
    } finally {
      setReopening(false)
    }
  }

  const saveEdit = async () => {
    const r = data()
    if (!r) return

    // Validate before sending — fail fast rather than a half-broken
    // batch of PATCH calls.
    const totalCents = Math.round(parseFloat(editTotalDollars()) * 100)
    if (isNaN(totalCents) || totalCents < 0) {
      setEditError("Total must be a non-negative number")
      return
    }
    const dateUnix = dateTimeInputToUnix(editDate())
    if (isNaN(dateUnix)) {
      setEditError("Invalid date")
      return
    }

    setSavingEdit(true)
    setEditError(null)
    try {
      // 1. Header (merchant, date, total).
      await api.patch(`/receipts/${r.receiptId}`, {
        merchantId: idStr(editMerchantId()),
        date: dateUnix,
        totalCents,
      })

      // 2. Every row (even unchanged — cheaper than diffing).
      for (let i = 0; i < r.items.length; i++) {
        const d = drafts[i]
        const qty = parseFloat(d?.qty ?? "")
        const priceCents = Math.round(parseFloat(d?.price ?? "") * 100)
        if (isNaN(qty) || qty <= 0) {
          throw new Error(`Row ${i + 1}: quantity must be a positive number`)
        }
        if (isNaN(priceCents) || priceCents < 0) {
          throw new Error(`Row ${i + 1}: unit price must be a non-negative number`)
        }
        await api.patch(`/receipts/${r.receiptId}/items/${i}`, {
          itemId: idStr(d?.itemId),
          quantity: qty,
          unitPriceCents: priceCents,
        })
      }

      // 3. Reload the enriched receipt (server re-derives line totals).
      refetch()
      setIsEditing(false)
    } catch (err) {
      setEditError((err as Error).message || "Save failed")
    } finally {
      setSavingEdit(false)
    }
  }

  // Body reads the resource (suspending until ready). Closure
  // component so data reads stay inside the boundary.
  const Body = () => {
    const r = () => data()!
    const hasItems = () => !!r().items && r().items.length > 0
    const sortedMerchants = () => [...merchants()].sort((a, b) => a.name.localeCompare(b.name))
    const sortedItems = () => [...items()].sort((a, b) => a.name.localeCompare(b.name))
    return (
      <>
        <div class="breadcrumb">
          <A href="/receipts">Receipts</A>
          <span class="separator">›</span>
          <span class="current">{r().merchantName || `Receipt #${shortId(r().receiptId)}`}</span>
        </div>

        <div class="page-header">
          <Show
            when={isEditing()}
            fallback={
              <div>
                <h1>{r().merchantName || `Receipt #${shortId(r().receiptId)}`}</h1>
                <div class="page-header-meta">
                  <span class="muted">{formatDateTime(r().date)}</span>
                  <span class="money">{formatMoney(r().totalCents)}</span>
                </div>
              </div>
            }
          >
            {/* Edit mode header: editable fields. */}
            <div class="edit-header">
              <div class="edit-field">
                <span class="edit-label">Merchant</span>
                <select
                  onChange={(e) => setEditMerchantId(e.currentTarget.value)}
                  disabled={savingEdit()}
                  class="edit-input"
                  aria-label="Merchant"
                >
                  <For each={sortedMerchants()}>
                    {(m) => (
                      <option value={m.merchantId} selected={editMerchantId() === m.merchantId}>
                        {m.name}
                      </option>
                    )}
                  </For>
                </select>
              </div>
              <div class="edit-field">
                <span class="edit-label">Date</span>
                <input
                  type="datetime-local"
                  value={editDate()}
                  onInput={(e) => setEditDate(e.currentTarget.value)}
                  disabled={savingEdit()}
                  class="edit-input"
                  aria-label="Date and time"
                />
              </div>
              <div class="edit-field">
                <span class="edit-label">Total ($)</span>
                <input
                  type="number"
                  step="0.01"
                  min="0"
                  value={editTotalDollars()}
                  onInput={(e) => setEditTotalDollars(e.currentTarget.value)}
                  disabled={savingEdit()}
                  class="edit-input money"
                  aria-label="Total in dollars"
                />
              </div>
            </div>
          </Show>
          <Show
            when={isEditing()}
            fallback={
              <div class="page-header-actions">
                <button onClick={enterEdit} class="btn-secondary" disabled={reopening()}>
                  Edit
                </button>
                <button onClick={handleReopen} class="btn-secondary" disabled={reopening()}>
                  {reopening() ? "Re-opening…" : "Re-open as Proposal"}
                </button>
                <button onClick={() => navigate("/receipts")} class="btn-secondary">
                  Back
                </button>
              </div>
            }
          >
            <div class="page-header-actions">
              <button
                onClick={saveEdit}
                disabled={savingEdit() || loadingEditData()}
                class="btn-primary"
              >
                {savingEdit() ? "Saving…" : "Save"}
              </button>
              <button onClick={cancelEdit} disabled={savingEdit()} class="btn-secondary">
                Cancel
              </button>
            </div>
          </Show>
        </div>

        {/* Edit-mode error (between header and table, no scroll needed). */}
        <Show when={isEditing() && editError()}>
          <p class="error">{editError()}</p>
        </Show>

        {/* Two-column layout: photo left, items right. Stacks on mobile. */}
        <div class="receipt-detail-layout">
          <Show when={photoSrc()}>
            <div class="receipt-photo">
              <ZoomableImageView src={photoSrc()} alt="Receipt" />
              <p class="receipt-photo-hint muted">Scroll or pinch to zoom</p>
            </div>
          </Show>

          <div class="receipt-items-column">
            <h2>Items</h2>
            <Show
              when={hasItems()}
              fallback={
                <div class="empty-state">
                  <p>No items on this receipt.</p>
                </div>
              }
            >
              <div class="items-table-wrapper">
                <table class="responsive-table">
                  <tr>
                    <th>Item</th>
                    <th>Category</th>
                    <th>Qty</th>
                    <th class="money">Unit Price</th>
                    <th class="money">Total</th>
                  </tr>
                  <For each={r().items}>
                    {(item, index) =>
                      isEditing() ? (
                        // Edit-mode row: item dropdown + numeric inputs.
                        // Per-option `selected` (not select `value=`):
                        // the browser drops a select.value set before
                        // options exist. Inputs bind store drafts
                        // directly — Solid sets .value without
                        // replacing the element, so focus is safe.
                        <tr class="editing-row">
                          <td data-label="Item">
                            <select
                              onChange={(e) => setDrafts(index(), { itemId: e.currentTarget.value })}
                              disabled={savingEdit()}
                              class="edit-input"
                              aria-label={`Item for row ${index() + 1}`}
                            >
                              <For each={sortedItems()}>
                                {(it) => (
                                  <option
                                    value={it.itemId}
                                    selected={drafts[index()]?.itemId === it.itemId}
                                  >
                                    {it.name}
                                  </option>
                                )}
                              </For>
                            </select>
                          </td>
                          <td data-label="Category">
                            <span class="category-badge">{item.categoryName || "Uncategorized"}</span>
                          </td>
                          <td data-label="Qty">
                            <input
                              type="number"
                              step="0.001"
                              min="0"
                              value={drafts[index()]?.qty ?? ""}
                              onInput={(e) => setDrafts(index(), { qty: e.currentTarget.value })}
                              disabled={savingEdit()}
                              class="edit-input edit-input-num"
                              aria-label={`Quantity for row ${index() + 1}`}
                            />
                          </td>
                          <td data-label="Unit Price">
                            <input
                              type="number"
                              step="0.01"
                              min="0"
                              value={drafts[index()]?.price ?? ""}
                              onInput={(e) => setDrafts(index(), { price: e.currentTarget.value })}
                              disabled={savingEdit()}
                              class="edit-input edit-input-num money"
                              aria-label={`Unit price for row ${index() + 1}`}
                            />
                          </td>
                          {/* Live-computed preview as the user types. */}
                          <td data-label="Total" class="money muted">
                            {liveTotal(index())}
                          </td>
                        </tr>
                      ) : (
                        // Read-mode row.
                        <tr>
                          <td data-label="Item">
                            <A href={`/items/${item.itemId}`} class="item-name-link">
                              {item.name}
                            </A>
                          </td>
                          <td data-label="Category">
                            <span class="category-badge">{item.categoryName || "Uncategorized"}</span>
                          </td>
                          <td data-label="Qty">
                            {formatQuantity(item.quantity)}
                            <Show
                              when={item.quantity !== 1 && item.unitPriceCents !== item.totalPriceCents}
                            >
                              <div class="item-unit-price">
                                @ {formatMoney(item.unitPriceCents)}/unit
                              </div>
                            </Show>
                          </td>
                          <td data-label="Unit Price" class="money muted">
                            {formatMoney(item.unitPriceCents)}
                          </td>
                          <td data-label="Total" class="money">
                            {formatMoney(item.totalPriceCents)}
                          </td>
                        </tr>
                      )
                    }
                  </For>
                </table>
              </div>
            </Show>
          </div>
        </div>
      </>
    )
  }

  // Live line-total preview for the edit row. Reads the drafts store
  // in JSX (reactive by design — this cell WANTS to re-run per keystroke).
  const liveTotal = (index: number): string => {
    const q = parseFloat(drafts[index]?.qty ?? "0")
    const p = parseFloat(drafts[index]?.price ?? "0")
    if (isNaN(q) || isNaN(p)) return "—"
    return formatMoney(Math.round(q * p * 100))
  }

  return (
    <div class="receipt-detail-page">
      <QueryBoundary pending={<SkeletonHeader />}>
        <Show
          when={data()?.receiptId}
          fallback={
            <div class="empty-state">
              <h2>Receipt not found</h2>
              <button onClick={() => navigate("/receipts")}>Back to receipts</button>
            </div>
          }
        >
          <Body />
        </Show>
      </QueryBoundary>
    </div>
  )
}
