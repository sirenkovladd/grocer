package api

import (
	"encoding/json"
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
