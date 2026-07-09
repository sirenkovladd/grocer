package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"code.sirenko.ca/grocer/internal/domain"
)

// isInProgressStatus reports whether a proposal is still being parsed and
// therefore a stream consumer should subscribe to live events.
func isInProgressStatus(status string) bool {
	switch status {
	case "uploaded", "parsed_ocr", "parsed_llm", "parsing":
		return true
	}
	return false
}

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

	// If not in progress, we're done. The in-progress states are
	// "uploaded" (just received), "parsed_ocr" (OCR done), "parsed_llm"
	// (LLM extraction done), and the legacy "parsing" alias.
	if !isInProgressStatus(proposal.Status) {
		return
	}

	// Clear replay buffer — snapshot already includes all persisted items
	r.eventHub.ClearReplay(id)

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
	go r.parser.ParseReceiptAsync(context.Background(), id, llmData, proposal.OwnerID)

	writeJSON(w, http.StatusOK, map[string]string{"id": fmt.Sprintf("%d", id)})
}

func (r *Router) handleDeleteProposal(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid proposal ID")
		return
	}

	if err := r.store.DeleteProposal(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete proposal")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type updateProposalItemRequest struct {
	ParsedName     string  `json:"parsedName"`
	Quantity       float64 `json:"quantity"`
	UnitPriceCents int64   `json:"unitPriceCents"`
}

func (r *Router) handleUpdateProposalItem(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid proposal ID")
		return
	}

	indexStr := req.PathValue("index")
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item index")
		return
	}

	proposal, err := r.store.GetProposal(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "proposal not found")
		return
	}

	if index < 0 || index >= len(proposal.Items) {
		writeError(w, http.StatusBadRequest, "item index out of range")
		return
	}

	var reqBody updateProposalItemRequest
	if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	proposal.Items[index].ParsedName = reqBody.ParsedName
	proposal.Items[index].Quantity = reqBody.Quantity
	proposal.Items[index].UnitPriceCents = reqBody.UnitPriceCents

	if err := r.store.UpdateProposalItems(id, proposal.Items); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update item")
		return
	}

	writeJSON(w, http.StatusOK, proposal.Items[index])
}
