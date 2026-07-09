import van from "vanjs-core"
import { api, navigate } from "../main"
import { formatDate, formatMoney, formatQuantity } from "../utils"
import { fetchPhotoUrl, revokePhotoUrl } from "../photos"

const { div, h1, h2, a, span, table, tr, td, th, button, p, img } = van.tags

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
    span({ class: "current" }, merchantName || `Receipt #${receiptId.slice(0, 8)}…`),
  )

const ReceiptDetailPage = () => {
  const receipt = van.state<EnrichedReceipt | null>(null)
  const photoSrc = van.state<string>("")
  const loading = van.state(true)
  const error = van.state<string | null>(null)
  let currentReceiptId: string | null = null

  const id = window.location.hash.split("/").pop() || ""

  const load = async () => {
    if (!id) return
    loading.val = true
    error.val = null
    try {
      const data = await api.get(`/receipts/${id}/enriched`)
      receipt.val = data

      // Revoke previous photo URL (if any) before fetching a new one to
      // avoid blob URL leaks. fetchPhotoUrl caches by ID, so the new
      // URL reuses the same blob on the same receipt.
      if (currentReceiptId && currentReceiptId !== data.receiptId) {
        revokePhotoUrl(currentReceiptId)
        photoSrc.val = ""
      }
      currentReceiptId = data.receiptId

      if (data?.photoUrl) {
        try {
          photoSrc.val = await fetchPhotoUrl(data.receiptId)
        } catch (err) {
          console.warn("Failed to load photo:", err)
          photoSrc.val = ""
        }
      }
    } catch (err) {
      console.error("Failed to load receipt:", err)
      error.val = (err as Error).message || "Failed to load receipt"
    } finally {
      loading.val = false
    }
  }

  load()

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

      return div(
        Breadcrumb(r.merchantName, r.receiptId),

        div({ class: "page-header" },
          div(
            h1(r.merchantName || `Receipt #${r.receiptId.slice(0, 8)}…`),
            div({ class: "page-header-meta" },
              span({ class: "muted" }, formatDate(r.date)),
              span({ class: "money" }, formatMoney(r.totalCents)),
            ),
          ),
          button({ onclick: () => navigate("/receipts") }, "Back"),
        ),

        // Photo
        () => photoSrc.val
          ? div({ class: "receipt-photo" }, img({ src: photoSrc.val, alt: "Receipt" }))
          : "",

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
                ...r.items.map(item => {
                  // Weighted display: a weighted item has a unit price
                  // and quantity that imply a different line total
                  // (e.g. "$1.96/kg × 0.5kg = $0.98"). Show the unit
                  // price as a subtitle under the quantity.
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
      )
    },
  )
}

export default ReceiptDetailPage
