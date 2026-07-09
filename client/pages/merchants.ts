import van from "vanjs-core"
import { api, navigate } from "../main"
import { formatDate, formatMoney } from "../utils"

const { div, h1, h2, table, tr, td, th, select, option, button, p } = van.tags

interface Item {
  itemId: number
  name: string
  categoryId: number
  merchantId: number
}

interface MerchantComparison {
  merchant: string
  lastPrice: number
  avgPrice: number
  count: number
}

const MerchantsPage = () => {
  const items = van.state<Item[]>([])
  const selectedItem = van.state<string>("")
  const comparison = van.state<MerchantComparison[]>([])
  const loading = van.state(true)

  const loadItems = async () => {
    loading.val = true
    try {
      const data = await api.get("/items")
      items.val = data || []
    } catch (err) {
      console.error("Failed to load items:", err)
    }
    loading.val = false
  }

  loadItems()

  const loadComparison = async (itemId: string) => {
    if (!itemId) {
      comparison.val = []
      return
    }

    try {
      const data = await api.get(`/analysis/items/${itemId}`)
      
      // Group by date and calculate stats
      // For now, we'll show the price history
      // In a real app, we'd have a dedicated endpoint for merchant comparison
      comparison.val = data || []
    } catch (err) {
      console.error("Failed to load comparison:", err)
    }
  }

  return div({ class: "merchants-page" },
    div({ class: "page-header" },
      h1("Merchant Comparison"),
    ),
    div({ class: "item-selector card" },
      h2("Select Item"),
      () => select({
        value: selectedItem,
        onchange: (e: Event) => {
          const value = (e.target as HTMLSelectElement).value
          selectedItem.val = value
          loadComparison(value)
        },
      },
        option({ value: "" }, "Choose an item..."),
        ...items.val.map(item =>
          option({ value: item.itemId.toString() }, item.name)
        ),
      ),
    ),
    () => comparison.val.length > 0
      ? div({ class: "comparison-results card" },
          h2("Price History"),
          div({ class: "items-table-wrapper" },
            table({ class: "responsive-table" },
              tr(
                th("Date"),
                th({ class: "money" }, "Price"),
              ),
              ...comparison.val.map((c: any) => {
                // Analysis endpoint returns date as "2006-01-02" and
                // price in dollars (same wart as item-detail; see
                // client/pages/item-detail.ts for the rationale).
                // Parse date as local midnight so the displayed date
                // matches the user's intent.
                const unixSecs = Math.floor(new Date(c.date + "T00:00:00").getTime() / 1000)
                return tr(
                  td({ "data-label": "Date" }, formatDate(unixSecs)),
                  td({ "data-label": "Price", class: "money" }, formatMoney(c.price * 100)),
                )
              }),
            ),
          ),
        )
      : selectedItem.val
        ? p({ class: "no-data" }, "No data available for this item")
        : p({ class: "no-data" }, "Select an item to view price history"),
  )
}

export default MerchantsPage
