package api

import (
	"net/http"
	"strconv"
	"strings"
)

func (r *Router) handleGetPhoto(w http.ResponseWriter, req *http.Request) {
	// GET /api/photos/{id} - works for both receipts and proposals
	idStr := req.PathValue("id")
	if idStr == "" {
		// Try to extract from URL path
		parts := strings.Split(req.URL.Path, "/")
		if len(parts) >= 4 {
			idStr = parts[3]
		}
	}

	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid ID")
		return
	}

	// Try receipt first, then proposal
	var photoURL string
	receipt, err := r.store.GetReceipt(id)
	if err == nil {
		photoURL = receipt.PhotoURL
	} else {
		proposal, err := r.store.GetProposal(id)
		if err == nil {
			photoURL = proposal.PhotoURL
		}
	}

	if photoURL == "" {
		writeError(w, http.StatusNotFound, "no photo found")
		return
	}

	// Try local cache first
	var data []byte
	if r.photoCache != nil {
		data, err = r.photoCache.Get(req.Context(), photoURL)
	}

	// If cache miss, get from GCloud
	if data == nil && r.photoStore != nil {
		data, err = r.photoStore.Get(req.Context(), photoURL)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get photo")
			return
		}

		// Cache locally
		if r.photoCache != nil {
			r.photoCache.Set(req.Context(), photoURL, data)
		}
	}

	if data == nil {
		writeError(w, http.StatusNotFound, "photo not found")
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(data)
}
