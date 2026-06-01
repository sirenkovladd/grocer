package api

import (
	"net/http"
	"strconv"
	"strings"
)

func (r *Router) handleGetPhoto(w http.ResponseWriter, req *http.Request) {
	// GET /api/photos/{receiptId}
	idStr := req.PathValue("id")
	if idStr == "" {
		// Try to extract from URL path
		parts := strings.Split(req.URL.Path, "/")
		if len(parts) >= 4 {
			idStr = parts[3]
		}
	}

	receiptID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid receipt ID")
		return
	}

	// Get receipt to find photo URL
	receipt, err := r.store.GetReceipt(receiptID)
	if err != nil {
		writeError(w, http.StatusNotFound, "receipt not found")
		return
	}

	if receipt.PhotoURL == "" {
		writeError(w, http.StatusNotFound, "no photo for this receipt")
		return
	}

	// Try local cache first
	var data []byte
	if r.photoCache != nil {
		data, err = r.photoCache.Get(req.Context(), receipt.PhotoURL)
	}

	// If cache miss, get from GCloud
	if data == nil && r.photoStore != nil {
		data, err = r.photoStore.Get(req.Context(), receipt.PhotoURL)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get photo")
			return
		}

		// Cache locally
		if r.photoCache != nil {
			r.photoCache.Set(req.Context(), receipt.PhotoURL, data)
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
