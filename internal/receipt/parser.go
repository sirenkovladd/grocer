package receipt

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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

		parsed, err := llm.ParseReceiptResponse(accumulated)
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

	parsed, err := llm.ParseReceiptResponse(accumulated)
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
			}
		}

		out[i] = pi
	}
	return out
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
				CategoryID: pi.CategoryID,
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
	}

	if err := p.store.ApproveProposalWithTransaction(proposalID, itemsToCreate, receipt); err != nil {
		return nil, err
	}

	return receipt, nil
}

// dollarsToCents converts a dollar amount (float64) to cents (int64)
func dollarsToCents(dollars float64) int64 {
	return int64(dollars * 100)
}
