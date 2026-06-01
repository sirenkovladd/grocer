package api

import (
	"net/http"
	"strings"

	"code.sirenko.ca/grocer/internal/domain"
)

func (r *Router) handleSearchReceipts(w http.ResponseWriter, req *http.Request) {
	query := req.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "missing search query parameter 'q'")
		return
	}

	query = strings.ToLower(query)

	receipts, err := r.store.ListReceipts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Load all merchants for name matching
	merchants, err := r.store.ListMerchants()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	merchantMap := make(map[uint64]*domain.Merchant)
	for _, m := range merchants {
		merchantMap[m.MerchantID] = m
	}

	var results []*domain.Receipt
	for _, receipt := range receipts {
		// Search by merchant name
		if merchant, ok := merchantMap[receipt.MerchantID]; ok {
			if strings.Contains(strings.ToLower(merchant.Name), query) {
				results = append(results, receipt)
				continue
			}
		}

		// Search by receipt ID
		if strings.Contains(string(rune(receipt.ReceiptID)), query) {
			results = append(results, receipt)
		}
	}

	writeJSON(w, http.StatusOK, results)
}

func (r *Router) handleSearchItems(w http.ResponseWriter, req *http.Request) {
	query := req.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "missing search query parameter 'q'")
		return
	}

	query = strings.ToLower(query)

	items, err := r.store.ListItems()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var results []*domain.Item
	for _, item := range items {
		// Search by name
		if strings.Contains(strings.ToLower(item.Name), query) {
			results = append(results, item)
			continue
		}

		// Search by normalized name
		if strings.Contains(strings.ToLower(item.Normalized), query) {
			results = append(results, item)
			continue
		}

		// Search by aliases
		for _, alias := range item.Aliases {
			if strings.Contains(strings.ToLower(alias), query) {
				results = append(results, item)
				break
			}
		}
	}

	writeJSON(w, http.StatusOK, results)
}
