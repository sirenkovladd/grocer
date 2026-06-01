package api

import (
	"encoding/json"
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
