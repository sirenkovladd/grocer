package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"code.sirenko.ca/grocer/internal/costco"
	"code.sirenko.ca/grocer/internal/llm"
)

// maxCostcoPasteBytes caps pasted Costco JSON at 5MB — a 3-month list plus
// a dozen details fits in ~500KB; 5MB leaves headroom without letting a
// hostile client wedge the JSON decoder.
const maxCostcoPasteBytes = 5 << 20

func readCostcoBody(w http.ResponseWriter, req *http.Request) ([]byte, bool) {
	raw, err := io.ReadAll(io.LimitReader(req.Body, maxCostcoPasteBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return nil, false
	}
	if len(raw) > maxCostcoPasteBytes {
		writeError(w, http.StatusBadRequest, "paste too large (max 5MB)")
		return nil, false
	}
	return raw, true
}

// extractRaw pulls the pasted JSON out of either {"content": "<json>"} or a
// bare JSON body (the UI sends the former; curl users may send the latter).
func extractRaw(body []byte) json.RawMessage {
	var wrapped struct {
		Content json.RawMessage `json:"content"`
		Raw     json.RawMessage `json:"raw"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil {
		if len(wrapped.Content) > 0 {
			// Content may itself be a JSON-encoded string (textarea paste).
			var s string
			if err := json.Unmarshal(wrapped.Content, &s); err == nil {
				return json.RawMessage(s)
			}
			return wrapped.Content
		}
		if len(wrapped.Raw) > 0 {
			return wrapped.Raw
		}
	}
	return json.RawMessage(body)
}

// POST /api/costco/preview-list — parse a pasted list response and flag
// which barcodes are already imported.
func (r *Router) handleCostcoPreviewList(w http.ResponseWriter, req *http.Request) {
	body, ok := readCostcoBody(w, req)
	if !ok {
		return
	}
	rows, err := costco.ParseList(extractRaw(body))
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("parse list: %v", err))
		return
	}
	summaries := costco.SummarizeList(rows)
	for i, s := range summaries {
		ext := costco.ExternalID(s.Barcode)
		if _, err := r.store.FindReceiptByExternalID(ext); err == nil {
			summaries[i].AlreadyImported = true
			continue
		}
		if _, err := r.store.FindProposalByExternalID(ext); err == nil {
			summaries[i].AlreadyImported = true
		}
	}
	writeJSON(w, http.StatusOK, summaries)
}

// costcoDetailPreview is one parsed detail receipt plus per-line catalog
// match info (no LLM calls — preview must be instant).
type costcoDetailPreview struct {
	Barcode       string `json:"barcode"`
	WarehouseName string `json:"warehouseName"`
	Date          int64  `json:"date"`
	TotalCents    int64  `json:"totalCents"`
	AlreadyImported bool `json:"alreadyImported"`
	Lines         []costcoLinePreview `json:"lines"`
}

type costcoLinePreview struct {
	Name            string  `json:"name"`
	SKU             string  `json:"sku"`
	Quantity        float64 `json:"quantity"`
	UnitPriceCents  int64   `json:"unitPriceCents"`
	TotalPriceCents int64   `json:"totalPriceCents"`
	MatchedItemID   string  `json:"matchedItemId,omitempty"`
	MatchedName     string  `json:"matchedName,omitempty"`
	WillCreate      bool    `json:"willCreate"`
}

// POST /api/costco/preview-detail — parse pasted detail JSON (one or many)
// and show how each line maps to the catalog.
func (r *Router) handleCostcoPreviewDetail(w http.ResponseWriter, req *http.Request) {
	body, ok := readCostcoBody(w, req)
	if !ok {
		return
	}
	details, err := costco.ParseMultiDetail(extractRaw(body))
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("parse detail: %v", err))
		return
	}
	tz := llm.TimezoneFromContext(req.Context())
	out := make([]costcoDetailPreview, 0, len(details))
	for _, d := range details {
		parsed := costco.ConvertDetail(d, tz)
		ext := costco.ExternalID(parsed.Barcode)
		imported := false
		if _, err := r.store.FindReceiptByExternalID(ext); err == nil {
			imported = true
		} else if _, err := r.store.FindProposalByExternalID(ext); err == nil {
			imported = true
		}
		prev := costcoDetailPreview{
			Barcode: parsed.Barcode, WarehouseName: parsed.WarehouseName,
			Date: parsed.Date.Unix(), TotalCents: parsed.TotalCents,
			AlreadyImported: imported,
		}
		for _, ln := range parsed.Lines {
			lp := costcoLinePreview{
				Name: ln.Name, SKU: ln.SKU, Quantity: ln.Quantity,
				UnitPriceCents: ln.UnitPriceCents, TotalPriceCents: ln.TotalPriceCents,
			}
			if r.parser != nil {
				if matched, score, _ := r.parser.Matcher().FindMatch(ln.Name); matched != nil && score >= 0.80 {
					lp.MatchedItemID = fmt.Sprintf("%d", matched.ItemID)
					lp.MatchedName = matched.Name
				} else {
					lp.WillCreate = true
				}
			} else {
				lp.WillCreate = true
			}
			prev.Lines = append(prev.Lines, lp)
		}
		out = append(out, prev)
	}
	writeJSON(w, http.StatusOK, out)
}

type costcoImportRequest struct {
	// OwnerID is the family member the imported receipts are tagged with.
	OwnerID FlexibleID `json:"ownerId"`
	// Details holds pasted detail envelopes (single, array, or NDJSON).
	// Either Details or Barcodes + direct-fetch creds style is not used;
	// paste-first keeps auth out of the server. For direct fetch, use
	// /api/costco/sync.
	Details json.RawMessage `json:"details,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
	// Mode: "proposal" (default — creates reviewable proposals) or
	// "receipt" (commits receipts directly, creating catalog items).
	Mode string `json:"mode,omitempty"`
}

// POST /api/costco/import — convert pasted details into proposals or receipts.
func (r *Router) handleCostcoImport(w http.ResponseWriter, req *http.Request) {
	body, ok := readCostcoBody(w, req)
	if !ok {
		return
	}
	var imp costcoImportRequest
	if err := json.Unmarshal(body, &imp); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if imp.OwnerID == 0 {
		imp.OwnerID = FlexibleID(r.getUserID(req))
	}
	if _, err := r.store.GetUserByUserID(imp.OwnerID.Uint64()); err != nil {
		writeError(w, http.StatusBadRequest, "owner not found")
		return
	}
	raw := imp.Details
	if len(raw) == 0 {
		raw = imp.Content
	}
	if len(raw) == 0 {
		// Bare paste without the wrapper: the whole body IS the details.
		raw = extractRaw(body)
	} else {
		// Unwrap string-encoded pastes.
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			raw = json.RawMessage(s)
		}
	}
	details, err := costco.ParseMultiDetail(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("parse details: %v", err))
		return
	}
	mode := strings.ToLower(strings.TrimSpace(imp.Mode))
	if mode == "" {
		mode = "proposal"
	}
	if mode != "proposal" && mode != "receipt" {
		writeError(w, http.StatusBadRequest, `mode must be "proposal" or "receipt"`)
		return
	}
	tz := llm.TimezoneFromContext(req.Context())
	type imported struct {
		Barcode string `json:"barcode"`
		ID      string `json:"id"`
		Mode    string `json:"mode"`
	}
	var created []imported
	var skipped []string
	var failed []map[string]string
	for _, d := range details {
		parsed := costco.ConvertDetail(d, tz)
		if parsed.Barcode == "" {
			failed = append(failed, map[string]string{"barcode": "", "error": "missing transactionBarcode"})
			continue
		}
		ext := costco.ExternalID(parsed.Barcode)
		if _, err := r.store.FindReceiptByExternalID(ext); err == nil {
			skipped = append(skipped, parsed.Barcode)
			continue
		}
		if _, err := r.store.FindProposalByExternalID(ext); err == nil {
			skipped = append(skipped, parsed.Barcode)
			continue
		}
		id, err := r.importCostcoReceipt(req.Context(), imp.OwnerID.Uint64(), parsed, ext, mode)
		if err != nil {
			log.Printf("costco import %s: %v", parsed.Barcode, err)
			failed = append(failed, map[string]string{"barcode": parsed.Barcode, "error": err.Error()})
			continue
		}
		created = append(created, imported{Barcode: parsed.Barcode, ID: fmt.Sprintf("%d", id), Mode: mode})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"created": created, "skipped": skipped, "failed": failed,
	})
}

// importCostcoReceipt persists one parsed Costco receipt as a proposal
// (default) or a committed receipt. Catalog matching reuses the receipt
// parser's matcher + LLM categorizer so Costco items learn aliases and
// categories exactly like photo-parsed items.
func (r *Router) importCostcoReceipt(ctx context.Context, ownerID uint64, parsed *costco.ParsedReceipt, externalID, mode string) (uint64, error) {
	if r.parser == nil {
		return 0, fmt.Errorf("parser not configured")
	}
	llmParsed := costco.ToLLMParsed(parsed, costco.MerchantName)
	proposalItems, merchantID, merchantName := r.parser.BuildForImport(ctx, llmParsed)
	if mode == "proposal" {
		p, err := r.parser.CreateProposalForImport(ownerID, merchantID, merchantName, parsed.Date.Unix(), parsed.TotalCents, externalID, proposalItems)
		if err != nil {
			return 0, err
		}
		return p.ProposalID, nil
	}
	rcpt, err := r.parser.CommitImportAsReceipt(ctx, ownerID, merchantID, parsed.Date.Unix(), parsed.TotalCents, externalID, proposalItems)
	if err != nil {
		return 0, err
	}
	return rcpt.ReceiptID, nil
}

type costcoSyncRequest struct {
	Bearer           string `json:"bearer"`
	WCSClientID      string `json:"wcsClientId,omitempty"`
	ClientIdentifier string `json:"clientIdentifier,omitempty"`
	Cookies          string `json:"cookies,omitempty"`
	StartDate        string `json:"startDate"`
	EndDate          string `json:"endDate"`
}

// POST /api/costco/sync — experimental direct fetch with a fresh bearer.
// Works until the token expires (~10 min) or Kasada challenges. On any
// failure the error message tells the user to fall back to paste import.
func (r *Router) handleCostcoSync(w http.ResponseWriter, req *http.Request) {
	body, ok := readCostcoBody(w, req)
	if !ok {
		return
	}
	var s costcoSyncRequest
	if err := json.Unmarshal(body, &s); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(s.Bearer) == "" {
		writeError(w, http.StatusBadRequest, "bearer is required (copy costco-x-authorization from DevTools, without the 'Bearer ' prefix)")
		return
	}
	if strings.TrimSpace(s.StartDate) == "" || strings.TrimSpace(s.EndDate) == "" {
		writeError(w, http.StatusBadRequest, "startDate and endDate are required (M/D/YYYY, e.g. 8/01/2026)")
		return
	}
	client := costco.NewClient()
	creds := costco.FetchCredentials{Bearer: strings.TrimSpace(s.Bearer), WCSClientID: s.WCSClientID, ClientIdentifier: s.ClientIdentifier, Cookies: s.Cookies}
	rows, err := client.FetchList(req.Context(), creds, s.StartDate, s.EndDate)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("costco fetch failed (token expired or bot-challenged — use paste import instead): %v", err))
		return
	}
	summaries := costco.SummarizeList(rows)
	for i, sm := range summaries {
		ext := costco.ExternalID(sm.Barcode)
		if _, err := r.store.FindReceiptByExternalID(ext); err == nil {
			summaries[i].AlreadyImported = true
			continue
		}
		if _, err := r.store.FindProposalByExternalID(ext); err == nil {
			summaries[i].AlreadyImported = true
		}
	}
	writeJSON(w, http.StatusOK, summaries)
}
