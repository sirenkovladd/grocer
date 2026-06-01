import van from "vanjs-core"
import { api, navigate } from "../main"

const { div, h1, h2, table, tr, td, th, button, p } = van.tags

interface Receipt {
  receiptId: number
  merchantId: number
  ownerId: number
  date: number
  photoUrl: string
  items: { itemId: number; quantity: number; unitPriceCents: number }[]
  totalCents: number
}

const ReceiptDetailPage = () => {
  const receipt = van.state<Receipt | null>(null)
  const loading = van.state(true)

  const loadReceipt = async () => {
    const id = window.location.hash.split("/").pop()
    if (!id) return

    loading.val = true
    try {
      const data = await api.get(`/receipts/${id}`)
      receipt.val = data
    } catch (err) {
      console.error("Failed to load receipt:", err)
    }
    loading.val = false
  }

  loadReceipt()

  return div({ class: "receipt-detail-page" },
    () => loading.val
      ? div("Loading...")
      : !receipt.val
        ? div("Receipt not found")
        : div(
            div({ class: "page-header" },
              h1(`Receipt #${receipt.val.receiptId}`),
              button({ onclick: () => navigate("/receipts") }, "Back"),
            ),
            div({ class: "receipt-info card" },
              p(`Date: ${new Date(receipt.val.date * 1000).toLocaleDateString()}`),
              p(`Total: $${(receipt.val.totalCents / 100).toFixed(2)}`),
            ),
            h2("Items"),
            table({ class: "items-table" },
              tr(
                th("Item"),
                th("Quantity"),
                th("Unit Price"),
                th("Total"),
              ),
              ...receipt.val.items.map(item =>
                tr(
                  td(`Item #${item.itemId}`),
                  td(item.quantity.toString()),
                  td(`$${(item.unitPriceCents / 100).toFixed(2)}`),
                  td(`$${(item.quantity * item.unitPriceCents / 100).toFixed(2)}`),
                )
              ),
            ),
          ),
  )
}

export default ReceiptDetailPage
