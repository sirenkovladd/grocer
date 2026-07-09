package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"code.sirenko.ca/grocer/internal/domain"
)

func (r *Router) handleListMerchants(w http.ResponseWriter, req *http.Request) {
	merchants, err := r.store.ListMerchants()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, merchants)
}

type createMerchantRequest struct {
	Name string `json:"name"`
}

func (r *Router) handleCreateMerchant(w http.ResponseWriter, req *http.Request) {
	var reqBody createMerchantRequest
	if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Input validation
	if err := validateMerchantName(reqBody.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	merchant := &domain.Merchant{
		MerchantID: r.store.MerchantID.Gen(),
		Name:       reqBody.Name,
	}

	if err := r.store.CreateMerchant(merchant); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, merchant)
}

type updateMerchantRequest struct {
	Name *string `json:"name,omitempty"`
}

func (r *Router) handleUpdateMerchant(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid merchant ID")
		return
	}

	merchant, err := r.store.GetMerchant(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "merchant not found")
		return
	}

	var reqBody updateMerchantRequest
	if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if reqBody.Name != nil {
		if err := validateMerchantName(*reqBody.Name); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		merchant.Name = *reqBody.Name
	}

	if err := r.store.UpdateMerchant(merchant); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, merchant)
}

// handleDeleteMerchant removes a merchant. Refuses with 400 if any
// receipt still references the merchant — otherwise the enriched
// receipt handler would render "Unknown merchant" for those rows.
// To proceed, the caller should first merge it into another
// merchant (the merge tool on the Merchants management page).
func (r *Router) handleDeleteMerchant(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid merchant ID")
		return
	}

	if _, err := r.store.GetMerchant(id); err != nil {
		writeError(w, http.StatusNotFound, "merchant not found")
		return
	}

	// Refuse if any receipt still references this merchant.
	receipts, err := r.store.ListReceipts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	usedIn := 0
	for _, rcpt := range receipts {
		if rcpt.MerchantID == id {
			usedIn++
		}
	}
	if usedIn > 0 {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("cannot delete merchant: %d receipt(s) still reference it", usedIn))
		return
	}

	if err := r.store.DeleteMerchant(id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// mergeMerchantRequest — {id} is the source, body's targetId is the
// merchant to retarget references to.
type mergeMerchantRequest struct {
	TargetID uint64 `json:"targetId"`
}

// handleMergeMerchant consolidates two merchants: every receipt
// that references the source is rewritten to reference the target,
// then the source is deleted. Mirrors the item-merge endpoint.
func (r *Router) handleMergeMerchant(w http.ResponseWriter, req *http.Request) {
	sourceStr := req.PathValue("id")
	sourceID, err := strconv.ParseUint(sourceStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid source merchant ID")
		return
	}

	var reqBody mergeMerchantRequest
	if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if reqBody.TargetID == sourceID {
		writeError(w, http.StatusBadRequest, "cannot merge a merchant into itself")
		return
	}
	if reqBody.TargetID == 0 {
		writeError(w, http.StatusBadRequest, "targetId is required")
		return
	}

	if _, err := r.store.GetMerchant(sourceID); err != nil {
		writeError(w, http.StatusNotFound, "source merchant not found")
		return
	}
	if _, err := r.store.GetMerchant(reqBody.TargetID); err != nil {
		writeError(w, http.StatusNotFound, "target merchant not found")
		return
	}

	retargeted, err := r.store.MergeMerchant(sourceID, reqBody.TargetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "merge failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":     "merged",
		"retargeted": retargeted,
	})
}
