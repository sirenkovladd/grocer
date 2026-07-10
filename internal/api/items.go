package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

func (r *Router) handleListItems(w http.ResponseWriter, req *http.Request) {
	items, err := r.store.ListItems()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (r *Router) handleGetItem(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item ID")
		return
	}

	item, err := r.store.GetItem(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type updateItemRequest struct {
	Name       *string  `json:"name,omitempty"`
	CategoryID *uint64  `json:"categoryId,omitempty"`
	Aliases    []string `json:"aliases,omitempty"`
}

func (r *Router) handleUpdateItem(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item ID")
		return
	}

	item, err := r.store.GetItem(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}

	var reqBody updateItemRequest
	if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if reqBody.Name != nil {
		if err := validateItemName(*reqBody.Name); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		item.Name = *reqBody.Name
	}
	if reqBody.CategoryID != nil {
		item.CategoryID = *reqBody.CategoryID
	}
	if reqBody.Aliases != nil {
		if err := validateAliases(reqBody.Aliases); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		item.Aliases = reqBody.Aliases
	}

	if err := r.store.UpdateItem(item); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleDeleteItem removes an item from the catalog. Refuses with
// 400 if any receipt still references the item — deleting it would
// leave those receipts pointing at a missing entity, which the
// enriched-receipt handlers would then render as "Unknown item".
// To proceed, the caller should first migrate the affected receipts
// to a different item (re-categorize, or use the merge tool planned
// as item #12).
func (r *Router) handleDeleteItem(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item ID")
		return
	}

	// Confirm the item exists — return 404 otherwise instead of a
	// silent no-op from the store.
	if _, err := r.store.GetItem(id); err != nil {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}

	// Check if any receipt still references this item. Batch-load
	// receipts once; iterate to find any with matching itemID.
	receipts, err := r.store.ListReceipts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	usedIn := 0
	for _, rcpt := range receipts {
		for _, ri := range rcpt.Items {
			if ri.ItemID == id {
				usedIn++
				break
			}
		}
	}
	if usedIn > 0 {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("cannot delete item: %d receipt(s) still reference it", usedIn))
		return
	}

	if err := r.store.DeleteItem(id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ItemWithStats extends domain.Item with purchase statistics
// computed by joining against all receipts. Used by the items
// list page to render the "last bought" column and to allow
// sorting by purchase count.
type ItemWithStats struct {
	ItemID          uint64   `json:"itemId,string"`
	Name            string   `json:"name"`
	CategoryID      uint64   `json:"categoryId,string"`
	MerchantID      uint64   `json:"merchantId,string"`
	Normalized      string   `json:"normalized"`
	Aliases         []string `json:"aliases,omitempty"`
	PurchaseCount   int      `json:"purchaseCount"`
	LastPurchasedAt int64    `json:"lastPurchasedAt"` // Unix seconds; 0 if never
}

// handleListItemsWithStats returns the same set as /api/items but
// joined with per-item purchase statistics. The join is O(items
// + receipts) and runs once per page load, so at family scale
// (handful of hundreds of items, thousands of receipts) the cost
// is negligible.
func (r *Router) handleListItemsWithStats(w http.ResponseWriter, req *http.Request) {
	items, err := r.store.ListItems()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	receipts, err := r.store.ListReceipts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Walk every receipt once, accumulating per-item stats.
	type stat struct{ count int; lastAt int64 }
	stats := make(map[uint64]stat, len(items))
	for _, r := range receipts {
		for _, ri := range r.Items {
			s := stats[ri.ItemID]
			s.count++
			if r.Date > s.lastAt {
				s.lastAt = r.Date
			}
			stats[ri.ItemID] = s
		}
	}

	result := make([]ItemWithStats, 0, len(items))
	for _, it := range items {
		s := stats[it.ItemID]
		result = append(result, ItemWithStats{
			ItemID:          it.ItemID,
			Name:            it.Name,
			CategoryID:      it.CategoryID,
			MerchantID:      it.MerchantID,
			Normalized:      it.Normalized,
			Aliases:         it.Aliases,
			PurchaseCount:   s.count,
			LastPurchasedAt: s.lastAt,
		})
	}

	writeJSON(w, http.StatusOK, result)
}

// mergeItemRequest is the body of POST /api/items/{id}/merge. The
// {id} in the path is the source; the body's `targetId` is the item
// to retarget references to.
type mergeItemRequest struct {
	TargetID uint64 `json:"targetId,string"`
}

// handleMergeItem consolidates two items: every receipt that
// references the source item (path id) is rewritten to reference
// the target item (body targetId), then the source item is
// deleted. Returns the number of receipt line items that were
// retargeted so the client can show a summary.
func (r *Router) handleMergeItem(w http.ResponseWriter, req *http.Request) {
	sourceStr := req.PathValue("id")
	sourceID, err := strconv.ParseUint(sourceStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid source item ID")
		return
	}

	var reqBody mergeItemRequest
	if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if reqBody.TargetID == sourceID {
		writeError(w, http.StatusBadRequest, "cannot merge an item into itself")
		return
	}
	if reqBody.TargetID == 0 {
		writeError(w, http.StatusBadRequest, "targetId is required")
		return
	}

	// Confirm both items exist — silent no-ops are worse than
	// explicit 404s for a destructive operation.
	if _, err := r.store.GetItem(sourceID); err != nil {
		writeError(w, http.StatusNotFound, "source item not found")
		return
	}
	if _, err := r.store.GetItem(reqBody.TargetID); err != nil {
		writeError(w, http.StatusNotFound, "target item not found")
		return
	}

	retargeted, err := r.store.MergeItem(sourceID, reqBody.TargetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "merge failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":     "merged",
		"retargeted": retargeted,
	})
}
