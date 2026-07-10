import van from "vanjs-core"
import { api, idStr, navigate } from "../main"
import { formatMoney } from "../utils"

const { div, h1, h2, h3, p, button, input, select, option, label, span } = van.tags

// Manual receipt entry form. Used when the LLM can't parse a photo
// or for entering historic receipts. Skips the photo + LLM + propose
// pipeline entirely and commits a receipt directly via
// POST /api/receipts/manual.
//
// Each item row is bound to a slot in a parallel array of edit
// state objects. The slot is keyed by index; adding/removing rows
// changes the array length. This is the simplest reactive model
// that handles both the inputs and the per-row Remove button.

interface Merchant {
  merchantId: string
  name: string
}

interface CatalogItem {
  itemId: string
  name: string
}

interface ItemRow {
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

const ManualReceiptPage = () => {
  const merchants = van.state<Merchant[]>([])
  const items = van.state<CatalogItem[]>([])
  const loadingLookups = van.state(true)
  const lookupError = van.state<string | null>(null)

  // Form state
  const merchantId = van.state("")
  const date = van.state(unixToDateTimeInput(Math.floor(Date.now() / 1000)))
  const totalDollars = van.state("")
  const rows = van.state<ItemRow[]>([
    { itemId: "", quantity: "1", unitPriceDollars: "" },
  ])
  const submitting = van.state(false)
  const submitError = van.state<string | null>(null)

  const loadLookups = async () => {
    loadingLookups.val = true
    lookupError.val = null
    try {
      const [m, i] = await Promise.all([
        api.get("/merchants"),
        api.get("/items"),
      ])
      merchants.val = Array.isArray(m) ? m : []
      items.val = Array.isArray(i) ? i : []
    } catch (err) {
      lookupError.val = (err as Error).message || "Failed to load merchants and items"
    } finally {
      loadingLookups.val = false
    }
  }

  loadLookups()

  const addRow = () => {
    rows.val = [...rows.val, { itemId: "", quantity: "1", unitPriceDollars: "" }]
  }

  const removeRow = (index: number) => {
    if (rows.val.length === 1) return
    rows.val = rows.val.filter((_, i) => i !== index)
  }

  const updateRow = (index: number, patch: Partial<ItemRow>) => {
    rows.val = rows.val.map((r, i) => i === index ? { ...r, ...patch } : r)
  }

  const handleSubmit = async (e: Event) => {
    e.preventDefault()
    submitError.val = null

    if (!merchantId.val) {
      submitError.val = "Pick a merchant"
      return
    }
    const dateUnix = dateTimeInputToUnix(date.val)
    if (isNaN(dateUnix)) {
      submitError.val = "Pick a valid date"
      return
    }
    const totalCents = Math.round(parseFloat(totalDollars.val || "0") * 100)
    if (isNaN(totalCents) || totalCents < 0) {
      submitError.val = "Total must be a non-negative number"
      return
    }
    const cleanRows: { itemId: string; quantity: number; unitPriceCents: number }[] = []
    for (let i = 0; i < rows.val.length; i++) {
      const r = rows.val[i]
      if (!r.itemId) {
        submitError.val = `Row ${i + 1}: pick an item`
        return
      }
      const qty = parseFloat(r.quantity)
      const priceCents = Math.round(parseFloat(r.unitPriceDollars || "0") * 100)
      if (isNaN(qty) || qty <= 0) {
        submitError.val = `Row ${i + 1}: quantity must be a positive number`
        return
      }
      if (isNaN(priceCents) || priceCents < 0) {
        submitError.val = `Row ${i + 1}: unit price must be a non-negative number`
        return
      }
      cleanRows.push({ itemId: idStr(r.itemId), quantity: qty, unitPriceCents: priceCents })
    }

    submitting.val = true
    try {
      const data = await api.post("/receipts/manual", {
        merchantId: idStr(merchantId.val),
        date: dateUnix,
        totalCents,
        items: cleanRows,
      }) as { receiptId: string }
      navigate(`/receipts/${data.receiptId}`)
    } catch (err) {
      submitError.val = (err as Error).message || "Failed to create receipt"
    } finally {
      submitting.val = false
    }
  }

  // Live total preview from the per-line rows. Recomputes when any
  // row's qty/price changes. The form's "Total" input is a separate
  // user-editable field (it should equal the sum for most receipts,
  // but tax/discount can differ).
  const linesTotalCents = (): number => {
    let total = 0
    for (const r of rows.val) {
      const qty = parseFloat(r.quantity)
      const priceCents = Math.round(parseFloat(r.unitPriceDollars || "0") * 100)
      if (!isNaN(qty) && !isNaN(priceCents) && qty > 0 && priceCents >= 0) {
        total += Math.round(qty * priceCents)
      }
    }
    return total
  }

  return div({ class: "manual-receipt-page" },
    div({ class: "page-header" },
      h1("Create Receipt Manually"),
      button({ onclick: () => navigate("/receipts"), class: "btn-secondary" }, "Back"),
    ),

    p({ class: "muted" },
      "Use this when the photo + LLM flow didn't work, or for entering historic receipts. ",
      "Pick items from the existing catalog. ",
      "Need a new item or merchant? Add them on the ", span({ class: "muted" }, "Items / Merchants"), " pages first.",
    ),

    () => {
      if (lookupError.val) {
        return div({ class: "empty-state" },
          h3("Couldn't load form data"),
          p(lookupError.val),
          button({ onclick: loadLookups }, "Try Again"),
        )
      }
      if (loadingLookups.val) return div({ class: "muted" }, "Loading…")
      if (merchants.val.length === 0) {
        return div({ class: "empty-state" },
          h3("No merchants yet"),
          p("Add a merchant on the Merchants page before creating a receipt."),
          button({ onclick: () => navigate("/merchants") }, "Go to Merchants"),
        )
      }
      if (items.val.length === 0) {
        return div({ class: "empty-state" },
          h3("No items yet"),
          p("Add at least one item on the Items page before creating a receipt."),
          button({ onclick: () => navigate("/items") }, "Go to Items"),
        )
      }
      const sortedMerchants = [...merchants.val].sort((a, b) => a.name.localeCompare(b.name))
      const sortedItems = [...items.val].sort((a, b) => a.name.localeCompare(b.name))

      return form({ onsubmit: handleSubmit, class: "manual-form" },
        div({ class: "form-row" },
          div({ class: "form-field" },
            label({ for: "manual-merchant" }, "Merchant"),
            select({
              id: "manual-merchant",
              value: merchantId,
              onchange: (e: Event) => { merchantId.val = (e.target as HTMLSelectElement).value },
              disabled: submitting,
              required: true,
            },
              option({ value: "" }, "Pick a merchant…"),
              ...sortedMerchants.map(m =>
                option({ value: m.merchantId }, m.name),
              ),
            ),
          ),
          div({ class: "form-field" },
            label({ for: "manual-date" }, "Date & Time"),
            input({
              id: "manual-date",
              type: "datetime-local",
              value: date,
              oninput: (e: Event) => { date.val = (e.target as HTMLInputElement).value },
              disabled: submitting,
              required: true,
            }),
          ),
          div({ class: "form-field" },
            label({ for: "manual-total" }, "Total ($)"),
            input({
              id: "manual-total",
              type: "number",
              step: "0.01",
              min: "0",
              value: totalDollars,
              oninput: (e: Event) => { totalDollars.val = (e.target as HTMLInputElement).value },
              disabled: submitting,
              required: true,
              placeholder: "0.00",
            }),
            () => span({ class: "muted total-hint" },
              `Items sum: ${formatMoney(linesTotalCents())}`,
            ),
          ),
        ),

        h2("Items"),
        div({ class: "items-table-wrapper" },
          table({ class: "responsive-table" },
            tr(
              th({ style: "width: 50%" }, "Item"),
              th("Qty"),
              th("Unit Price ($)"),
              th(""),
            ),
            ...rows.val.map((row, index) =>
              tr(
                td({ "data-label": "Item" },
                  select({
                    value: () => row.itemId,
                    onchange: (e: Event) => updateRow(index, { itemId: (e.target as HTMLSelectElement).value }),
                    disabled: submitting,
                    required: true,
                  },
                    option({ value: "" }, "Pick an item…"),
                    ...sortedItems.map(it =>
                      option({ value: it.itemId }, it.name),
                    ),
                  ),
                ),
                td({ "data-label": "Qty" },
                  input({
                    type: "number",
                    step: "0.001",
                    min: "0",
                    value: () => row.quantity,
                    oninput: (e: Event) => updateRow(index, { quantity: (e.target as HTMLInputElement).value }),
                    disabled: submitting,
                    required: true,
                    class: "edit-input-num",
                  }),
                ),
                td({ "data-label": "Unit Price" },
                  input({
                    type: "number",
                    step: "0.01",
                    min: "0",
                    value: () => row.unitPriceDollars,
                    oninput: (e: Event) => updateRow(index, { unitPriceDollars: (e.target as HTMLInputElement).value }),
                    disabled: submitting,
                    required: true,
                    class: "edit-input-num",
                    placeholder: "0.00",
                  }),
                ),
                td({ "data-label": "" },
                  button({
                    type: "button",
                    onclick: () => removeRow(index),
                    disabled: submitting || rows.val.length === 1,
                    class: "btn-sm btn-danger",
                  }, "Remove"),
                ),
              ),
            ),
          ),
        ),

        div({ class: "form-row" },
          button({
            type: "button",
            onclick: addRow,
            disabled: submitting,
            class: "btn-secondary",
          }, "+ Add Item"),
        ),

        () => submitError.val
          ? p({ class: "error" }, submitError.val)
          : "",

        div({ class: "form-row form-actions" },
          button({
            type: "submit",
            disabled: submitting,
            class: "btn-primary",
          }, () => submitting.val ? "Creating…" : "Create Receipt"),
          button({
            type: "button",
            onclick: () => navigate("/receipts"),
            disabled: submitting,
            class: "btn-secondary",
          }, "Cancel"),
        ),
      )
    },
  )
}

const form = (props: any, ...children: any[]) => {
  const el = document.createElement("form")
  for (const [k, v] of Object.entries(props)) {
    if (k.startsWith("on") && typeof v === "function") {
      el.addEventListener(k.slice(2).toLowerCase(), v)
    } else {
      el.setAttribute(k, v)
    }
  }
  for (const c of children) el.appendChild(c)
  return el
}

export default ManualReceiptPage
