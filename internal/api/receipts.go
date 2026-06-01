package api

import (
	"io"
	"net/http"
	"strconv"
	"time"

	"code.sirenko.ca/grocer/internal/domain"
)

func (r *Router) handleListReceipts(w http.ResponseWriter, req *http.Request) {
	from := req.URL.Query().Get("from")
	to := req.URL.Query().Get("to")
	owner := req.URL.Query().Get("owner")
	category := req.URL.Query().Get("category")

	receipts, err := r.store.ListReceipts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Apply filters
	filtered := receipts
	if from != "" {
		fromDate, err := time.Parse("2006-01-02", from)
		if err == nil {
			var result []*domain.Receipt
			for _, receipt := range filtered {
				if time.Unix(receipt.Date, 0).After(fromDate) || time.Unix(receipt.Date, 0).Equal(fromDate) {
					result = append(result, receipt)
				}
			}
			filtered = result
		}
	}

	if to != "" {
		toDate, err := time.Parse("2006-01-02", to)
		if err == nil {
			var result []*domain.Receipt
			for _, receipt := range filtered {
				if time.Unix(receipt.Date, 0).Before(toDate) || time.Unix(receipt.Date, 0).Equal(toDate) {
					result = append(result, receipt)
				}
			}
			filtered = result
		}
	}

	if owner != "" {
		ownerID, err := strconv.ParseUint(owner, 10, 64)
		if err == nil {
			var result []*domain.Receipt
			for _, receipt := range filtered {
				if receipt.OwnerID == ownerID {
					result = append(result, receipt)
				}
			}
			filtered = result
		}
	}

	if category != "" {
		categoryID, err := strconv.ParseUint(category, 10, 64)
		if err == nil {
			var result []*domain.Receipt
			for _, receipt := range filtered {
				for _, item := range receipt.Items {
					itemObj, err := r.store.GetItem(item.ItemID)
					if err == nil && itemObj.CategoryID == categoryID {
						result = append(result, receipt)
						break
					}
				}
			}
			filtered = result
		}
	}

	writeJSON(w, http.StatusOK, filtered)
}

func (r *Router) handleGetReceipt(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid receipt ID")
		return
	}

	receipt, err := r.store.GetReceipt(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "receipt not found")
		return
	}

	writeJSON(w, http.StatusOK, receipt)
}

func (r *Router) handleUploadReceipt(w http.ResponseWriter, req *http.Request) {
	userID := r.getUserID(req)

	if err := req.ParseMultipartForm(10 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "file too large")
		return
	}

	file, _, err := req.FormFile("photo")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing photo")
		return
	}
	defer file.Close()

	photoData, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read file")
		return
	}

	proposal, err := r.parser.ParseReceipt(req.Context(), photoData, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to parse receipt")
		return
	}

	// Save photo if photo store is configured
	if r.photoStore != nil {
		photoURL, err := r.photoStore.Save(req.Context(), proposal.ProposalID, photoData)
		if err == nil {
			proposal.PhotoURL = photoURL
			r.store.UpdateProposal(proposal)
		}
	}

	writeJSON(w, http.StatusOK, proposal)
}
