package api

import (
	"encoding/json"
	"fmt"
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

	// Filter out approved proposals (they became receipts)
	var active []*domain.Proposal
	for _, p := range proposals {
		if p.Status != "approved" {
			active = append(active, p)
		}
	}

	writeJSON(w, http.StatusOK, active)
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

func (r *Router) handleProposalStream(w http.ResponseWriter, req *http.Request) {
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

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Del("Content-Length")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	writeSSE := func(event string, data interface{}) {
		jsonData, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, jsonData)
		flusher.Flush()
	}

	// Always send snapshot first
	writeSSE("snapshot", proposal)

	// If not parsing, we're done
	if proposal.Status != "parsing" {
		return
	}

	// Subscribe to live events
	ch := r.eventHub.Subscribe(id)
	defer r.eventHub.Unsubscribe(id, ch)

	ctx := req.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			writeSSE(event.Type, event)
			if event.Type == "done" || event.Type == "error" {
				return
			}
		}
	}
}

func (r *Router) handleReparseProposal(w http.ResponseWriter, req *http.Request) {
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

	if proposal.Status != "failed" {
		writeError(w, http.StatusBadRequest, "proposal is not in failed state")
		return
	}

	// Reset proposal for reparse
	if err := r.store.ResetProposalForReparse(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reset proposal")
		return
	}

	// Get photo data for re-parsing
	if r.photoStore == nil {
		writeError(w, http.StatusInternalServerError, "photo store not configured")
		return
	}

	photoData, err := r.photoStore.Get(req.Context(), proposal.PhotoURL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retrieve photo")
		return
	}

	llmData := resizeImageForLLM(photoData)
	go r.parser.ParseReceiptAsync(req.Context(), id, llmData, proposal.OwnerID)

	writeJSON(w, http.StatusOK, map[string]uint64{"id": id})
}
