import van from "vanjs-core"
import { navigate } from "../main"
import { formatDate, formatMoney } from "../utils"
import { fetchPhotoUrl } from "../photos"

const { div, span, img } = van.tags

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

// ReceiptCard renders one summary as a clickable card with a small
// receipt photo thumbnail (when available). The photo is fetched
// via the size=thumb variant of /api/photos/{id}, which is server-
// resized to ~200px on the longest edge.
//
// The image is loaded lazily after the card mounts. If the fetch
// fails (no photo, network error, etc.) we just don't render the
// <img> — the card text remains usable.
export const ReceiptCard = (r: EnrichedReceiptSummary) => {
  const thumbUrl = van.state<string>("")

  // Fire-and-forget thumbnail load. We don't await — the card should
  // appear immediately with text, and the photo fades in when ready.
  // (No fade yet; the page-header work for that is a polish item.)
  if (r.photoUrl) {
    fetchPhotoUrl(r.receiptId, "thumb")
      .then(url => { thumbUrl.val = url })
      .catch(err => {
        // Silent: missing/broken photo is normal for older receipts.
        console.debug("Receipt thumb load failed:", err)
      })
  }

  return div(
    {
      class: "receipt-card card",
      onclick: () => navigate(`/receipts/${r.receiptId}`),
    },
    () => thumbUrl.val
      ? div({ class: "receipt-thumb" },
          img({ src: thumbUrl.val, alt: "", loading: "lazy" }),
        )
      : "",
    div({ class: "receipt-header" },
      div({ class: "receipt-merchant" }, r.merchantName),
      div({ class: "receipt-date muted" }, formatDate(r.date)),
    ),
    div({ class: "receipt-meta" },
      span(`${r.itemCount} ${r.itemCount === 1 ? "item" : "items"}`),
      span({ class: "money" }, formatMoney(r.totalCents)),
    ),
  )
}
