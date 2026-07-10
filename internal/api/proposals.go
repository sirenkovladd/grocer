package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"code.sirenko.ca/grocer/internal/domain"
	"code.sirenko.ca/grocer/internal/llm"
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

type reparseRequest struct {
	Engine string `json:"engine"` // "full" | "llm_text" | "llm_image", default "full"
}

func (r *Router) handleReparseProposal(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid proposal ID")
		return
	}

	var reqBody reparseRequest
	_ = json.NewDecoder(req.Body).Decode(&reqBody) // body is optional; ignore errors
	engine := reqBody.Engine
	if engine == "" {
		engine = "full"
	}
	switch engine {
	case "full", "llm_text", "llm_image":
		// ok
	default:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown engine: %q (want full, llm_text, or llm_image)", engine))
		return
	}

	proposal, err := r.store.GetProposal(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "proposal not found")
		return
	}

	// engine=llm_text requires an existing OCR result on the proposal.
	if engine == "llm_text" && proposal.OcrMarkdown == "" {
		writeError(w, http.StatusBadRequest, "no OCR result; use engine=full or engine=llm_image")
		return
	}

	// Reset proposal for reparse. For llm_text, keep the existing OCR
	// markdown so the LLM can re-process it; for full and llm_image, OCR
	// will be (re-)run or skipped entirely, so clearing it is fine.
	if engine == "llm_text" {
		if err := r.store.ResetProposalForReparseKeepOCR(id); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reset proposal")
			return
		}
	} else {
		if err := r.store.ResetProposalForReparse(id); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reset proposal")
			return
		}
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
	go r.parser.ParseReceiptAsync(context.Background(), id, llmData, proposal.OwnerID, engine)

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

type applyExternalRequest struct {
	Content string `json:"content"`
}

func (r *Router) handleApplyExternal(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid proposal ID")
		return
	}

	var reqBody applyExternalRequest
	if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	proposal, err := r.store.GetProposal(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "proposal not found")
		return
	}

	parsed, err := llm.ParseUserInput(req.Context(), []byte(reqBody.Content))
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("parse: %v", err))
		return
	}

	// Reuse existing OCR markdown (if any) so item matching uses
	// OCR-confidence gating, same as the auto pipeline. Without OCR
	// fields, matching falls back to string similarity alone.
	var ocr *llm.OCRResult
	if proposal.OcrMarkdown != "" {
		ocr = &llm.OCRResult{
			Markdown:      proposal.OcrMarkdown,
			MinConfidence: float64(proposal.OcrMinConfidence),
		}
	}

	updated, err := r.parser.ApplyUserInput(req.Context(), id, parsed, ocr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("apply: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

type updateProposalItemRequest struct {
	ParsedName      string  `json:"parsedName"`
	Quantity        float64 `json:"quantity"`
	UnitPriceCents  int64   `json:"unitPriceCents"`
	TotalPriceCents int64   `json:"totalPriceCents"`
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
	proposal.Items[index].TotalPriceCents = reqBody.TotalPriceCents

	// The client edits the total price (what was actually paid). If we got a
	// total and a quantity, recompute unit_price so quantity * unit_price
	// still matches the total the user entered. (We accept both unit and
	// total in the request so the schema stays flexible; if both are zero
	// the row just keeps whatever was already there.)
	if reqBody.TotalPriceCents > 0 && reqBody.Quantity > 0 && reqBody.UnitPriceCents == 0 {
		proposal.Items[index].UnitPriceCents = int64(float64(reqBody.TotalPriceCents) / reqBody.Quantity)
	}

	if err := r.store.UpdateProposalItems(id, proposal.Items); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update item")
		return
	}

	writeJSON(w, http.StatusOK, proposal.Items[index])
}

// handleAddProposalItem appends a new empty ProposalItem to the proposal.
// The frontend opens the inline editor on the new row so the user can
// fill in the fields. The default quantity is 1 (matches what the inline
// editor's PATCH path uses when the user types nothing).
func (r *Router) handleAddProposalItem(w http.ResponseWriter, req *http.Request) {
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

	newItem := domain.ProposalItem{
		Quantity: 1,
	}
	proposal.Items = append(proposal.Items, newItem)
	if err := r.store.UpdateProposalItems(id, proposal.Items); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add item")
		return
	}

	writeJSON(w, http.StatusOK, proposal.Items[len(proposal.Items)-1])
}

// handleDeleteProposalItem removes the item at the given index. The
// frontend implements the 5s "Undo" snackbar by POSTing a re-create on
// undo (the simpler model — avoids index-tracking across concurrent
// deletes).
func (r *Router) handleDeleteProposalItem(w http.ResponseWriter, req *http.Request) {
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

	proposal.Items = append(proposal.Items[:index], proposal.Items[index+1:]...)
	if err := r.store.UpdateProposalItems(id, proposal.Items); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete item")
		return
	}

	writeJSON(w, http.StatusOK, map[string]int{"deletedIndex": index})
}
