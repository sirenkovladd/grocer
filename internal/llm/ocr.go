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
// per-page metadata for clients that want it.
type OCRResult struct {
	Markdown      string
	Pages         []OCRPage
	Blocks        []Block
	Tables        []Table
	Header        string
	Footer        string
	MinConfidence float64
	Model         string
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
type Block struct {
	Type    string
	Content string
	BBox    [4]int
}

type Table struct {
	ID      string
	Content string
}

// ErrOCRFailure wraps a low-level OCR error with context for the caller.
var ErrOCRFailure = errors.New("ocr failure")

// confidenceForLine returns the minimum word-confidence score for any block
// whose Content contains the given item name (case-insensitive substring).
// Falls back to ocr.MinConfidence if no block matches. Returns 1.0 if the
// OCR result has no confidence information.
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
		// Heuristic: if this block has its own confidence embedded, use it.
		// Otherwise fall back to the receipt-level min confidence.
		conf := blockConfidence(b)
		if conf > best {
			best = conf
		}
	}
	if best < 0 {
		return float32(ocr.MinConfidence)
	}
	return best
}

// blockConfidence extracts a confidence from a Block. Mistral OCR 4 returns
// per-page word-level scores; the per-block rollup isn't directly provided,
// so we use the receipt-level MinConfidence as a conservative proxy when we
// can't compute a tighter bound. A future implementation could maintain a
// parallel block → word-score index.
func blockConfidence(b Block) float32 {
	_ = b
	return -1 // sentinel: "no per-block score, fall back to receipt-level"
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
