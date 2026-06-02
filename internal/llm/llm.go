package llm

import (
	"context"
	"time"

	"code.sirenko.ca/grocer/internal/domain"
)

type Provider interface {
	ParseReceipt(ctx context.Context, photo []byte) (*ParsedReceipt, error)
	ParseReceiptStream(ctx context.Context, photo []byte) (<-chan StreamChunk, error)
	CategorizeItem(ctx context.Context, itemName string, existingCategories []domain.Category) (*Categorization, error)
}

// StreamChunk is a single chunk from a streaming LLM response.
type StreamChunk struct {
	Text  string
	Error error
}

type ParsedReceipt struct {
	Merchant string
	Date     time.Time
	Items    []ParsedItem
	Total    float64
}

type ParsedItem struct {
	Name       string
	Quantity   float64
	UnitPrice  float64
	TotalPrice float64
}

type Categorization struct {
	CategoryID    uint64
	IsNew         bool
	SuggestedName string
}
