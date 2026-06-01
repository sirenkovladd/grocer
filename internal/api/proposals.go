package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"code.sirenko.ca/grocer/internal/domain"
)

type approveRequest struct {
	Choices map[int]string `json:"choices"`
}

func (r *Router) handleListProposals(w http.ResponseWriter, req *http.Request) {
	proposals, err := r.store.ListProposals()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Filter pending only
	var pending []*domain.Proposal
	for _, p := range proposals {
		if p.Status == "pending" {
			pending = append(pending, p)
		}
	}

	writeJSON(w, http.StatusOK, pending)
}

func (r *Router) handleGetProposal(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid proposal ID")
		return
	}

	proposal, err := r.store.GetProposal(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "proposal not found")
		return
	}

	writeJSON(w, http.StatusOK, proposal)
}

func (r *Router) handleApproveProposal(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid proposal ID")
		return
	}

	var reqBody approveRequest
	if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	receipt, err := r.parser.ApproveProposal(req.Context(), id, reqBody.Choices)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to approve proposal")
		return
	}

	writeJSON(w, http.StatusOK, receipt)
}
