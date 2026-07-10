// Package api — transport-layer DTOs.
//
// This file defines the JSON shapes returned by the public HTTP API. DTOs
// are intentionally separated from internal/domain types so the wire
// format can evolve independently of the storage model — for example,
// by embedding lookup names for client convenience (see the enriched
// receipt DTOs below).
package api

import (
	"encoding/json"
	"strconv"
)

// FlexibleID is a uint64 that JSON-decodes from either a quoted string
// ("123") or a bare number (123). It is used in request DTOs to accept
// both forms from clients.
//
// Rationale: Go's `,string` struct tag is asymmetric — it makes
// encoding emit a string, but only accepts strings when decoding. Our
// response DTOs use `,string` on uint64 ID fields to preserve
// precision through JavaScript's 53-bit safe-integer limit (the IDs
// are timestamp-based and exceed 2^53), so round-tripping the same
// value back as a request body must accept strings too. Accepting
// bare numbers in addition is a small convenience for hand-crafted
// requests that haven't been string-quoted.
type FlexibleID uint64

// Uint64 returns the underlying uint64 value. Use this at the boundary
// where the request DTO is consumed (e.g. when passing the ID to the
// store) so call sites don't need to know about the wrapper type.
func (f FlexibleID) Uint64() uint64 { return uint64(f) }

func (f *FlexibleID) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return err
		}
		*f = FlexibleID(n)
		return nil
	}
	n, err := strconv.ParseUint(string(data), 10, 64)
	if err != nil {
		return err
	}
	*f = FlexibleID(n)
	return nil
}


// Fallback strings used by enriched endpoints when a referenced entity
// (merchant, owner, category, item) cannot be found. Centralized here so
// the list and detail handlers in receipts.go stay in sync.
const (
	// UnknownMerchant is used when a receipt references a merchant ID
	// that no longer exists in the store (deleted or never created).
	UnknownMerchant = "Unknown merchant"

	// UnknownOwner is used when a receipt's owner ID no longer maps to
	// a user. "Unknown" (no suffix) is intentional — the field is
	// typically rendered as a person's display name, where "Unknown
	// owner" reads awkwardly.
	UnknownOwner = "Unknown"

	// UnknownCategory is used when a receipt item references a category
	// ID that no longer exists in the store.
	UnknownCategory = "Uncategorized"

	// UnknownItem is used when a receipt references an item ID that no
	// longer exists in the store. In practice items are never deleted
	// (they are renamed/merged instead), so this is a defensive
	// fallback for data drift between the receipt and the catalog.
	UnknownItem = "Unknown item"
)

// EnrichedReceiptSummary is the list-view projection of a receipt,
// returned by GET /api/receipts/enriched.
//
// It embeds the merchant and owner names so the client does not need a
// second round trip to render a row. IDs are still present so the client
// can navigate (e.g. to the detail page).
//
// IMPORTANT: The name fields are a *live join* — they reflect the current
// name of the referenced entity, not the name at the time the receipt was
// recorded. If a merchant is renamed, all receipts show the new name.
// This is the intended behavior for now (see UX overhaul spec, risk #9);
// if historical name preservation is ever needed, snapshot the name into
// the receipt record (schema change).
type EnrichedReceiptSummary struct {
	ReceiptID    uint64 `json:"receiptId,string"`
	MerchantID   uint64 `json:"merchantId,string"`
	MerchantName string `json:"merchantName"`
	OwnerID      uint64 `json:"ownerId,string"`
	OwnerName    string `json:"ownerName"`
	Date         int64  `json:"date"`
	ItemCount    int    `json:"itemCount"`
	TotalCents   int64  `json:"totalCents"`
	PhotoURL     string `json:"photoUrl,omitempty"`
}

// EnrichedReceiptItem is one line item on a receipt with its item name
// and category name embedded.
//
// TotalPriceCents is computed server-side as
// int64(math.Round(Quantity * UnitPriceCents)) so the client and server
// agree on the line total without re-implementing the formula. The
// rounding is important: float-truncation would round 166.5¢ down to
// 166¢ for a half-unit at 333¢ each.
type EnrichedReceiptItem struct {
	ItemID          uint64  `json:"itemId,string"`
	Name            string  `json:"name"`
	CategoryID      uint64  `json:"categoryId,string"`
	CategoryName    string  `json:"categoryName"`
	Quantity        float64 `json:"quantity"`
	UnitPriceCents  int64   `json:"unitPriceCents"`
	TotalPriceCents int64   `json:"totalPriceCents"`
}

// EnrichedReceipt is the detail-view projection of a receipt, returned
// by GET /api/receipts/{id}/enriched.
//
// It includes the full item list with names and category names embedded.
// Same live-join semantics as EnrichedReceiptSummary: name fields reflect
// the current state of the referenced entities.
type EnrichedReceipt struct {
	ReceiptID    uint64                `json:"receiptId,string"`
	MerchantID   uint64                `json:"merchantId,string"`
	MerchantName string                `json:"merchantName"`
	OwnerID      uint64                `json:"ownerId,string"`
	OwnerName    string                `json:"ownerName"`
	Date         int64                 `json:"date"`
	PhotoURL     string                `json:"photoUrl,omitempty"`
	Items        []EnrichedReceiptItem `json:"items"`
	TotalCents   int64                 `json:"totalCents"`
}
