package receipt

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strconv"

	"code.sirenko.ca/grocer/internal/domain"
	"code.sirenko.ca/grocer/internal/llm"
	"code.sirenko.ca/grocer/internal/store"
)

// ocrAutoMatchThreshold is the string-similarity threshold above which a
// parsed item is auto-matched to an existing catalog item — but only if
// the OCR confidence is also above ocrMinConfidenceForAutoMatch.
const ocrAutoMatchThreshold = 0.99

// ocrMinConfidenceForAutoMatch is the minimum OCR confidence (0..1) required
// for an item to be auto-matched. Below this, the item is forced into human
// review even if the name matches an existing item, because the OCR was
// unsure about what was actually on the receipt.
const ocrMinConfidenceForAutoMatch = 0.85

type EventPublisher interface {
	Publish(proposalID uint64, event ParseEvent)
}

type Parser struct {
	store     *store.Store
	llm       llm.Provider
	matcher   *Matcher
	ocrEngine llm.OCREngine
	eventHub  EventPublisher
}

func NewParser(store *store.Store, llmProvider llm.Provider) *Parser {
	return &Parser{
		store:   store,
		llm:     llmProvider,
		matcher: NewMatcher(store),
	}
}

func (p *Parser) SetEventHub(hub EventPublisher) {
	p.eventHub = hub
}

// Matcher exposes the catalog matcher for preview endpoints (e.g. Costco
// import preview) that need match info without running the full pipeline.
func (p *Parser) Matcher() *Matcher {
	return p.matcher
}

// BuildForImport runs matcher + LLM categorizer over an already-structured
// receipt (e.g. from the Costco importer) and returns proposal items plus
// the resolved merchant. Costco data has no OCR confidences, so matching
// uses string similarity alone (ocr == nil disables the confidence gate).
func (p *Parser) BuildForImport(ctx context.Context, parsed *llm.ParsedReceipt) ([]domain.ProposalItem, uint64, string) {
	merchant, err := p.findOrCreateMerchant(parsed.Merchant)
	if err != nil {
		// findOrCreateMerchant only fails on store errors; fall back to a
		// zero merchant so the import can still be reviewed by hand.
		log.Printf("BuildForImport: merchant %q: %v", parsed.Merchant, err)
		return p.buildProposalItems(ctx, parsed.Items, nil), 0, parsed.Merchant
	}
	return p.buildProposalItems(ctx, parsed.Items, nil), merchant.MerchantID, merchant.Name
}

// CreateProposalForImport persists an importer-built receipt as a pending
// proposal for human review. externalID (e.g. "costco:<barcode>") enables
// idempotent re-imports.
func (p *Parser) CreateProposalForImport(ownerID, merchantID uint64, merchantName string, dateUnix, totalCents int64, externalID string, items []domain.ProposalItem) (*domain.Proposal, error) {
	proposal := &domain.Proposal{
		ProposalID: p.store.ProposalID.Gen(),
		OwnerID:    ownerID,
		MerchantID: merchantID,
		Merchant:   merchantName,
		Date:       dateUnix,
		Items:      items,
		TotalCents: totalCents,
		Status:     StatusPending,
		ExternalID: externalID,
	}
	if err := p.store.CreateProposal(proposal); err != nil {
		return nil, err
	}
	return proposal, nil
}

// CommitImportAsReceipt persists an importer-built receipt directly,
// creating catalog items for unmatched lines. Used for trusted sources
// (Costco API) where human review of every line is overkill. New items
// get the matched CategoryID (or the default category when the LLM
// suggested none); every new item records the parsed name as its first
// alias so future imports match exactly.
func (p *Parser) CommitImportAsReceipt(ctx context.Context, ownerID, merchantID uint64, dateUnix, totalCents int64, externalID string, proposalItems []domain.ProposalItem) (*domain.Receipt, error) {
	_ = ctx
	var itemsToCreate []*domain.Item
	receiptItems := make([]domain.ReceiptItem, len(proposalItems))
	for i, pi := range proposalItems {
		if pi.Quantity <= 0 {
			pi.Quantity = 1
			proposalItems[i].Quantity = 1
		}
		if pi.UnitPriceCents < 0 {
			pi.UnitPriceCents = 0
			proposalItems[i].UnitPriceCents = 0
		}
		if pi.ParsedName == "" {
			pi.ParsedName = "Costco item"
			proposalItems[i].ParsedName = "Costco item"
		}
		var itemID uint64
		if pi.MatchedItemID != 0 {
			if existing, err := p.store.GetItem(pi.MatchedItemID); err == nil {
				itemID = existing.ItemID
				if !equalFoldOrEmpty(pi.ParsedName, existing.Name) && !containsFold(existing.Aliases, pi.ParsedName) && len(existing.Aliases) < 20 {
					existing.Aliases = append(existing.Aliases, pi.ParsedName)
					_ = p.store.UpdateItem(existing)
				}
			} else {
				pi.MatchedItemID = 0
				proposalItems[i].MatchedItemID = 0
			}
		}
		if itemID == 0 {
			if pi.IsNewCategory && pi.CategoryID == 0 && pi.SuggestedCategory != "" {
				if created, err := p.findOrCreateCategoryByName(pi.SuggestedCategory); err == nil {
					pi.CategoryID = created.CategoryID
					proposalItems[i].CategoryID = created.CategoryID
				}
			}
			if pi.CategoryID != 0 {
				if _, err := p.store.GetCategory(pi.CategoryID); err != nil {
					pi.CategoryID = 0
					proposalItems[i].CategoryID = 0
				}
			}
			if pi.CategoryID == 0 {
				if def, err := p.defaultCategory(); err == nil {
					pi.CategoryID = def
					proposalItems[i].CategoryID = def
				}
			}
			item := &domain.Item{
				ItemID:     p.store.ItemID.Gen(),
				Name:       pi.ParsedName,
				CategoryID: pi.CategoryID,
				MerchantID: merchantID,
				Normalized: normalizeName(pi.ParsedName),
				Aliases:    []string{pi.ParsedName},
			}
			itemsToCreate = append(itemsToCreate, item)
			itemID = item.ItemID
		}
		receiptItems[i] = domain.ReceiptItem{
			ItemID:         itemID,
			Quantity:       pi.Quantity,
			UnitPriceCents: pi.UnitPriceCents,
		}
	}
	receipt := &domain.Receipt{
		ReceiptID:  p.store.ReceiptID.Gen(),
		MerchantID: merchantID,
		OwnerID:    ownerID,
		Date:       dateUnix,
		Items:      receiptItems,
		TotalCents: totalCents,
		ExternalID: externalID,
	}
	// Reuse the atomic approve path with a synthetic pending proposal so
	// receipt + items commit together. The proposal row is transient and
	// marked approved in the same transaction.
	synth := &domain.Proposal{
		ProposalID: p.store.ProposalID.Gen(),
		OwnerID:    ownerID,
		MerchantID: merchantID,
		Date:       dateUnix,
		Items:      proposalItems,
		TotalCents: totalCents,
		Status:     StatusPending,
		ExternalID: externalID,
	}
	if err := p.store.CreateProposal(synth); err != nil {
		return nil, err
	}
	// Flip to approved via the transactional path: create items + receipt,
	// mark proposal approved atomically.
	if err := p.store.ApproveProposalWithTransaction(synth.ProposalID, itemsToCreate, receipt); err != nil {
		// Best effort cleanup of the synthetic proposal on failure.
		_ = p.store.DeleteProposal(synth.ProposalID)
		return nil, err
	}
	return receipt, nil
}

// defaultCategory returns the first available category ID for imports
// whose LLM categorizer returned nothing (offline, or uncategorized).
// Callers fall back to 0 ("Uncategorized" in the UI) when the catalog
// is empty.
func (p *Parser) defaultCategory() (uint64, error) {
	cats, err := p.store.ListCategories()
	if err != nil || len(cats) == 0 {
		return 0, fmt.Errorf("no categories")
	}
	return cats[0].CategoryID, nil
}

// SetOCREngine wires a Mistral OCR engine (or any OCREngine) into the parser.
// When set, ParseReceiptAsync / ParseReceiptData will OCR the photo first
// and pass the markdown to the LLM extraction step. When nil, the legacy
// image-in path is used.
func (p *Parser) SetOCREngine(engine llm.OCREngine) {
	p.ocrEngine = engine
}

// ParseReceiptData parses receipt from photo without saving (caller handles persistence)
func (p *Parser) ParseReceiptData(ctx context.Context, photo []byte, ownerID uint64) (*domain.Proposal, error) {
	parsed, ocr, err := p.runParsePipeline(ctx, photo)
	if err != nil {
		return nil, err
	}

	merchant, err := p.findOrCreateMerchant(parsed.Merchant)
	if err != nil {
		return nil, fmt.Errorf("findOrCreateMerchant: %w", err)
	}

	proposalItems := p.buildProposalItems(ctx, parsed.Items, ocr)

	proposal := &domain.Proposal{
		ProposalID: p.store.ProposalID.Gen(),
		OwnerID:    ownerID,
		MerchantID: merchant.MerchantID,
		Merchant:   merchant.Name,
		Date:       parsed.Date.Unix(),
		Items:      proposalItems,
		TotalCents: dollarsToCents(parsed.Total),
		Status:     StatusPending,
	}
	if ocr != nil {
		proposal.OcrMarkdown = ocr.Markdown
		proposal.OcrMinConfidence = float32(ocr.MinConfidence)
	}
	return proposal, nil
}

// ParseEvent is sent during streaming parse.
type ParseEvent struct {
	Type     string               `json:"type"` // "progress", "item", "done", "error", "ocr_done"
	Message  string               `json:"message,omitempty"`
	Item     *domain.ProposalItem `json:"item,omitempty"`
	Index    int                  `json:"index,omitempty"`
	Proposal *domain.Proposal     `json:"proposal,omitempty"`
}

// ParseReceiptStream parses receipt from photo, emitting events as items are recognized.
// Events are sent to the returned channel. Caller must consume all events.
func (p *Parser) ParseReceiptStream(ctx context.Context, photo []byte, ownerID uint64) (<-chan ParseEvent, error) {
	log.Printf("PARSE_STREAM: starting, photo=%d bytes, ownerID=%d", len(photo), ownerID)

	events := make(chan ParseEvent, 32)

	go func() {
		defer close(events)

		// Stage 1: OCR (optional)
		var ocr *llm.OCRResult
		if p.ocrEngine != nil {
			events <- ParseEvent{Type: "progress", Message: "Reading receipt..."}
			result, err := p.ocrEngine.Extract(ctx, photo, "image/jpeg")
			if err != nil {
				events <- ParseEvent{Type: "error", Message: fmt.Sprintf("OCR: %v", err)}
				return
			}
			ocr = result
			events <- ParseEvent{Type: "ocr_done", Message: fmt.Sprintf("Read receipt (%d blocks, %.0f%% confidence)", len(ocr.Blocks), ocr.MinConfidence*100)}
		}

		// Stage 2: LLM extraction (text-only if OCR succeeded, else image-in)
		events <- ParseEvent{Type: "progress", Message: "Extracting items..."}
		var streamCh <-chan llm.StreamChunk
		var err error
		if ocr != nil {
			streamCh, err = p.llm.ParseReceiptFromTextStream(ctx, ocr)
		} else {
			streamCh, err = p.llm.ParseReceiptStream(ctx, photo)
		}
		if err != nil {
			events <- ParseEvent{Type: "error", Message: fmt.Sprintf("LLM: %v", err)}
			return
		}

		var accumulated string
		var lastItemCount int
		chunkCount := 0

		for chunk := range streamCh {
			if chunk.Error != nil {
				events <- ParseEvent{Type: "error", Message: chunk.Error.Error()}
				return
			}
			chunkCount++
			accumulated += chunk.Text
			if chunkCount%10 == 0 {
				log.Printf("PARSE_STREAM: received %d chunks, accumulated %d chars", chunkCount, len(accumulated))
			}

			var partial struct {
				Items []struct {
					Name       string  `json:"name"`
					Quantity   float64 `json:"quantity"`
					UnitPrice  float64 `json:"unit_price"`
					TotalPrice float64 `json:"total_price"`
				} `json:"items"`
			}
			if err := json.Unmarshal([]byte(accumulated), &partial); err == nil && len(partial.Items) > lastItemCount {
				for i := lastItemCount; i < len(partial.Items); i++ {
					it := partial.Items[i]
					events <- ParseEvent{
						Type:  "item",
						Index: i,
						Item: &domain.ProposalItem{
							ParsedName:     it.Name,
							Quantity:       it.Quantity,
							UnitPriceCents: dollarsToCents(it.UnitPrice),
						},
					}
				}
				lastItemCount = len(partial.Items)
				events <- ParseEvent{Type: "progress", Message: fmt.Sprintf("Found %d items...", lastItemCount)}
			}
		}

		parsed, err := llm.ParseReceiptResponse(ctx, accumulated)
		if err != nil {
			events <- ParseEvent{Type: "error", Message: fmt.Sprintf("parse response: %v", err)}
			return
		}

		events <- ParseEvent{Type: "progress", Message: "Matching items to catalog..."}

		merchant, err := p.findOrCreateMerchant(parsed.Merchant)
		if err != nil {
			events <- ParseEvent{Type: "error", Message: fmt.Sprintf("merchant: %v", err)}
			return
		}

		proposalItems := p.buildProposalItems(ctx, parsed.Items, ocr)

		proposal := &domain.Proposal{
			ProposalID: p.store.ProposalID.Gen(),
			OwnerID:    ownerID,
			MerchantID: merchant.MerchantID,
			Merchant:   merchant.Name,
			Date:       parsed.Date.Unix(),
			Items:      proposalItems,
			TotalCents: dollarsToCents(parsed.Total),
			Status:     StatusPending,
		}
		if ocr != nil {
			proposal.OcrMarkdown = ocr.Markdown
			proposal.OcrMinConfidence = float32(ocr.MinConfidence)
		}

		if err := p.store.CreateProposal(proposal); err != nil {
			events <- ParseEvent{Type: "error", Message: fmt.Sprintf("save proposal: %v", err)}
			return
		}

		events <- ParseEvent{Type: "done", Proposal: proposal}
	}()

	return events, nil
}

// ParseReceiptAsync runs the parse pipeline in the background, updating
// the proposal in the store and broadcasting events via the hub. The
// engine parameter selects which pipeline to run:
//
//   - "full" or "": OCR (if configured) + LLM from text. Original behavior.
//   - "llm_text": Skip OCR, use the proposal's existing OcrMarkdown, call LLM.
//     Useful when OCR was clean but the LLM extraction was bad.
//   - "llm_image": Skip OCR, send the photo bytes directly to the LLM.
//
// For "llm_text", the proposal must already have OcrMarkdown set; otherwise
// the caller should reject the request before invoking this function.
func (p *Parser) ParseReceiptAsync(ctx context.Context, proposalID uint64, photo []byte, ownerID uint64, engine string) {
	log.Printf("PARSE_ASYNC: starting for proposal %d, engine=%s", proposalID, engine)

	publish := func(event ParseEvent) {
		if p.eventHub != nil {
			p.eventHub.Publish(proposalID, event)
		}
	}

	fail := func(msg string) {
		log.Printf("PARSE_ASYNC: failed proposal %d: %s", proposalID, msg)
		_ = p.store.UpdateProposalStatus(proposalID, StatusFailed, msg)
		publish(ParseEvent{Type: "error", Message: msg})
	}

	// Stage 1: OCR (optional) — controlled by engine.
	var ocr *llm.OCRResult
	switch engine {
	case "llm_image":
		// Skip OCR entirely. LLM receives the image directly.
	case "llm_text":
		// Reuse existing OCR markdown from the proposal.
		proposal, err := p.store.GetProposal(proposalID)
		if err != nil {
			fail(fmt.Sprintf("load proposal: %v", err))
			return
		}
		if proposal.OcrMarkdown == "" {
			fail("llm_text engine requires existing OCR result; use engine=full or engine=llm_image")
			return
		}
		ocr = &llm.OCRResult{
			Markdown:      proposal.OcrMarkdown,
			MinConfidence: float64(proposal.OcrMinConfidence),
		}
		publish(ParseEvent{Type: "ocr_done", Message: fmt.Sprintf("Reusing existing OCR (%.0f%% confidence)", ocr.MinConfidence*100)})
	default: // "full" or ""
		if p.ocrEngine != nil {
			publish(ParseEvent{Type: "progress", Message: "Reading receipt..."})
			result, err := p.ocrEngine.Extract(ctx, photo, "image/jpeg")
			if err != nil {
				fail(fmt.Sprintf("OCR: %v", err))
				return
			}
			ocr = result
			if err := p.store.UpdateProposalOcrResult(proposalID, ocr.Markdown, float32(ocr.MinConfidence)); err != nil {
				fail(fmt.Sprintf("save OCR result: %v", err))
				return
			}
			publish(ParseEvent{Type: "ocr_done", Message: fmt.Sprintf("Read receipt (%d blocks, %.0f%% confidence)", len(ocr.Blocks), ocr.MinConfidence*100)})
		}
	}

	// Stage 2: LLM extraction
	publish(ParseEvent{Type: "progress", Message: "Extracting items..."})
	var streamCh <-chan llm.StreamChunk
	var err error
	if ocr != nil {
		streamCh, err = p.llm.ParseReceiptFromTextStream(ctx, ocr)
	} else {
		streamCh, err = p.llm.ParseReceiptStream(ctx, photo)
	}
	if err != nil {
		fail(fmt.Sprintf("LLM stream: %v", err))
		return
	}

	var accumulated string
	var lastItemCount int
	chunkCount := 0

	for chunk := range streamCh {
		if chunk.Error != nil {
			fail(fmt.Sprintf("LLM stream error: %v", chunk.Error))
			return
		}
		chunkCount++
		accumulated += chunk.Text

		var partial struct {
			Items []struct {
				Name       string  `json:"name"`
				Quantity   float64 `json:"quantity"`
				UnitPrice  float64 `json:"unit_price"`
				TotalPrice float64 `json:"total_price"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(accumulated), &partial); err == nil && len(partial.Items) > lastItemCount {
			for i := lastItemCount; i < len(partial.Items); i++ {
				it := partial.Items[i]
				pi := domain.ProposalItem{
					ParsedName:     it.Name,
					Quantity:       it.Quantity,
					UnitPriceCents: dollarsToCents(it.UnitPrice),
				}
				_ = p.store.AppendProposalItem(proposalID, pi)
				publish(ParseEvent{Type: "item", Index: i, Item: &pi})
			}
			lastItemCount = len(partial.Items)
			publish(ParseEvent{Type: "progress", Message: fmt.Sprintf("Found %d items...", lastItemCount)})
		}
	}

	parsed, err := llm.ParseReceiptResponse(ctx, accumulated)
	if err != nil {
		fail(fmt.Sprintf("parse response: %v", err))
		return
	}

	publish(ParseEvent{Type: "progress", Message: "Matching items to catalog..."})

	merchant, err := p.findOrCreateMerchant(parsed.Merchant)
	if err != nil {
		fail(fmt.Sprintf("merchant: %v", err))
		return
	}

	proposalItems := p.buildProposalItems(ctx, parsed.Items, ocr)

	if err := p.store.UpdateProposalParseResult(proposalID, merchant.MerchantID, merchant.Name, parsed.Date.Unix(), dollarsToCents(parsed.Total), proposalItems); err != nil {
		fail(fmt.Sprintf("save result: %v", err))
		return
	}

	// Save the snapshot synchronously after the final proposal update.
	// UpdateProposalParseResult already scheduled a debounced save, but
	// the debounce (default 5s) is too slow: the OCR snapshot's PubSub
	// event delivers ~1s after the OCR save, and the resulting reload
	// can clobber the just-written "pending" state in memory with the
	// stale "parsed_ocr" snapshot. A synchronous save here pushes the
	// final state to GCS immediately, so the reload (which fires 1s
	// after the OCR save) sees the correct data when it pulls.
	if err := p.store.SaveSnapshot(ctx); err != nil {
		// Non-fatal: the debounced save will retry in a few seconds,
		// and the worst case is a brief window where the proposal
		// shows the old state.
		log.Printf("PARSE_ASYNC: synchronous snapshot save failed for proposal %d: %v", proposalID, err)
	}

	proposal := &domain.Proposal{
		ProposalID: proposalID,
		OwnerID:    ownerID,
		MerchantID: merchant.MerchantID,
		Merchant:   merchant.Name,
		Date:       parsed.Date.Unix(),
		Items:      proposalItems,
		TotalCents: dollarsToCents(parsed.Total),
		Status:     StatusPending,
	}
	if ocr != nil {
		proposal.OcrMarkdown = ocr.Markdown
		proposal.OcrMinConfidence = float32(ocr.MinConfidence)
	}

	publish(ParseEvent{Type: "done", Proposal: proposal})
	log.Printf("PARSE_ASYNC: completed proposal %d", proposalID)
}

func (p *Parser) ParseReceipt(ctx context.Context, photo []byte, ownerID uint64) (*domain.Proposal, error) {
	proposal, err := p.ParseReceiptData(ctx, photo, ownerID)
	if err != nil {
		return nil, err
	}

	if err := p.store.CreateProposal(proposal); err != nil {
		return nil, fmt.Errorf("CreateProposal: %w", err)
	}

	return proposal, nil
}

// ApplyUserInput replaces the items on an existing proposal with ones
// derived from a user-supplied ParsedReceipt (typically the result of
// pasting an external LLM's TOML/JSON response). The matcher and
// categorizer run on the parsed items so the new proposal has the same
// item-matching quality as the auto pipeline. The proposal is left in
// the pending state so the user can review and approve as usual.
//
// If ocr is non-nil, OCR-confidence gating is applied to the auto-match
// step (just like the auto pipeline). Pass nil if no OCR result is
// available (e.g. the user ran the LLM-image pipeline previously).
func (p *Parser) ApplyUserInput(ctx context.Context, proposalID uint64, parsed *llm.ParsedReceipt, ocr *llm.OCRResult) (*domain.Proposal, error) {
	merchant, err := p.findOrCreateMerchant(parsed.Merchant)
	if err != nil {
		return nil, fmt.Errorf("findOrCreateMerchant: %w", err)
	}

	proposalItems := p.buildProposalItems(ctx, parsed.Items, ocr)

	if err := p.store.UpdateProposalParseResult(proposalID, merchant.MerchantID, merchant.Name, parsed.Date.Unix(), dollarsToCents(parsed.Total), proposalItems); err != nil {
		return nil, fmt.Errorf("save result: %w", err)
	}

	// Same race-condition fix as in ParseReceiptAsync: save the
	// snapshot synchronously so the final state hits GCS before
	// the OCR-snapshot PubSub reload can clobber it. See the comment
	// in ParseReceiptAsync for the full timing.
	if err := p.store.SaveSnapshot(ctx); err != nil {
		log.Printf("APPLY_USER_INPUT: synchronous snapshot save failed for proposal %d: %v", proposalID, err)
	}

	return p.store.GetProposal(proposalID)
}

// runParsePipeline is the non-streaming, single-shot version of the
// OCR → LLM extraction pipeline. Returns the parsed receipt, the OCR result
// (nil if no OCR engine was configured), and any error.
func (p *Parser) runParsePipeline(ctx context.Context, photo []byte) (*llm.ParsedReceipt, *llm.OCRResult, error) {
	if p.ocrEngine != nil {
		ocr, err := p.ocrEngine.Extract(ctx, photo, "image/jpeg")
		if err != nil {
			return nil, nil, fmt.Errorf("OCR: %w", err)
		}
		parsed, err := p.llm.ParseReceiptFromText(ctx, ocr)
		if err != nil {
			return nil, nil, fmt.Errorf("LLM extract: %w", err)
		}
		return parsed, ocr, nil
	}
	parsed, err := p.llm.ParseReceipt(ctx, photo)
	if err != nil {
		return nil, nil, err
	}
	return parsed, nil, nil
}

// buildProposalItems runs the matcher + categorizer and returns the final
// ProposalItem slice. If ocr is non-nil, OCR confidence is used to gate
// the auto-match threshold — items whose OCR confidence is below the
// minimum are forced into human review even when string similarity is high.
func (p *Parser) buildProposalItems(ctx context.Context, items []llm.ParsedItem, ocr *llm.OCRResult) []domain.ProposalItem {
	out := make([]domain.ProposalItem, len(items))
	for i, item := range items {
		matched, confidence, err := p.matcher.FindMatch(item.Name)
		if err != nil {
			log.Printf("buildProposalItems: matcher error on %q: %v", item.Name, err)
		}

		pi := domain.ProposalItem{
			ParsedName:     item.Name,
			Quantity:       item.Quantity,
			UnitPriceCents: dollarsToCents(item.UnitPrice),
			TotalPriceCents: dollarsToCents(item.TotalPrice),
		}
		if ocr != nil {
			pi.OcrConfidence = llm.ConfidenceForLine(ocr, item.Name)
			pi.SourceBlockType = llm.BlockTypeForLine(ocr, item.Name)
		}

		// OCR-aware auto-match: require BOTH high string similarity and
		// high OCR confidence for "existing" auto-match. Low confidence
		// forces the user to verify.
		ocrConf := float64(pi.OcrConfidence)
		ocrConfIsGood := ocr == nil || ocrConf >= ocrMinConfidenceForAutoMatch

		if matched != nil && confidence >= ocrAutoMatchThreshold && ocrConfIsGood {
			pi.MatchedItemID = matched.ItemID
			pi.CategoryID = matched.CategoryID
			pi.UserChoice = "existing"
		} else if matched != nil && confidence > 0.80 {
			pi.MatchedItemID = matched.ItemID
			pi.CategoryID = matched.CategoryID
		} else {
			cat, err := p.categorizeItem(ctx, item.Name)
			if err == nil {
				pi.CategoryID = cat.CategoryID
				pi.IsNewCategory = cat.IsNew
				if cat.IsNew {
					pi.SuggestedCategory = cat.SuggestedName
				}
			}
		}

		out[i] = pi
	}
	return out
}

func (p *Parser) findOrCreateCategoryByName(name string) (*domain.Category, error) {
	trimmed := normalizeName(name)
	if trimmed == "" {
		return nil, fmt.Errorf("empty category name")
	}
	cats, err := p.store.ListCategories()
	if err != nil {
		return nil, err
	}
	for _, c := range cats {
		if normalizeName(c.Name) == trimmed {
			return c, nil
		}
	}
	cat := &domain.Category{
		CategoryID: p.store.CategoryID.Gen(),
		Name:       name,
	}
	if err := p.store.CreateCategory(cat); err != nil {
		return nil, err
	}
	return cat, nil
}

func (p *Parser) findOrCreateMerchant(name string) (*domain.Merchant, error) {
	merchant, err := p.store.GetMerchantByName(name)
	if err == nil {
		return merchant, nil
	}

	merchant = &domain.Merchant{
		MerchantID: p.store.MerchantID.Gen(),
		Name:       name,
	}

	if err := p.store.CreateMerchant(merchant); err != nil {
		return nil, err
	}

	return merchant, nil
}

func (p *Parser) categorizeItem(ctx context.Context, itemName string) (*llm.Categorization, error) {
	catPtrs, err := p.store.ListCategories()
	if err != nil {
		return nil, err
	}

	categories := make([]domain.Category, len(catPtrs))
	for i, c := range catPtrs {
		categories[i] = *c
	}

	return p.llm.CategorizeItem(ctx, itemName, categories)
}

func (p *Parser) HandlePhoto(ctx context.Context, photo []byte, senderID string) (uint64, error) {
	if senderID == "" {
		return 0, fmt.Errorf("senderID is required")
	}

	ownerID, err := strconv.ParseUint(senderID, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid senderID format: %w", err)
	}

	proposal, err := p.ParseReceipt(ctx, photo, ownerID)
	if err != nil {
		return 0, err
	}

	return proposal.ProposalID, nil
}

func (p *Parser) ApproveProposal(ctx context.Context, proposalID uint64, choices map[int]string) (*domain.Receipt, error) {
	proposal, err := p.store.GetProposal(proposalID)
	if err != nil {
		return nil, err
	}

	if proposal.Status != StatusPending {
		return nil, fmt.Errorf("proposal already %s", proposal.Status)
	}

	for i, choice := range choices {
		if i >= len(proposal.Items) {
			continue
		}
		proposal.Items[i].UserChoice = choice
	}

	// Validate + resolve categories before touching the store so a bad row
	// fails fast without a half-written transaction.
	resolvedCategory := make([]uint64, len(proposal.Items))
	aliasUpdates := make(map[uint64]string) // itemID -> new alias
	for i, pi := range proposal.Items {
		if pi.Quantity <= 0 {
			return nil, fmt.Errorf("item %d: quantity must be positive", i)
		}
		if pi.UnitPriceCents < 0 {
			return nil, fmt.Errorf("item %d: unit price cannot be negative", i)
		}
		if pi.ParsedName == "" {
			return nil, fmt.Errorf("item %d: name is required", i)
		}
		if pi.UserChoice == "existing" && pi.MatchedItemID != 0 {
			matched, err := p.store.GetItem(pi.MatchedItemID)
			if err != nil {
				return nil, fmt.Errorf("item %d: matched item not found", i)
			}
			resolvedCategory[i] = matched.CategoryID
			// Alias learning: if the user confirmed a match whose printed
			// name differs from the catalog name, remember the variant so
			// future receipts match exactly. Capped to avoid unbounded growth.
			if !equalFoldOrEmpty(pi.ParsedName, matched.Name) && !containsFold(matched.Aliases, pi.ParsedName) {
				if len(matched.Aliases) < 20 {
					aliasUpdates[matched.ItemID] = pi.ParsedName
				}
			}
		} else {
			catID := pi.CategoryID
			if pi.IsNewCategory && catID == 0 && pi.SuggestedCategory != "" {
				created, err := p.findOrCreateCategoryByName(pi.SuggestedCategory)
				if err != nil {
					log.Printf("ApproveProposal: create category %q: %v", pi.SuggestedCategory, err)
				} else {
					catID = created.CategoryID
					proposal.Items[i].CategoryID = catID
				}
			}
			if catID != 0 {
				if _, err := p.store.GetCategory(catID); err != nil {
					log.Printf("ApproveProposal: item %d references missing category %d, falling back to Uncategorized", i, catID)
					catID = 0
					proposal.Items[i].CategoryID = 0
				}
			}
			resolvedCategory[i] = catID
		}
	}

	var itemsToCreate []*domain.Item
	receiptItems := make([]domain.ReceiptItem, len(proposal.Items))

	for i, pi := range proposal.Items {
		var itemID uint64

		if pi.UserChoice == "existing" && pi.MatchedItemID != 0 {
			itemID = pi.MatchedItemID
		} else {
			item := &domain.Item{
				ItemID:     p.store.ItemID.Gen(),
				Name:       pi.ParsedName,
				CategoryID: resolvedCategory[i],
				MerchantID: proposal.MerchantID,
				Normalized: normalizeName(pi.ParsedName),
				Aliases:    []string{pi.ParsedName},
			}
			itemsToCreate = append(itemsToCreate, item)
			itemID = item.ItemID
		}

		receiptItems[i] = domain.ReceiptItem{
			ItemID:         itemID,
			Quantity:       pi.Quantity,
			UnitPriceCents: pi.UnitPriceCents,
		}
	}

	receipt := &domain.Receipt{
		ReceiptID:  p.store.ReceiptID.Gen(),
		MerchantID: proposal.MerchantID,
		OwnerID:    proposal.OwnerID,
		Date:       proposal.Date,
		PhotoURL:   proposal.PhotoURL,
		Items:      receiptItems,
		TotalCents: proposal.TotalCents,
		ExternalID: proposal.ExternalID,
	}

	if err := p.store.ApproveProposalWithTransaction(proposalID, itemsToCreate, receipt); err != nil {
		return nil, err
	}

	// Best-effort alias learning after commit (non-critical path).
	for itemID, alias := range aliasUpdates {
		if it, err := p.store.GetItem(itemID); err == nil {
			it.Aliases = append(it.Aliases, alias)
			if err := p.store.UpdateItem(it); err != nil {
				log.Printf("ApproveProposal: alias update for item %d: %v", itemID, err)
			}
		}
	}

	return receipt, nil
}

func equalFoldOrEmpty(a, b string) bool {
	if a == "" || b == "" {
		return true
	}
	return normalizeName(a) == normalizeName(b)
}

func containsFold(list []string, s string) bool {
	n := normalizeName(s)
	for _, v := range list {
		if normalizeName(v) == n {
			return true
		}
	}
	return false
}

// dollarsToCents converts a dollar amount (float64) to cents (int64).
// math.Round (not truncation) is critical: int64(19.99*100) truncates to
// 1998 because 19.99 is not exactly representable in binary floating
// point (19.99*100 = 1998.999999...). Every money path in the codebase
// must go through this helper so $19.99 is always 1999¢.
func dollarsToCents(dollars float64) int64 {
	return int64(math.Round(dollars * 100))
}
