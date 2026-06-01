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

type Parser struct {
	store   *store.Store
	llm     llm.Provider
	matcher *Matcher
}

func NewParser(store *store.Store, llmProvider llm.Provider) *Parser {
	return &Parser{
		store:   store,
		llm:     llmProvider,
		matcher: NewMatcher(store),
	}
}

// ParseReceiptData parses receipt from photo without saving (caller handles persistence)
func (p *Parser) ParseReceiptData(ctx context.Context, photo []byte, ownerID uint64) (*domain.Proposal, error) {
	parsed, err := p.llm.ParseReceipt(ctx, photo)
	if err != nil {
		return nil, fmt.Errorf("llm.ParseReceipt: %w", err)
	}

	merchant, err := p.findOrCreateMerchant(parsed.Merchant)
	if err != nil {
		return nil, fmt.Errorf("findOrCreateMerchant: %w", err)
	}

	proposalItems := make([]domain.ProposalItem, len(parsed.Items))
	for i, item := range parsed.Items {
		matched, confidence, err := p.matcher.FindMatch(item.Name)
		if err != nil {
			return nil, fmt.Errorf("FindMatch: %w", err)
		}

		pi := domain.ProposalItem{
			ParsedName:     item.Name,
			Quantity:       item.Quantity,
			UnitPriceCents: dollarsToCents(item.UnitPrice),
			Confidence:     confidence,
		}

		if matched != nil && confidence >= 0.99 {
			pi.MatchedItemID = matched.ItemID
			pi.CategoryID = matched.CategoryID
			pi.UserChoice = "existing"
		} else if matched != nil && confidence > 0.80 {
			pi.MatchedItemID = matched.ItemID
			pi.CategoryID = matched.CategoryID
			pi.UserChoice = ""
		} else {
			cat, err := p.categorizeItem(ctx, item.Name)
			if err != nil {
				return nil, fmt.Errorf("categorizeItem: %w", err)
			}
			pi.CategoryID = cat.CategoryID
			pi.IsNewCategory = cat.IsNew
		}

		proposalItems[i] = pi
	}

	proposal := &domain.Proposal{
		ProposalID: p.store.ProposalID.Gen(),
		OwnerID:    ownerID,
		MerchantID: merchant.MerchantID,
		Merchant:   merchant.Name,
		Date:       parsed.Date.Unix(),
		Items:      proposalItems,
		TotalCents: dollarsToCents(parsed.Total),
		Status:     "pending",
	}

	return proposal, nil
}

// ParseEvent is sent during streaming parse.
type ParseEvent struct {
	Type     string             `json:"type"` // "progress", "item", "done", "error"
	Message  string             `json:"message,omitempty"`
	Item     *domain.ProposalItem `json:"item,omitempty"`
	Index    int                `json:"index,omitempty"`
	Proposal *domain.Proposal   `json:"proposal,omitempty"`
}

// ParseReceiptStream parses receipt from photo, emitting events as items are recognized.
// Events are sent to the returned channel. Caller must consume all events.
func (p *Parser) ParseReceiptStream(ctx context.Context, photo []byte, ownerID uint64) (<-chan ParseEvent, error) {
	log.Printf("PARSE_STREAM: starting LLM stream, photo=%d bytes, ownerID=%d", len(photo), ownerID)
	streamCh, err := p.llm.ParseReceiptStream(ctx, photo)
	if err != nil {
		log.Printf("PARSE_STREAM: LLM stream init error: %v", err)
		return nil, fmt.Errorf("llm.ParseReceiptStream: %w", err)
	}
	log.Printf("PARSE_STREAM: LLM stream channel obtained, starting goroutine")

	events := make(chan ParseEvent, 32)

	go func() {
		defer close(events)

		events <- ParseEvent{Type: "progress", Message: "Parsing receipt with AI..."}

		var accumulated string
		var lastItemCount int
		chunkCount := 0

		for chunk := range streamCh {
			if chunk.Error != nil {
				log.Printf("PARSE_STREAM: chunk error: %v", chunk.Error)
				events <- ParseEvent{Type: "error", Message: chunk.Error.Error()}
				return
			}
			chunkCount++
			accumulated += chunk.Text
			if chunkCount%10 == 0 {
				log.Printf("PARSE_STREAM: received %d chunks, accumulated %d chars", chunkCount, len(accumulated))
			}

			// Try to parse accumulated JSON to detect new items
			var partial struct {
				Merchant string `json:"merchant"`
				Items    []struct {
					Name       string  `json:"name"`
					Quantity   uint32  `json:"quantity"`
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

		// Final parse with full matching & categorization
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

		proposalItems := make([]domain.ProposalItem, len(parsed.Items))
		for i, item := range parsed.Items {
			matched, confidence, err := p.matcher.FindMatch(item.Name)
			if err != nil {
				events <- ParseEvent{Type: "error", Message: fmt.Sprintf("match: %v", err)}
				return
			}

			pi := domain.ProposalItem{
				ParsedName:     item.Name,
				Quantity:       item.Quantity,
				UnitPriceCents: dollarsToCents(item.UnitPrice),
				Confidence:     confidence,
			}

			if matched != nil && confidence >= 0.99 {
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

			proposalItems[i] = pi
		}

		proposal := &domain.Proposal{
			ProposalID: p.store.ProposalID.Gen(),
			OwnerID:    ownerID,
			MerchantID: merchant.MerchantID,
			Merchant:   merchant.Name,
			Date:       parsed.Date.Unix(),
			Items:      proposalItems,
			TotalCents: dollarsToCents(parsed.Total),
			Status:     "pending",
		}

		if err := p.store.CreateProposal(proposal); err != nil {
			events <- ParseEvent{Type: "error", Message: fmt.Sprintf("save proposal: %v", err)}
			return
		}

		events <- ParseEvent{Type: "done", Proposal: proposal}
	}()

	return events, nil
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

func (p *Parser) findOrCreateMerchant(name string) (*domain.Merchant, error) {
	// Try to find existing merchant first
	merchant, err := p.store.GetMerchantByName(name)
	if err == nil {
		return merchant, nil
	}

	// Create new merchant if not found
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
	// Parse senderID to get the owner ID
	if senderID == "" {
		return 0, fmt.Errorf("senderID is required")
	}

	// Try to parse senderID as uint64
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

	if proposal.Status != "pending" {
		return nil, fmt.Errorf("proposal already %s", proposal.Status)
	}

	for i, choice := range choices {
		if i >= len(proposal.Items) {
			continue
		}
		proposal.Items[i].UserChoice = choice
	}

	// Prepare items to create and receipt items
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

	// Use transaction for atomic operation
	if err := p.store.ApproveProposalWithTransaction(proposalID, itemsToCreate, receipt); err != nil {
		return nil, err
	}

	return receipt, nil
}

// dollarsToCents converts a dollar amount (float64) to cents (int64)
func dollarsToCents(dollars float64) int64 {
	return int64(dollars * 100)
}
