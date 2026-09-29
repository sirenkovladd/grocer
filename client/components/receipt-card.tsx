import { createEffect, createSignal, Show } from "solid-js"
import { navigate } from "../lib/api.ts"
import { formatDateTime, formatMoney } from "../utils.ts"
import { fetchPhotoUrl } from "../photos.ts"

// Shared shape between the receipts list, home page recent-receipts
// section, and any future "list of receipts" view. Matches the Go
// EnrichedReceiptSummary DTO from internal/api/types.go.
//
// ID fields are `string` because the backend serializes uint64 as a
// JSON string (json:"...,string").
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
// The image loads after the card mounts. If the fetch fails (no
// photo, network error, etc.) we just don't render the <img> — the
// card text remains usable.
//
// Takes the summary as its (single) props object so call sites
// spread: <ReceiptCard {...receipt} />
export default function ReceiptCard(r: EnrichedReceiptSummary) {
  const [thumbUrl, setThumbUrl] = createSignal("")

  // Fire-and-forget thumbnail load. The card renders immediately
  // with text; the photo appears when ready.
  createEffect(() => {
    if (!r.photoUrl) return
    fetchPhotoUrl(r.receiptId, "thumb")
      .then((url) => setThumbUrl(url))
      .catch((err) => {
        // Silent: missing/broken photo is normal for older receipts.
        console.debug("Receipt thumb load failed:", err)
      })
  })

  return (
    <div class="receipt-card card" onClick={() => navigate(`/receipts/${r.receiptId}`)}>
      {/* Horizontal row layout: small thumb on the left, text on the
          right. Receipts are always vertical, so the thumb uses
          object-fit: contain (not cover) to show the full receipt. */}
      <Show
        when={thumbUrl()}
        fallback={<div class="receipt-thumb receipt-thumb-empty" />}
      >
        <div class="receipt-thumb">
          <img src={thumbUrl()} alt="" loading="lazy" />
        </div>
      </Show>
      <div class="receipt-card-body">
        <div class="receipt-merchant">{r.merchantName}</div>
        <div class="receipt-meta">
          <span class="muted">{formatDateTime(r.date)}</span>
          <span class="receipt-card-sep">·</span>
          <span>{`${r.itemCount} ${r.itemCount === 1 ? "item" : "items"}`}</span>
          <span class="receipt-card-total money">{formatMoney(r.totalCents)}</span>
        </div>
      </div>
    </div>
  )
}
