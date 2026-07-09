package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"code.sirenko.ca/grocer/internal/domain"
	"golang.org/x/image/draw"
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

	filtered := r.filterReceipts(receipts, from, to, owner, category)
	writeJSON(w, http.StatusOK, filtered)
}

// filterReceipts applies the same query-parameter filters used by both
// /api/receipts and /api/receipts/enriched. The filter chain is:
//
//	from (YYYY-MM-DD)      — receipt.Date >= fromDate
//	to   (YYYY-MM-DD)      — receipt.Date <= toDate
//	owner (numeric ID)     — receipt.OwnerID == ownerID
//	category (numeric ID)  — receipt has any item with item.CategoryID == categoryID
//
// A malformed filter value is silently ignored, matching the existing
// /api/receipts contract (see ticket 03, open question on date
// validation). Filter results are always non-nil so the caller can rely
// on a `[]` JSON array for empty results.
func (r *Router) filterReceipts(receipts []*domain.Receipt, from, to, owner, category string) []*domain.Receipt {
	if receipts == nil {
		return []*domain.Receipt{}
	}

	if from != "" {
		if fromDate, err := time.Parse("2006-01-02", from); err == nil {
			result := make([]*domain.Receipt, 0, len(receipts))
			for _, rcpt := range receipts {
				if !time.Unix(rcpt.Date, 0).Before(fromDate) {
					result = append(result, rcpt)
				}
			}
			receipts = result
		}
	}

	if to != "" {
		if toDate, err := time.Parse("2006-01-02", to); err == nil {
			result := make([]*domain.Receipt, 0, len(receipts))
			for _, rcpt := range receipts {
				if !time.Unix(rcpt.Date, 0).After(toDate) {
					result = append(result, rcpt)
				}
			}
			receipts = result
		}
	}

	if owner != "" {
		if ownerID, err := strconv.ParseUint(owner, 10, 64); err == nil {
			result := make([]*domain.Receipt, 0, len(receipts))
			for _, rcpt := range receipts {
				if rcpt.OwnerID == ownerID {
					result = append(result, rcpt)
				}
			}
			receipts = result
		}
	}

	if category != "" {
		if categoryID, err := strconv.ParseUint(category, 10, 64); err == nil {
			// Batch-load items to avoid N+1.
			itemMap := r.loadItemMap(receipts)
			result := make([]*domain.Receipt, 0, len(receipts))
			for _, rcpt := range receipts {
				for _, item := range rcpt.Items {
					if itemObj, ok := itemMap[item.ItemID]; ok && itemObj.CategoryID == categoryID {
						result = append(result, rcpt)
						break
					}
				}
			}
			receipts = result
		}
	}

	return receipts
}

// loadItemMap batch loads all items referenced by receipts into a map for O(1) lookups
func (r *Router) loadItemMap(receipts []*domain.Receipt) map[uint64]*domain.Item {
	// Collect unique item IDs
	itemIDs := make(map[uint64]bool)
	for _, receipt := range receipts {
		for _, item := range receipt.Items {
			itemIDs[item.ItemID] = true
		}
	}

	// Batch load all items
	itemMap := make(map[uint64]*domain.Item)
	for itemID := range itemIDs {
		if itemObj, err := r.store.GetItem(itemID); err == nil {
			itemMap[itemID] = itemObj
		}
	}

	return itemMap
}

// loadMerchantMap batch-loads all merchants into a map keyed by MerchantID.
func (r *Router) loadMerchantMap() (map[uint64]*domain.Merchant, error) {
	merchants, err := r.store.ListMerchants()
	if err != nil {
		return nil, err
	}
	m := make(map[uint64]*domain.Merchant, len(merchants))
	for _, x := range merchants {
		m[x.MerchantID] = x
	}
	return m, nil
}

// loadUserMap batch-loads all users into a map keyed by UserID.
func (r *Router) loadUserMap() (map[uint64]*domain.User, error) {
	users, err := r.store.ListUsers()
	if err != nil {
		return nil, err
	}
	m := make(map[uint64]*domain.User, len(users))
	for _, x := range users {
		m[x.UserID] = x
	}
	return m, nil
}

// loadCategoryMap batch-loads all categories into a map keyed by CategoryID.
func (r *Router) loadCategoryMap() (map[uint64]*domain.Category, error) {
	categories, err := r.store.ListCategories()
	if err != nil {
		return nil, err
	}
	m := make(map[uint64]*domain.Category, len(categories))
	for _, x := range categories {
		m[x.CategoryID] = x
	}
	return m, nil
}

// handleListEnrichedReceipts returns a list of EnrichedReceiptSummary
// sorted by date descending (newest first). Supports the same query
// parameters as handleListReceipts (from, to, owner, category).
//
// Lookup data (merchants, users) is batch-loaded in one pass; no N+1.
func (r *Router) handleListEnrichedReceipts(w http.ResponseWriter, req *http.Request) {
	from := req.URL.Query().Get("from")
	to := req.URL.Query().Get("to")
	owner := req.URL.Query().Get("owner")
	category := req.URL.Query().Get("category")

	receipts, err := r.store.ListReceipts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	filtered := r.filterReceipts(receipts, from, to, owner, category)

	// Newest first — home/receipts pages want recent activity at the top.
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Date > filtered[j].Date
	})

	merchantMap, err := r.loadMerchantMap()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	userMap, err := r.loadUserMap()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	summaries := enrichReceiptsToSummary(filtered, merchantMap, userMap)
	writeJSON(w, http.StatusOK, summaries)
}

// handleGetEnrichedReceipt returns one EnrichedReceipt with full
// per-item enrichment. Returns 404 if the ID does not exist.
func (r *Router) handleGetEnrichedReceipt(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid receipt ID")
		return
	}

	rcpt, err := r.store.GetReceipt(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "receipt not found")
		return
	}

	merchantMap, err := r.loadMerchantMap()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	userMap, err := r.loadUserMap()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	itemMap := r.loadItemMap([]*domain.Receipt{rcpt})
	categoryMap, err := r.loadCategoryMap()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	enriched := enrichReceipt(rcpt, merchantMap, userMap, itemMap, categoryMap)
	writeJSON(w, http.StatusOK, enriched)
}

// enrichReceiptsToSummary converts a slice of domain.Receipt into
// EnrichedReceiptSummary. Lookup maps must be pre-populated by the
// caller. Missing entries in the maps fall back to the Unknown*
// constants from types.go.
func enrichReceiptsToSummary(
	receipts []*domain.Receipt,
	merchantMap map[uint64]*domain.Merchant,
	userMap map[uint64]*domain.User,
) []EnrichedReceiptSummary {
	summaries := make([]EnrichedReceiptSummary, 0, len(receipts))
	for _, rcpt := range receipts {
		summaries = append(summaries, EnrichedReceiptSummary{
			ReceiptID:    rcpt.ReceiptID,
			MerchantID:   rcpt.MerchantID,
			MerchantName: merchantName(merchantMap, rcpt.MerchantID),
			OwnerID:      rcpt.OwnerID,
			OwnerName:    ownerName(userMap, rcpt.OwnerID),
			Date:         rcpt.Date,
			ItemCount:    len(rcpt.Items),
			TotalCents:   rcpt.TotalCents,
			PhotoURL:     rcpt.PhotoURL,
		})
	}
	return summaries
}

// enrichReceipt converts one domain.Receipt into a fully-enriched
// EnrichedReceipt. All four lookup maps must be pre-populated. Item-name
// and category-name lookups fall back to UnknownItem / UnknownCategory
// if the referenced entity was deleted.
func enrichReceipt(
	rcpt *domain.Receipt,
	merchantMap map[uint64]*domain.Merchant,
	userMap map[uint64]*domain.User,
	itemMap map[uint64]*domain.Item,
	categoryMap map[uint64]*domain.Category,
) EnrichedReceipt {
	items := make([]EnrichedReceiptItem, 0, len(rcpt.Items))
	for _, item := range rcpt.Items {
		itemObj := itemMap[item.ItemID] // may be nil if the item was deleted
		var name string
		var catID uint64
		var catName string
		if itemObj != nil {
			name = itemObj.Name
			catID = itemObj.CategoryID
			if cat := categoryMap[catID]; cat != nil {
				catName = cat.Name
			} else {
				catName = UnknownCategory
			}
		} else {
			name = UnknownItem
			catName = UnknownCategory
		}
		items = append(items, EnrichedReceiptItem{
			ItemID:          item.ItemID,
			Name:            name,
			CategoryID:      catID,
			CategoryName:    catName,
			Quantity:        item.Quantity,
			UnitPriceCents:  item.UnitPriceCents,
			// math.Round (not float-truncation) is critical: 0.5 * 333
			// would truncate to 166 instead of rounding to 167.
			TotalPriceCents: int64(math.Round(item.Quantity * float64(item.UnitPriceCents))),
		})
	}

	return EnrichedReceipt{
		ReceiptID:    rcpt.ReceiptID,
		MerchantID:   rcpt.MerchantID,
		MerchantName: merchantName(merchantMap, rcpt.MerchantID),
		OwnerID:      rcpt.OwnerID,
		OwnerName:    ownerName(userMap, rcpt.OwnerID),
		Date:         rcpt.Date,
		PhotoURL:     rcpt.PhotoURL,
		Items:        items,
		TotalCents:   rcpt.TotalCents,
	}
}

// merchantName returns the merchant's name, or UnknownMerchant if the
// lookup fails (merchant deleted, or ID never existed).
func merchantName(m map[uint64]*domain.Merchant, id uint64) string {
	if m, ok := m[id]; ok && m != nil {
		return m.Name
	}
	return UnknownMerchant
}

// ownerName returns the user's display name, or UnknownOwner if the
// lookup fails. See UnknownOwner for the rationale on the short string.
func ownerName(m map[uint64]*domain.User, id uint64) string {
	if u, ok := m[id]; ok && u != nil {
		return u.Name
	}
	return UnknownOwner
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

// updateReceiptRequest carries optional fields for the inline edit
// UI on the receipt detail page. Any combination may be present;
// nil fields leave the corresponding receipt attribute unchanged.
type updateReceiptRequest struct {
	MerchantID *uint64 `json:"merchantId,omitempty"`
	Date       *int64  `json:"date,omitempty"`
	TotalCents *int64  `json:"totalCents,omitempty"`
}

// handleUpdateReceipt applies an inline edit to a saved receipt:
// merchant (re-link to a different merchant by id), date, and total.
// Per-item edits are a separate endpoint below.
func (r *Router) handleUpdateReceipt(w http.ResponseWriter, req *http.Request) {
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

	var reqBody updateReceiptRequest
	if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if reqBody.MerchantID != nil {
		// Validate the merchant exists; otherwise the receipt would
		// dangle and the enriched handler would render "Unknown
		// merchant" forever.
		if _, err := r.store.GetMerchant(*reqBody.MerchantID); err != nil {
			writeError(w, http.StatusBadRequest, "merchant not found")
			return
		}
		receipt.MerchantID = *reqBody.MerchantID
	}
	if reqBody.Date != nil {
		receipt.Date = *reqBody.Date
	}
	if reqBody.TotalCents != nil {
		if *reqBody.TotalCents < 0 {
			writeError(w, http.StatusBadRequest, "total cannot be negative")
			return
		}
		receipt.TotalCents = *reqBody.TotalCents
	}

	if err := r.store.UpdateReceipt(receipt); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, receipt)
}

// updateReceiptItemRequest carries optional fields for a single
// receipt line item. ItemID changes are supported (re-link to a
// different catalog item); this covers the "wrong item matched"
// case where the LLM picked the wrong banana.
type updateReceiptItemRequest struct {
	ItemID         *uint64  `json:"itemId,omitempty"`
	Quantity       *float64 `json:"quantity,omitempty"`
	UnitPriceCents *int64   `json:"unitPriceCents,omitempty"`
}

// handleUpdateReceiptItem edits a single line item on a saved
// receipt. Index is the position in the receipt.Items slice, same
// convention as the proposal item edit endpoint.
func (r *Router) handleUpdateReceiptItem(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid receipt ID")
		return
	}

	indexStr := req.PathValue("index")
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item index")
		return
	}

	receipt, err := r.store.GetReceipt(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "receipt not found")
		return
	}

	if index < 0 || index >= len(receipt.Items) {
		writeError(w, http.StatusBadRequest, "item index out of range")
		return
	}

	var reqBody updateReceiptItemRequest
	if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if reqBody.ItemID != nil {
		// Validate the new item exists so the receipt doesn't dangle.
		if _, err := r.store.GetItem(*reqBody.ItemID); err != nil {
			writeError(w, http.StatusBadRequest, "item not found")
			return
		}
		receipt.Items[index].ItemID = *reqBody.ItemID
	}
	if reqBody.Quantity != nil {
		if *reqBody.Quantity <= 0 {
			writeError(w, http.StatusBadRequest, "quantity must be positive")
			return
		}
		receipt.Items[index].Quantity = *reqBody.Quantity
	}
	if reqBody.UnitPriceCents != nil {
		if *reqBody.UnitPriceCents < 0 {
			writeError(w, http.StatusBadRequest, "unit price cannot be negative")
			return
		}
		receipt.Items[index].UnitPriceCents = *reqBody.UnitPriceCents
	}

	if err := r.store.UpdateReceipt(receipt); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, receipt.Items[index])
}

// handleReopenReceipt converts an approved receipt back into a
// proposal for full re-edit. Used by the 'Re-open as proposal'
// button on the receipt detail page when the user wants more
// invasive changes than the per-field PATCH (e.g. replacing the
// whole item list, or re-running the LLM).
//
// We snapshot the current items (preserving itemId, quantity,
// unitPriceCents, computed total), look up the merchant and
// item names, then create a fresh proposal. The original
// receipt is deleted; the user re-approves the new proposal
// to commit a new receipt. This is destructive of the source
// receipt; the client should confirm before calling.
func (r *Router) handleReopenReceipt(w http.ResponseWriter, req *http.Request) {
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

	// Look up merchant name. Falls back to "Unknown" if the
	// merchant was deleted; the user can rename in the proposal.
	merchantName := "Unknown"
	if m, err := r.store.GetMerchant(receipt.MerchantID); err == nil {
		merchantName = m.Name
	}

	// Build the proposal items with parsedName from the catalog.
	// We preserve quantity and unitPriceCents exactly so the line
	// totals are unchanged; the user can edit them in the
	// proposal flow. Items that point at a deleted catalog item
	// fall back to "Unknown item" — the proposal can then be
	// edited to retarget them.
	proposalItems := make([]domain.ProposalItem, 0, len(receipt.Items))
	for _, ri := range receipt.Items {
		parsedName := "Unknown item"
		var catID uint64
		if it, err := r.store.GetItem(ri.ItemID); err == nil {
			parsedName = it.Name
			catID = it.CategoryID
		}
		// Use the per-line total if it differs from quantity*unit
		// (weighted items); otherwise compute it.
		total := int64(math.Round(ri.Quantity * float64(ri.UnitPriceCents)))
		proposalItems = append(proposalItems, domain.ProposalItem{
			ParsedName:      parsedName,
			Quantity:        ri.Quantity,
			UnitPriceCents:  ri.UnitPriceCents,
			TotalPriceCents: total,
			MatchedItemID:   ri.ItemID,
			CategoryID:      catID,
		})
	}

	// Create the new proposal. Status is "pending" — the proposal
	// is ready for review immediately, not "uploaded"/"parsing".
	proposal := &domain.Proposal{
		ProposalID: r.store.ProposalID.Gen(),
		OwnerID:    receipt.OwnerID,
		MerchantID: receipt.MerchantID,
		Merchant:   merchantName,
		Date:       receipt.Date,
		PhotoURL:   receipt.PhotoURL,
		Items:      proposalItems,
		TotalCents: receipt.TotalCents,
		Status:     "pending",
	}
	if err := r.store.CreateProposal(proposal); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create proposal")
		return
	}

	// Delete the source receipt. The proposal approval flow
	// creates a fresh receipt when the user re-approves; we
	// don't want two records of the same shopping trip.
	if err := r.store.DeleteReceipt(id); err != nil {
		// Roll back: delete the proposal we just created so the
		// user doesn't see a half-finished state.
		r.store.DeleteProposal(proposal.ProposalID)
		writeError(w, http.StatusInternalServerError, "failed to delete source receipt")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"id": fmt.Sprintf("%d", proposal.ProposalID),
	})
}

// createManualReceiptRequest is the body of POST /api/receipts/manual.
// Used by the "Create receipt manually" form on the upload page when
// the LLM can't parse a photo (or for entering historic receipts).
type createManualReceiptRequest struct {
	MerchantID uint64        `json:"merchantId"`
	Date       int64         `json:"date"`
	TotalCents int64         `json:"totalCents"`
	Items      []struct {
		ItemID         uint64  `json:"itemId"`
		Quantity       float64 `json:"quantity"`
		UnitPriceCents int64   `json:"unitPriceCents"`
	} `json:"items"`
}

// handleCreateManualReceipt creates a receipt from a manually-entered
// form, bypassing the photo → LLM pipeline. Used for receipts the
// LLM couldn't parse, or for entering historic receipts. Validates
// that the merchant and every referenced item exist; at least one
// item is required.
func (r *Router) handleCreateManualReceipt(w http.ResponseWriter, req *http.Request) {
	var reqBody createManualReceiptRequest
	if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if reqBody.MerchantID == 0 {
		writeError(w, http.StatusBadRequest, "merchantId is required")
		return
	}
	if reqBody.Date == 0 {
		writeError(w, http.StatusBadRequest, "date is required")
		return
	}
	if reqBody.TotalCents < 0 {
		writeError(w, http.StatusBadRequest, "total cannot be negative")
		return
	}
	if len(reqBody.Items) == 0 {
		writeError(w, http.StatusBadRequest, "at least one item is required")
		return
	}

	if _, err := r.store.GetMerchant(reqBody.MerchantID); err != nil {
		writeError(w, http.StatusBadRequest, "merchant not found")
		return
	}

	// Validate every item ID exists; build the receipt items.
	receiptItems := make([]domain.ReceiptItem, 0, len(reqBody.Items))
	for i, it := range reqBody.Items {
		if it.ItemID == 0 {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("item %d: itemId is required", i))
			return
		}
		if it.Quantity <= 0 {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("item %d: quantity must be positive", i))
			return
		}
		if it.UnitPriceCents < 0 {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("item %d: unit price cannot be negative", i))
			return
		}
		if _, err := r.store.GetItem(it.ItemID); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("item %d: item not found", i))
			return
		}
		receiptItems = append(receiptItems, domain.ReceiptItem{
			ItemID:         it.ItemID,
			Quantity:       it.Quantity,
			UnitPriceCents: it.UnitPriceCents,
		})
	}

	receipt := &domain.Receipt{
		ReceiptID:  r.store.ReceiptID.Gen(),
		MerchantID: reqBody.MerchantID,
		OwnerID:    r.getUserID(req),
		Date:       reqBody.Date,
		Items:      receiptItems,
		TotalCents: reqBody.TotalCents,
	}
	if err := r.store.CreateReceipt(receipt); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create receipt")
		return
	}
	writeJSON(w, http.StatusCreated, receipt)
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

	// Get original image hash for duplicate detection
	originalHash := req.FormValue("originalHash")
	if originalHash != "" {
		existing, err := r.store.FindProposalByHash(originalHash)
		if err == nil && existing != nil {
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error":      "duplicate_image",
				"message":    "This image was already uploaded",
				"existingId": fmt.Sprintf("%d", existing.ProposalID),
			})
			return
		}
	}

	// Resize for LLM
	llmData := resizeImageForLLM(photoData)

	// Create proposal immediately with "uploaded" status. The OCR stage
	// (if configured) will move it to "parsed_ocr", then the LLM extraction
	// stage to "parsed_llm", and finally to "pending" when ready for review.
	proposal := &domain.Proposal{
		ProposalID:   r.store.ProposalID.Gen(),
		OwnerID:      userID,
		Status:       "uploaded",
		OriginalHash: originalHash,
	}

	// Save photo if photo store is configured
	if r.photoStore != nil {
		photoURL, err := r.photoStore.Save(req.Context(), proposal.ProposalID, photoData)
		if err == nil {
			proposal.PhotoURL = photoURL
		}
	}

	if err := r.store.CreateProposal(proposal); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create proposal")
		return
	}

	// Spawn background parse goroutine with detached context
	go r.parser.ParseReceiptAsync(context.Background(), proposal.ProposalID, llmData, userID, "full")

	writeJSON(w, http.StatusOK, map[string]string{"id": fmt.Sprintf("%d", proposal.ProposalID)})
}

const maxLLMImageDim = 2000

// resizeImageForLLM resizes an image to max 1024px on the longest side
// and re-encodes as JPEG at 80% quality. Returns original if smaller or on error.
func resizeImageForLLM(data []byte) []byte {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		log.Printf("WARNING: could not decode image for resize: %v", err)
		return data
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	if w <= maxLLMImageDim && h <= maxLLMImageDim {
		return data
	}

	// Scale down preserving aspect ratio
	ratio := float64(maxLLMImageDim) / float64(max(w, h))
	newW := int(float64(w) * ratio)
	newH := int(float64(h) * ratio)

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 80}); err != nil {
		log.Printf("WARNING: could not encode resized image: %v", err)
		return data
	}

	log.Printf("Resized image: %dx%d -> %dx%d (%d -> %d bytes)", w, h, newW, newH, len(data), buf.Len())
	return buf.Bytes()
}
