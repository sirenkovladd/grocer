package receipt

import (
	"context"
	"fmt"
	"strings"

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

func (p *Parser) ParseReceipt(ctx context.Context, photo []byte, ownerID uint64) (*domain.Proposal, error) {
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
			ParsedName: item.Name,
			Quantity:   item.Quantity,
			UnitPrice:  item.UnitPrice,
			Confidence: confidence,
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
		Merchant:   merchant.Name,
		Date:       parsed.Date.Unix(),
		Items:      proposalItems,
		Total:      parsed.Total,
		Status:     "pending",
	}

	if err := p.store.CreateProposal(proposal); err != nil {
		return nil, fmt.Errorf("CreateProposal: %w", err)
	}

	return proposal, nil
}

func (p *Parser) findOrCreateMerchant(name string) (*domain.Merchant, error) {
	merchants, err := p.store.ListMerchants()
	if err != nil {
		return nil, err
	}

	for _, m := range merchants {
		if strings.EqualFold(m.Name, name) {
			return m, nil
		}
	}

	merchant := &domain.Merchant{
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
	// For now, use a default user ID (first user)
	// TODO: Implement proper bot user mapping
	users, err := p.store.ListUsers()
	if err != nil || len(users) == 0 {
		return 0, fmt.Errorf("no users found")
	}

	ownerID := users[0].UserID

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

			if err := p.store.CreateItem(item); err != nil {
				return nil, err
			}
			itemID = item.ItemID
		}

		receiptItems[i] = domain.ReceiptItem{
			ItemID:    itemID,
			Quantity:  pi.Quantity,
			UnitPrice: pi.UnitPrice,
		}
	}

	receipt := &domain.Receipt{
		ReceiptID: p.store.ReceiptID.Gen(),
		OwnerID:   proposal.OwnerID,
		Date:      proposal.Date,
		PhotoURL:  proposal.PhotoURL,
		Items:     receiptItems,
		Total:     proposal.Total,
	}

	if err := p.store.CreateReceipt(receipt); err != nil {
		return nil, err
	}

	proposal.Status = "approved"
	if err := p.store.UpdateProposal(proposal); err != nil {
		return nil, err
	}

	return receipt, nil
}
