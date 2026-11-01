package llm

import (
	"context"
	"errors"
	"time"

	"code.sirenko.ca/grocer/internal/domain"
)

// ErrStreamingUnsupported is returned by Provider stream constructors
// when the backend has no SSE path (e.g. the Zen Anthropic gateway).
// Callers should fall back to the single-shot ParseReceipt /
// ParseReceiptFromText methods. Match with errors.Is — providers must
// wrap it with %w, never compare message strings.
var ErrStreamingUnsupported = errors.New("streaming not supported")

type Provider interface {
	ParseReceipt(ctx context.Context, photo []byte) (*ParsedReceipt, error)
	ParseReceiptStream(ctx context.Context, photo []byte) (<-chan StreamChunk, error)
	ParseReceiptFromText(ctx context.Context, ocr *OCRResult) (*ParsedReceipt, error)
	ParseReceiptFromTextStream(ctx context.Context, ocr *OCRResult) (<-chan StreamChunk, error)
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
