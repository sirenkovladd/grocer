import { createSignal, For, Show } from "solid-js"
import { createStore } from "solid-js/store"
import QueryBoundary from "../components/QueryBoundary.tsx"
import { createQuery } from "../lib/query.ts"
import { api, idStr, navigate } from "../lib/api.ts"
import { formatMoney } from "../utils.ts"

// Manual receipt entry form. Used when the LLM can't parse a photo
// or for historic receipts. Skips photo + LLM + propose entirely and
// commits via POST /api/receipts/manual.
//
// Rows live in a createStore array with stable per-row `key`s.
// setRows(index, patch) mutates the row in place, preserving object
// identity, so <For> never recreates a row's inputs while typing
// (no focus loss). add/remove keep survivor identities.

interface Merchant {
  merchantId: string
  name: string
}

interface CatalogItem {
  itemId: string
  name: string
}

interface ItemRow {
  key: number
  itemId: string
  quantity: string
  unitPriceDollars: string
}

const unixToDateTimeInput = (unixSecs: number): string => {
  const d = new Date(unixSecs * 1000)
  const yyyy = d.getFullYear()
  const mm = String(d.getMonth() + 1).padStart(2, "0")
  const dd = String(d.getDate()).padStart(2, "0")
  const hh = String(d.getHours()).padStart(2, "0")
  const mi = String(d.getMinutes()).padStart(2, "0")
  return `${yyyy}-${mm}-${dd}T${hh}:${mi}`
}

const dateTimeInputToUnix = (yyyyMmDdThhMm: string): number => {
  return Math.floor(new Date(yyyyMmDdThhMm + ":00").getTime() / 1000)
}

export default function ManualReceiptPage() {
  // Form state (static inputs — outside the lookups boundary).
  const [merchantId, setMerchantId] = createSignal("")
  const [date, setDate] = createSignal(unixToDateTimeInput(Math.floor(Date.now() / 1000)))
  const [totalDollars, setTotalDollars] = createSignal("")
  const [rows, setRows] = createStore<ItemRow[]>([
    { key: 0, itemId: "", quantity: "1", unitPriceDollars: "" },
  ])
  const [submitting, setSubmitting] = createSignal(false)
  const [submitError, setSubmitError] = createSignal<string | null>(null)
  let nextKey = 1

  const { data } = createQuery(async () => {
    const [m, i] = await Promise.all([api.get("/merchants"), api.get("/items")])
    return {
      merchants: (Array.isArray(m) ? m : []) as Merchant[],
      items: (Array.isArray(i) ? i : []) as CatalogItem[],
    }
  })

  const addRow = () => {
    setRows(rows.length, { key: nextKey++, itemId: "", quantity: "1", unitPriceDollars: "" })
  }

  const removeRow = (index: number) => {
    if (rows.length === 1) return
    setRows(rows.filter((_, i) => i !== index))
  }

  const handleSubmit = async (e: Event) => {
    e.preventDefault()
    setSubmitError(null)

    if (!merchantId()) {
      setSubmitError("Pick a merchant")
      return
    }
    const dateUnix = dateTimeInputToUnix(date())
    if (isNaN(dateUnix)) {
      setSubmitError("Pick a valid date")
      return
    }
    const totalCents = Math.round(parseFloat(totalDollars() || "0") * 100)
    if (isNaN(totalCents) || totalCents < 0) {
      setSubmitError("Total must be a non-negative number")
      return
    }
    const cleanRows: { itemId: string; quantity: number; unitPriceCents: number }[] = []
    for (let i = 0; i < rows.length; i++) {
      const r = rows[i]!
      if (!r.itemId) {
        setSubmitError(`Row ${i + 1}: pick an item`)
        return
      }
      const qty = parseFloat(r.quantity)
      const priceCents = Math.round(parseFloat(r.unitPriceDollars || "0") * 100)
      if (isNaN(qty) || qty <= 0) {
        setSubmitError(`Row ${i + 1}: quantity must be a positive number`)
        return
      }
      if (isNaN(priceCents) || priceCents < 0) {
        setSubmitError(`Row ${i + 1}: unit price must be a non-negative number`)
        return
      }
      cleanRows.push({ itemId: idStr(r.itemId)!, quantity: qty, unitPriceCents: priceCents })
    }

    setSubmitting(true)
    try {
      // Field names match the server's userInputReceipt struct 1:1
      // (see internal/llm/llm_user_input.go).
      const resp = (await api.post("/receipts/manual", {
        merchantId: idStr(merchantId())!,
        date: dateUnix,
        totalCents,
        items: cleanRows,
      })) as { receiptId: string }
      navigate(`/receipts/${resp.receiptId}`)
    } catch (err) {
      setSubmitError((err as Error).message || "Failed to create receipt")
    } finally {
      setSubmitting(false)
    }
  }

  // Live total preview from the rows. The form's "Total" input stays
  // a separate editable field (tax/discount can differ from the sum).
  const linesTotalCents = (): number => {
    let total = 0
    for (const r of rows) {
      const qty = parseFloat(r.quantity)
      const priceCents = Math.round(parseFloat(r.unitPriceDollars || "0") * 100)
      if (!isNaN(qty) && !isNaN(priceCents) && qty > 0 && priceCents >= 0) {
        total += Math.round(qty * priceCents)
      }
    }
    return total
  }

  // Form reads the lookups (suspending until ready). Closure
  // component so data reads stay inside the boundary.
  const Body = () => {
    const sortedMerchants = () =>
      [...(data()?.merchants ?? [])].sort((a, b) => a.name.localeCompare(b.name))
    const sortedItems = () => [...(data()?.items ?? [])].sort((a, b) => a.name.localeCompare(b.name))
    return (
      <Show
        when={(data()?.merchants.length ?? 0) > 0}
        fallback={
          <div class="empty-state">
            <h3>No merchants yet</h3>
            <p>Add a merchant on the Merchants page before creating a receipt.</p>
            <button onClick={() => navigate("/merchants")}>Go to Merchants</button>
          </div>
        }
      >
        <Show
          when={(data()?.items.length ?? 0) > 0}
          fallback={
            <div class="empty-state">
              <h3>No items yet</h3>
              <p>Add at least one item on the Items page before creating a receipt.</p>
              <button onClick={() => navigate("/items")}>Go to Items</button>
            </div>
          }
        >
          <form onSubmit={handleSubmit} class="manual-form">
            <div class="form-row">
              <div class="form-field">
                <label for="manual-merchant">Merchant</label>
                <select
                  id="manual-merchant"
                  onChange={(e) => setMerchantId(e.currentTarget.value)}
                  disabled={submitting()}
                  required
                >
                  <option value="" selected={merchantId() === ""}>
                    Pick a merchant…
                  </option>
                  <For each={sortedMerchants()}>
                    {(m) => (
                      <option value={m.merchantId} selected={merchantId() === m.merchantId}>
                        {m.name}
                      </option>
                    )}
                  </For>
                </select>
              </div>
              <div class="form-field">
                <label for="manual-date">Date & Time</label>
                <input
                  id="manual-date"
                  type="datetime-local"
                  value={date()}
                  onInput={(e) => setDate(e.currentTarget.value)}
                  disabled={submitting()}
                  required
                />
              </div>
              <div class="form-field">
                <label for="manual-total">Total ($)</label>
                <input
                  id="manual-total"
                  type="number"
                  step="0.01"
                  min="0"
                  value={totalDollars()}
                  onInput={(e) => setTotalDollars(e.currentTarget.value)}
                  disabled={submitting()}
                  required
                  placeholder="0.00"
                />
                <span class="muted total-hint">Items sum: {formatMoney(linesTotalCents())}</span>
              </div>
            </div>

            <h2>Items</h2>
            <div class="items-table-wrapper">
              <table class="responsive-table">
                <tr>
                  <th style="width: 50%">Item</th>
                  <th>Qty</th>
                  <th>Unit Price ($)</th>
                  <th />
                </tr>
                <For each={rows}>
                  {(row, i) => (
                    <tr>
                      <td data-label="Item">
                        <select
                          onChange={(e) => setRows(i(), { itemId: e.currentTarget.value })}
                          disabled={submitting()}
                          required
                        >
                          <option value="" selected={row.itemId === ""}>
                            Pick an item…
                          </option>
                          <For each={sortedItems()}>
                            {(it) => (
                              <option value={it.itemId} selected={row.itemId === it.itemId}>
                                {it.name}
                              </option>
                            )}
                          </For>
                        </select>
                      </td>
                      <td data-label="Qty">
                        <input
                          type="number"
                          step="0.001"
                          min="0"
                          value={row.quantity}
                          onInput={(e) => setRows(i(), { quantity: e.currentTarget.value })}
                          disabled={submitting()}
                          required
                          class="edit-input-num"
                        />
                      </td>
                      <td data-label="Unit Price">
                        <input
                          type="number"
                          step="0.01"
                          min="0"
                          value={row.unitPriceDollars}
                          onInput={(e) => setRows(i(), { unitPriceDollars: e.currentTarget.value })}
                          disabled={submitting()}
                          required
                          class="edit-input-num"
                          placeholder="0.00"
                        />
                      </td>
                      <td data-label="">
                        <button
                          type="button"
                          onClick={() => removeRow(i())}
                          disabled={submitting() || rows.length === 1}
                          class="btn-sm btn-danger"
                        >
                          Remove
                        </button>
                      </td>
                    </tr>
                  )}
                </For>
              </table>
            </div>

            <div class="form-row">
              <button type="button" onClick={addRow} disabled={submitting()} class="btn-secondary">
                + Add Item
              </button>
            </div>

            <Show when={submitError()}>
              <p class="error">{submitError()}</p>
            </Show>

            <div class="form-row form-actions">
              <button type="submit" disabled={submitting()} class="btn-primary">
                {submitting() ? "Creating…" : "Create Receipt"}
              </button>
              <button
                type="button"
                onClick={() => navigate("/receipts")}
                disabled={submitting()}
                class="btn-secondary"
              >
                Cancel
              </button>
            </div>
          </form>
        </Show>
      </Show>
    )
  }

  return (
    <div class="manual-receipt-page">
      <div class="page-header">
        <h1>Create Receipt Manually</h1>
        <button onClick={() => navigate("/receipts")} class="btn-secondary">
          Back
        </button>
      </div>

      <p class="muted">
        Use this when the photo + LLM flow didn't work, or for entering historic receipts. Pick items
        from the existing catalog. Need a new item or merchant? Add them on the{" "}
        <span class="muted">Items / Merchants</span> pages first.
      </p>

      <QueryBoundary pending={<div class="muted">Loading…</div>}>
        <Body />
      </QueryBoundary>
    </div>
  )
}
