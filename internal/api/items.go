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
