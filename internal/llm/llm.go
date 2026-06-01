package llm

import (
	"context"
	"time"

	"code.sirenko.ca/grocer/internal/domain"
)

type Provider interface {
	ParseReceipt(ctx context.Context, photo []byte) (*ParsedReceipt, error)
	CategorizeItem(ctx context.Context, itemName string, existingCategories []domain.Category) (*Categorization, error)
}

type ParsedReceipt struct {
	Merchant string
	Date     time.Time
	Items    []ParsedItem
	Total    float64
}

type ParsedItem struct {
	Name       string
	Quantity   uint32
	UnitPrice  float64
	TotalPrice float64
}

type Categorization struct {
	CategoryID    uint64
	IsNew         bool
	SuggestedName string
}
