package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// MistralOCR implements OCREngine against Mistral's /v1/ocr endpoint.
// Default model: "mistral-ocr-4-0". Requires blocks, tables, header/footer,
// and per-word confidence to be returned.
type MistralOCR struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

func NewMistralOCR(apiKey, model string) *MistralOCR {
	if model == "" {
		model = "mistral-ocr-4-0"
	}
	return &MistralOCR{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://api.mistral.ai/v1",
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

type mistralOCRRequest struct {
	Model                       string                    `json:"model"`
	Document                    mistralDoc                `json:"document"`
	IncludeBlocks               bool                      `json:"include_blocks"`
	TableFormat                 string                    `json:"table_format"`
	ExtractHeader               bool                      `json:"extract_header"`
	ExtractFooter               bool                      `json:"extract_footer"`
	ConfidenceScoresGranularity string                    `json:"confidence_scores_granularity"`
	DocumentAnnotationFormat    *documentAnnotationFormat `json:"document_annotation_format,omitempty"`
}

// documentAnnotationFormat asks Mistral to also produce a structured
// extraction matching the embedded JSON schema, returned in the
// document_annotation field of the response. See
// https://docs.mistral.ai/capabilities/document_ai/annotations
type documentAnnotationFormat struct {
	Type       string `json:"type"`
	JSONSchema struct {
		Name   string          `json:"name"`
		Schema json.RawMessage `json:"schema"`
		Strict bool            `json:"strict"`
	} `json:"json_schema"`
}

// receiptAnnotationSchema is the JSON schema sent to Mistral to
// pre-segment the receipt into line items, modifier lines, and totals.
// The downstream LLM extraction step uses this structured view as a
// primary source (with the markdown as a cross-check).
//
// All price/date fields are raw strings (printed as-shown). Parsing
// into numeric values is the LLM's job so that the same "copy exactly
// as printed" rule applies in both the annotated and markdown paths.
const receiptAnnotationSchema = `{
  "type": "object",
  "properties": {
    "merchant": {
      "type": "string",
      "description": "Store or merchant name as printed on the receipt, typically the largest text in the header. Empty string if not determinable."
    },
    "transaction_date": {
      "type": "string",
      "description": "Transaction date in YYYY-MM-DD format. Empty string if not determinable."
    },
    "line_items": {
      "type": "array",
      "description": "Purchased items in the order they appear on the receipt.",
      "items": {
        "type": "object",
        "properties": {
          "name": {
            "type": "string",
            "description": "Item name exactly as printed on the receipt line."
          },
          "price_text": {
            "type": "string",
            "description": "Price as printed on the same line (raw string, do not normalize, e.g. '1.78' or '$4.49')."
          }
        },
        "required": ["name", "price_text"]
      }
    },
    "modifiers": {
      "type": "array",
      "description": "Lines attached to a previous item (discounts, deposits, weight/unit info). Empty array if none.",
      "items": {
        "type": "object",
        "properties": {
          "text": {
            "type": "string",
            "description": "Raw text of the modifier line."
          },
          "kind": {
            "type": "string",
            "description": "One of: 'discount' (Card Save / Coupon / More Rewards), 'deposit' (*DEPOSIT), 'recycle_fee' (*RECYCLE FEE / *ENV FEE), 'weight_unit_price' ('0.875 kg @ $1.96/kg'), 'unknown'."
          },
          "applies_to_index": {
            "type": "integer",
            "description": "0-based index of the line_item this modifier attaches to. -1 if unattached."
          }
        },
        "required": ["text", "kind", "applies_to_index"]
      }
    },
    "totals": {
      "type": "object",
      "properties": {
        "subtotal_text": {
          "type": "string",
          "description": "Subtotal as printed (raw, e.g. '12.50'). Empty string if not present."
        },
        "tax_text": {
          "type": "string",
          "description": "Tax as printed (raw, e.g. '1.03'). Empty string if not present."
        },
        "total_text": {
          "type": "string",
          "description": "Total as printed (raw, e.g. '13.53'). Empty string if not present."
        }
      },
      "required": ["subtotal_text", "tax_text", "total_text"]
    }
  },
  "required": ["merchant", "transaction_date", "line_items", "modifiers", "totals"]
}`

type mistralDoc struct {
	Type     string `json:"type"`
	ImageURL string `json:"image_url,omitempty"`
	DocURL   string `json:"document_url,omitempty"`
}

type mistralOCRResponse struct {
	Pages []struct {
		Index      int     `json:"index"`
		Markdown   string  `json:"markdown"`
		Header     *string `json:"header"`
		Footer     *string `json:"footer"`
		Dimensions *struct {
			DPI    int `json:"dpi"`
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"dimensions"`
		Blocks []struct {
			Type         string `json:"type"`
			Content      string `json:"content"`
			TopLeftX     int    `json:"top_left_x"`
			TopLeftY     int    `json:"top_left_y"`
			BottomRightX int    `json:"bottom_right_x"`
			BottomRightY int    `json:"bottom_right_y"`
		} `json:"blocks"`
		Tables []struct {
			ID      string `json:"id"`
			Content string `json:"content"`
		} `json:"tables"`
		ConfidenceScores *struct {
			AveragePageConfidenceScore float64           `json:"average_page_confidence_score"`
			MinimumPageConfidenceScore float64           `json:"minimum_page_confidence_score"`
			WordConfidenceScores       []json.RawMessage `json:"word_confidence_scores"`
		} `json:"confidence_scores"`
	} `json:"pages"`
	Model              string          `json:"model"`
	DocumentAnnotation json.RawMessage `json:"document_annotation"`
	UsageInfo          struct {
		PagesProcessed int `json:"pages_processed"`
	} `json:"usage_info"`
}

func (m *MistralOCR) Extract(ctx context.Context, photo []byte, mime string) (*OCRResult, error) {
	if len(photo) == 0 {
		return nil, fmt.Errorf("%w: empty photo", ErrOCRFailure)
	}
	if mime == "" {
		mime = "image/jpeg"
	}

	req := mistralOCRRequest{
		Model: m.model,
		Document: mistralDoc{
			Type:     "image_url",
			ImageURL: fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(photo)),
		},
		IncludeBlocks:               true,
		TableFormat:                 "null",
		ExtractHeader:               true,
		ExtractFooter:               true,
		ConfidenceScoresGranularity: "word",
	}

	// Ask Mistral to also produce a structured extraction matching
	// receiptAnnotationSchema. The result comes back in
	// response.document_annotation and is parsed into OCRResult.Annotated
	// so the downstream LLM has pre-segmented line items, modifier lines,
	// and totals instead of re-deriving them from markdown.
	af := &documentAnnotationFormat{Type: "json_schema"}
	af.JSONSchema.Name = "receipt_annotation"
	af.JSONSchema.Schema = json.RawMessage(receiptAnnotationSchema)
	af.JSONSchema.Strict = true
	req.DocumentAnnotationFormat = af

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	var respBody []byte
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<attempt) * 200 * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
		respBody, lastErr = m.doRequest(ctx, body)
		if lastErr == nil {
			break
		}
		if !isRetryable(lastErr) {
			return nil, lastErr
		}
		log.Printf("MISTRAL_OCR: retryable error attempt %d: %v", attempt+1, lastErr)
	}
	if lastErr != nil {
		return nil, fmt.Errorf("%w: %v", ErrOCRFailure, lastErr)
	}

	var ocrResp mistralOCRResponse
	if err := json.Unmarshal(respBody, &ocrResp); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	if len(ocrResp.Pages) == 0 {
		return nil, fmt.Errorf("%w: no pages in response", ErrOCRFailure)
	}

	return parseMistralOCRResult(&ocrResp), nil
}

func parseMistralOCRResult(r *mistralOCRResponse) *OCRResult {
	result := &OCRResult{
		Model: r.Model,
	}

	var pageMarkdowns []string
	var minConf float64 = 1.0

	for _, p := range r.Pages {
		page := OCRPage{
			Index:    p.Index,
			Markdown: p.Markdown,
		}
		if p.Dimensions != nil {
			page.Width = p.Dimensions.Width
			page.Height = p.Dimensions.Height
		}
		result.Pages = append(result.Pages, page)
		pageMarkdowns = append(pageMarkdowns, p.Markdown)

		// Header/footer come from the first page that has them set.
		if p.Header != nil && *p.Header != "" && result.Header == "" {
			result.Header = *p.Header
		}
		if p.Footer != nil && *p.Footer != "" && result.Footer == "" {
			result.Footer = *p.Footer
		}

		for _, b := range p.Blocks {
			result.Blocks = append(result.Blocks, Block{
				Type:    b.Type,
				Content: b.Content,
				BBox:    [4]int{b.TopLeftX, b.TopLeftY, b.BottomRightX, b.BottomRightY},
			})
		}
		for _, t := range p.Tables {
			result.Tables = append(result.Tables, Table{ID: t.ID, Content: t.Content})
		}
		if p.ConfidenceScores != nil {
			// Per-word scores are preserved as raw JSON for future use but
			// not decoded here: their shape varies and we don't need them
			// since minimum_page_confidence_score already gives the page
			// minimum. See doc comment on WordConfidenceScores.
			if p.ConfidenceScores.MinimumPageConfidenceScore > 0 && p.ConfidenceScores.MinimumPageConfidenceScore < minConf {
				minConf = p.ConfidenceScores.MinimumPageConfidenceScore
			}
		}
	}

	// Concatenate per-page markdown with a blank line separator.
	result.Markdown = strings.Join(pageMarkdowns, "\n\n")

	if minConf == 1.0 {
		// No confidence data was returned.
		minConf = 0
	}
	result.MinConfidence = minConf

	// Parse document_annotation if present. Failures here are non-fatal:
	// the LLM extraction step falls back to markdown-only parsing when
	// Annotated is nil. Common causes of failure: API didn't include the
	// field (older models), model returned a shape that doesn't match
	// the schema, or the response was empty/null.
	if len(r.DocumentAnnotation) > 0 && !bytes.Equal(r.DocumentAnnotation, []byte("null")) {
		var ann AnnotatedReceipt
		if err := json.Unmarshal(r.DocumentAnnotation, &ann); err != nil {
			log.Printf("MISTRAL_OCR: failed to parse document_annotation: %v (payload: %s)", err, truncate(string(r.DocumentAnnotation), 200))
		} else {
			result.Annotated = &ann
		}
	}

	return result
}

func (m *MistralOCR) doRequest(ctx context.Context, body []byte) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "POST", m.baseURL+"/ocr", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+m.apiKey)

	resp, err := m.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &httpStatusError{status: resp.StatusCode, body: string(respBody)}
	}
	return respBody, nil
}

type httpStatusError struct {
	status int
	body   string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.status, truncate(e.body, 500))
}

func isRetryable(err error) bool {
	var httpErr *httpStatusError
	if !asHTTPError(err, &httpErr) {
		return false
	}
	switch httpErr.status {
	case 429, 500, 502, 503, 504:
		return true
	}
	return false
}

func asHTTPError(err error, target **httpStatusError) bool {
	if err == nil {
		return false
	}
	if t, ok := err.(*httpStatusError); ok {
		*target = t
		return true
	}
	// Allow wrapped errors (e.g. fmt.Errorf("...: %w", err)).
	for {
		if t, ok := err.(*httpStatusError); ok {
			*target = t
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
		if err == nil {
			return false
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
