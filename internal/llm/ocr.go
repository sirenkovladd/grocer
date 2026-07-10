package llm

import (
	"context"
	"errors"
	"strings"
)

// OCREngine extracts text and structural information from a receipt photo.
// Implementations include MistralOCR (cloud). Returning nil for any optional
// field is allowed — callers must tolerate partial results.
type OCREngine interface {
	// Extract runs OCR on a single image. The mime argument is the photo's
	// MIME type (e.g. "image/jpeg", "image/png") and is forwarded to the API.
	Extract(ctx context.Context, photo []byte, mime string) (*OCRResult, error)
}

// OCRResult is the structured output of an OCREngine. Markdown is the canonical
// text used as input to the downstream LLM extraction step. Pages preserves
// per-page metadata for clients that want it. Annotated, when non-nil, is a
// pre-structured view of the receipt (line items, modifier lines, totals)
// produced by Mistral's document_annotation_format — the downstream LLM
// receives this alongside the markdown to reduce re-parsing work.
type OCRResult struct {
	Markdown      string
	Pages         []OCRPage
	Blocks        []Block
	Tables        []Table
	Header        string
	Footer        string
	MinConfidence float64
	Model         string
	Annotated     *AnnotatedReceipt
}

// AnnotatedReceipt is the structured extraction from Mistral's
// document_annotation_format. It pre-segments the receipt into line items,
// modifier lines, and totals so the downstream LLM doesn't have to
// re-derive this structure from markdown.
//
// All price/date fields are intentionally raw strings (printed price,
// printed total) rather than parsed values. Parsing is the LLM's job —
// OCR can misread "$1.78" as "1.78" or "1,78" or "I.78" and we want the
// LLM to apply the same "copy exactly as printed" rule it already uses
// for the markdown path. Empty strings mean "not present on this receipt".
type AnnotatedReceipt struct {
	Merchant  string              `json:"merchant"`
	Date      string              `json:"transaction_date"`
	LineItems []AnnotatedLineItem `json:"line_items"`
	Modifiers []AnnotatedModifier `json:"modifiers"`
	Totals    AnnotatedTotals     `json:"totals"`
}

// AnnotatedLineItem is a single purchased item, segmented by Mistral.
type AnnotatedLineItem struct {
	Name      string `json:"name"`
	PriceText string `json:"price_text"`
}

// AnnotatedModifier is a non-item line that attaches to a previous line
// item (discounts, deposits, weight/unit price info). The Kind taxonomy
// mirrors the rules in receiptParsingRules.
type AnnotatedModifier struct {
	Text           string `json:"text"`
	Kind           string `json:"kind"` // "discount", "deposit", "recycle_fee", "weight_unit_price", "unknown"
	AppliesToIndex int    `json:"applies_to_index"` // -1 if unattached
}

// AnnotatedTotals is the totals block at the bottom of the receipt.
// All fields are raw, printed strings; empty means "not present".
type AnnotatedTotals struct {
	SubtotalText string `json:"subtotal_text"`
	TaxText      string `json:"tax_text"`
	TotalText    string `json:"total_text"`
}

type OCRPage struct {
	Index int
	// Markdown is the page's own markdown; the top-level OCRResult.Markdown
	// is the concatenation of all pages.
	Markdown string
	Width    int
	Height   int
}

// Block is a structurally-labeled region of a page. Type is one of:
// "text", "title", "list", "table", "image", "equation", "caption", "code",
// "references", "aside_text", "header", "footer", "signature".
// BBox is [top_left_x, top_left_y, bottom_right_x, bottom_right_y] in pixels.
// Confidence is the minimum per-word OCR confidence for words within the
// block (decoded from word_confidence_scores); 0 if not computable.
type Block struct {
	Type       string
	Content    string
	BBox       [4]int
	Confidence float32
}

// WordScore is a single per-word confidence entry from Mistral's
// word_confidence_scores array. Words are returned in reading order
// per page.
type WordScore struct {
	Word       string  `json:"word"`
	Confidence float64 `json:"confidence"`
}

type Table struct {
	ID      string
	Content string
}

// ErrOCRFailure wraps a low-level OCR error with context for the caller.
var ErrOCRFailure = errors.New("ocr failure")

// confidenceForLine returns the minimum word-confidence score for any block
// whose Content contains the given item name (case-insensitive substring).
// Falls back to ocr.MinConfidence if no block has a per-block confidence.
// Returns 0 if the OCR result has no confidence information.
//
// A block's Confidence is 0 when word scores weren't returned (older
// models) or when the block content has no matching words in the
// per-word scores (e.g. a footer line not in the OCR's word list).
// In both cases we treat it as "no signal" and use the page minimum
// as a conservative proxy.
func ConfidenceForLine(ocr *OCRResult, itemName string) float32 {
	if ocr == nil {
		return 0
	}
	needle := strings.ToLower(strings.TrimSpace(itemName))
	if needle == "" || len(ocr.Blocks) == 0 {
		return float32(ocr.MinConfidence)
	}
	var best float32 = -1
	for _, b := range ocr.Blocks {
		if b.Content == "" {
			continue
		}
		hay := strings.ToLower(b.Content)
		if !strings.Contains(hay, needle) {
			continue
		}
		// 0 means "no per-block signal available"; skip and let the
		// loop fall through to the MinConfidence fallback below.
		if b.Confidence > 0 && b.Confidence > best {
			best = b.Confidence
		}
	}
	if best < 0 {
		return float32(ocr.MinConfidence)
	}
	return best
}

// blockTypeForLine returns the Block.Type for the first block whose Content
// contains the item name (case-insensitive substring). Returns "" if no
// block matches.
func BlockTypeForLine(ocr *OCRResult, itemName string) string {
	if ocr == nil {
		return ""
	}
	needle := strings.ToLower(strings.TrimSpace(itemName))
	if needle == "" {
		return ""
	}
	for _, b := range ocr.Blocks {
		if b.Content == "" {
			continue
		}
		if strings.Contains(strings.ToLower(b.Content), needle) {
			return b.Type
		}
	}
	return ""
}
