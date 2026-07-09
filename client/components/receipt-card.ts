import van from "vanjs-core"
import { navigate } from "../main"
import { formatDate, formatMoney } from "../utils"

const { div, span } = van.tags

// Shared shape between the receipts list, home page recent-receipts
// section, and any future "list of receipts" view. Matches the Go
// EnrichedReceiptSummary DTO from internal/api/types.go.
//
// ID fields are `string` because the backend serializes uint64 as a
// JSON string (json:"...,string"). See ticket 04 decisions log.
export interface EnrichedReceiptSummary {
  receiptId: string
  merchantId: string
  merchantName: string
  ownerId: string
  ownerName: string // not displayed in card per UX overhaul plan
  date: number
  itemCount: number
  totalCents: number
  photoUrl?: string
}

// Click navigates to the receipt detail page. No `onClick` callback
// needed — this component is a leaf.
export const ReceiptCard = (r: EnrichedReceiptSummary) =>
  div(
    {
      class: "receipt-card card",
      onclick: () => navigate(`/receipts/${r.receiptId}`),
    },
    div({ class: "receipt-header" },
      div({ class: "receipt-merchant" }, r.merchantName),
      div({ class: "receipt-date muted" }, formatDate(r.date)),
    ),
    div({ class: "receipt-meta" },
      span(`${r.itemCount} ${r.itemCount === 1 ? "item" : "items"}`),
      span({ class: "money" }, formatMoney(r.totalCents)),
    ),
  )
