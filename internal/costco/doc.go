// Package costco imports Costco warehouse receipts into Grocer.
//
// Costco exposes order history through an authenticated GraphQL endpoint:
//
//	POST https://ecom-api.costco.com/ebusiness/order/v1/orders/graphql
//
// Two queries matter:
//
//  1. receiptsWithCounts(startDate, endDate, ...) — list view. Returns one
//     row per warehouse visit with transactionBarcode, date, warehouse,
//     total, and item count. Cheap: one call covers a date range.
//  2. receiptsWithCounts(barcode, documentType) — detail view. Returns the
//     full line-item breakdown for a single barcode.
//
// Authentication is a short-lived Bearer (costco-x-authorization, ~10 min)
// plus Kasada bot-challenge headers (x-kpsdk-*) that cannot be forged
// server-side. Automated server login is therefore fragile by design.
// The supported production flow is copy-paste:
//
//  1. In a logged-in Costco tab, open DevTools → Network.
//  2. Open Order History, copy the list-query response body.
//  3. Paste it into Grocer's Costco import page.
//  4. Click into each receipt (or "load all"), copy each detail-query
//     response body, paste them in. Grocer parses, matches against the
//     catalog, and creates proposals (review) or receipts (direct).
//
// An experimental direct-fetch client (client.go) is provided for the
// case where the user pastes a FRESH bearer token + cookies: the server
// will attempt the same GraphQL calls on their behalf. It works until
// the token expires or Kasada challenges — the paste flow always works,
// so the UI defaults to it.
package costco
