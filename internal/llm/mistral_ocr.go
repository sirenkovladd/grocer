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
	"regexp"
	"strings"
	"time"
)

// MistralOCR implements OCREngine against Mistral's /v1/ocr endpoint.
// Default model: "mistral-ocr-4-0". Requires blocks, tables, header/footer,
// and per-word confidence to be returned.
//
// Pages, if non-empty, restricts OCR to the given page indices
// (0-based). When empty, the API processes all pages.
//
// TableFormat controls how tables are returned:
//   - "markdown" (default): tables are returned as a separate `tables`
//     array on each page, with placeholders like [tbl-3](tbl-3) in the
//     page markdown. We replace those placeholders with the table
//     content so the LLM sees the actual structure inline.
//   - "null": tables are returned inline in the markdown only.
type MistralOCR struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
	pages   []int
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

// WithPages returns a copy of m that restricts OCR to the given 0-based
// page indices. Useful for skipping blank back pages of long receipts.
// Returns m unchanged if pages is empty.
func (m *MistralOCR) WithPages(pages ...int) *MistralOCR {
	if len(pages) == 0 {
		return m
	}
	cp := *m
	cp.pages = append([]int(nil), pages...)
	return &cp
}

type mistralOCRRequest struct {
	Model                       string                    `json:"model"`
	Document                    mistralDoc                `json:"document"`
	IncludeBlocks               bool                      `json:"include_blocks"`
	TableFormat                 string                    `json:"table_format"`
	ExtractHeader               bool                      `json:"extract_header"`
	ExtractFooter               bool                      `json:"extract_footer"`
	ConfidenceScoresGranularity string                    `json:"confidence_scores_granularity"`
	Pages                       []int                     `json:"pages,omitempty"`
	DocumentAnnotationFormat    *documentAnnotationFormat `json:"document_annotation_format,omitempty"`
	DocumentAnnotationPrompt    string                    `json:"document_annotation_prompt,omitempty"`
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

// receiptAnnotationPrompt is a high-level hint that accompanies the
// annotation schema. The schema's property descriptions cover the
// field-level guidance, but a top-level prompt helps with edge cases
// (e.g., "this is a grocery receipt" focuses the model on items, prices,
// and discounts and away from loyalty programs and payment info).
const receiptAnnotationPrompt = `This is a printed grocery store receipt. Focus on identifying purchased items with their printed prices, modifier lines (discounts, bottle deposits, weight/unit price info attached to a previous item), and the totals block. Skip loyalty programs, points, transaction IDs, card numbers, and payment info. Do not invent items that aren't printed on the receipt.`

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
			AveragePageConfidenceScore float64      `json:"average_page_confidence_score"`
			MinimumPageConfidenceScore float64      `json:"minimum_page_confidence_score"`
			WordConfidenceScores       []WordScore  `json:"word_confidence_scores"`
		} `json:"confidence_scores"`
	} `json:"pages"`
	Model              string          `json:"model"`
	DocumentAnnotation json.RawMessage `json:"document_annotation"`
	UsageInfo          struct {
		PagesProcessed int  `json:"pages_processed"`
		DocSizeBytes   *int `json:"doc_size_bytes"`
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
		TableFormat:                 "markdown",
		ExtractHeader:               true,
		ExtractFooter:               true,
		ConfidenceScoresGranularity: "word",
		Pages:                       m.pages,
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
	req.DocumentAnnotationPrompt = receiptAnnotationPrompt

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

	// Log usage for cost / page-count visibility. A single receipt photo
	// being processed as 2+ pages means we paid for OCR we didn't expect.
	log.Printf("MISTRAL_OCR: model=%s pages_processed=%d doc_size_bytes=%v",
		r.Model, r.UsageInfo.PagesProcessed, r.UsageInfo.DocSizeBytes)

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

		// Build per-block confidence from the page's word scores, then
		// append the block. The per-block signal is used by
		// ConfidenceForLine in the auto-match gating path.
		var wordScores []WordScore
		if p.ConfidenceScores != nil {
			wordScores = p.ConfidenceScores.WordConfidenceScores
			if p.ConfidenceScores.MinimumPageConfidenceScore > 0 && p.ConfidenceScores.MinimumPageConfidenceScore < minConf {
				minConf = p.ConfidenceScores.MinimumPageConfidenceScore
			}
		}
		for _, b := range p.Blocks {
			block := Block{
				Type:       b.Type,
				Content:    b.Content,
				BBox:       [4]int{b.TopLeftX, b.TopLeftY, b.BottomRightX, b.BottomRightY},
				Confidence: computeBlockConfidence(b.Content, wordScores),
			}
			result.Blocks = append(result.Blocks, block)
		}

		// Collect tables; placeholders in the markdown are replaced with
		// the actual table content after per-page markdown is joined.
		for _, t := range p.Tables {
			result.Tables = append(result.Tables, Table{ID: t.ID, Content: t.Content})
		}
	}

	// Concatenate per-page markdown with a blank line separator, then
	// replace [tbl-X](tbl-X) placeholders with the actual table content
	// so the downstream LLM sees tables inline (not as opaque IDs).
	joined := strings.Join(pageMarkdowns, "\n\n")
	joined = replaceTablePlaceholders(joined, result.Tables)
	result.Markdown = joined

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
	//
	// Mistral's API sometimes returns document_annotation as a JSON
	// string (containing the annotation object as a string-encoded JSON
	// payload) rather than as the object directly. We try the direct
	// unmarshal first; if that fails with a "cannot unmarshal string"
	// error, we fall back to unmarshaling as a string and then parsing
	// the inner JSON.
	if len(r.DocumentAnnotation) > 0 && !bytes.Equal(r.DocumentAnnotation, []byte("null")) {
		var ann AnnotatedReceipt
		if err := json.Unmarshal(r.DocumentAnnotation, &ann); err != nil {
			// Retry: the payload might be a JSON-encoded string.
			var asString string
			if err2 := json.Unmarshal(r.DocumentAnnotation, &asString); err2 == nil && asString != "" {
				if err3 := json.Unmarshal([]byte(asString), &ann); err3 != nil {
					log.Printf("MISTRAL_OCR: failed to parse document_annotation: %v (payload: %s)", err3, truncate(asString, 200))
				} else {
					result.Annotated = &ann
				}
			} else {
				log.Printf("MISTRAL_OCR: failed to parse document_annotation: %v (payload: %s)", err, truncate(string(r.DocumentAnnotation), 200))
			}
		} else {
			result.Annotated = &ann
		}
	}

	return result
}

// computeBlockConfidence returns the minimum word-confidence score for
// any per-word score whose text appears in the block's content. Returns
// 0 when no words can be matched (e.g., the OCR didn't return word
// scores, or the block content has no word-like tokens).
//
// The match is case-insensitive and ignores whitespace/punctuation
// differences between the block content and the word scores. We
// deliberately use the min over the matched words (not the avg) so a
// single low-confidence word — e.g., a misread price — drags the
// block down, which matches the auto-match gate's intent of
// "uncertain words → don't auto-match".
func computeBlockConfidence(blockContent string, words []WordScore) float32 {
	if len(words) == 0 {
		return 0
	}

	// Build a word → confidence map. If a word appears multiple times
	// (e.g., a repeated label), keep the minimum so the block can't
	// hide a low-confidence occurrence.
	wordConf := make(map[string]float64, len(words))
	for _, w := range words {
		key := strings.ToLower(strings.TrimSpace(w.Word))
		if key == "" {
			continue
		}
		if existing, ok := wordConf[key]; !ok || w.Confidence < existing {
			wordConf[key] = w.Confidence
		}
	}

	// Tokenize the block content the same way (lowercased, split on
	// whitespace/punctuation) and look up each token.
	tokens := tokenizeForConfidence(blockContent)
	if len(tokens) == 0 {
		return 0
	}

	var minConf float64 = 1.0
	found := false
	for _, tok := range tokens {
		if c, ok := wordConf[tok]; ok {
			found = true
			if c < minConf {
				minConf = c
			}
		}
	}
	if !found {
		return 0
	}
	return float32(minConf)
}

// tokenizeForConfidence lowercases s and splits it into word-like
// tokens, dropping whitespace and punctuation. Mirrors how Mistral
// appears to tokenize the per-word scores in practice.
func tokenizeForConfidence(s string) []string {
	s = strings.ToLower(s)
	var tokens []string
	var current strings.Builder
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}
		// Treat most non-alphanumeric runes as separators; this
		// handles prices like "1.78", "1,78", "$4.49", and
		// "1.78A" (weighted) by splitting on the non-digit chars.
		// Keep digits and letters together so prices like "1.78"
		// stay as one token matching the word score "1.78".
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			current.WriteRune(r)
		} else {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// tablePlaceholderRe matches Mistral's [tbl-X](tbl-X) placeholders that
// replace tables in the page markdown when table_format is not "null".
// The ID may include an extension (".html" for HTML format, ".md" for
// markdown) or be bare.
var tablePlaceholderRe = regexp.MustCompile(`\[(tbl-[^\]]+)\]\([^)]+\)`)

// replaceTablePlaceholders substitutes each [tbl-X](tbl-X) placeholder in
// markdown with the corresponding table's markdown content. Tables
// whose ID doesn't match any placeholder are dropped (they weren't
// referenced). Unresolved placeholders are left in place so we don't
// silently lose information.
func replaceTablePlaceholders(markdown string, tables []Table) string {
	if len(tables) == 0 || !strings.Contains(markdown, "[tbl-") {
		return markdown
	}
	// Build a lookup keyed by the ID with any file extension stripped
	// (so placeholders match regardless of whether Mistral includes
	// ".html" / ".md" in the placeholder text).
	byID := make(map[string]string, len(tables))
	for _, t := range tables {
		if t.ID == "" {
			continue
		}
		byID[stripTableExtension(t.ID)] = t.Content
	}
	return tablePlaceholderRe.ReplaceAllStringFunc(markdown, func(match string) string {
		sm := tablePlaceholderRe.FindStringSubmatch(match)
		if len(sm) < 2 {
			return match
		}
		key := stripTableExtension(sm[1])
		if content, ok := byID[key]; ok {
			return content
		}
		return match // leave placeholder if no matching table
	})
}

// stripTableExtension removes a trailing file extension (".html", ".md")
// from a table ID. Only strips if the part after the dot is a known
// extension — leaves other dotted IDs alone (e.g. "tbl-2.v2").
func stripTableExtension(id string) string {
	if idx := strings.LastIndex(id, "."); idx > 0 {
		ext := id[idx+1:]
		if ext == "html" || ext == "md" {
			return id[:idx]
		}
	}
	return id
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
