import van from "vanjs-core"
import { api, navigate } from "../main"

const { div, h1, button, span, h3, p } = van.tags

interface Receipt {
  receiptId: number
  merchantId: number
  ownerId: number
  date: number
  photoUrl: string
  items: { itemId: number; quantity: number; unitPriceCents: number }[]
  totalCents: number
}

const ReceiptCard = (receipt: Receipt) => {
  const date = new Date(receipt.date * 1000)
  const dateStr = date.toLocaleDateString()

  return div({ class: "receipt-card card", onclick: () => navigate(`/receipts/${receipt.receiptId}`) },
    div({ class: "receipt-header" },
      h3(`Receipt #${receipt.receiptId}`),
      span({ class: "receipt-date" }, dateStr),
    ),
    div({ class: "receipt-body" },
      p(`${receipt.items.length} items`),
      p({ class: "receipt-total" }, `$${(receipt.totalCents / 100).toFixed(2)}`),
    ),
  )
}

const ReceiptsPage = () => {
  const receipts = van.state<Receipt[]>([])
  const loading = van.state(true)

  const loadReceipts = async () => {
    loading.val = true
    try {
      const data = await api.get("/receipts")
      receipts.val = data || []
    } catch (err) {
      console.error("Failed to load receipts:", err)
    }
    loading.val = false
  }

  loadReceipts()

  return div({ class: "receipts-page" },
    div({ class: "page-header" },
      h1("Receipts"),
      button({ onclick: () => navigate("/receipts/upload") }, "Upload Receipt"),
    ),
    div({ class: "receipts-list" },
      () => loading.val
        ? div("Loading...")
        : receipts.val.length === 0
          ? div("No receipts yet")
          : div({ class: "receipts-grid" },
              ...receipts.val.map(r => ReceiptCard(r)),
            ),
    ),
  )
}

export default ReceiptsPage
