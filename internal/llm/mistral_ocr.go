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
	Model                       string     `json:"model"`
	Document                    mistralDoc `json:"document"`
	IncludeBlocks               bool       `json:"include_blocks"`
	TableFormat                 string     `json:"table_format"`
	ExtractHeader               bool       `json:"extract_header"`
	ExtractFooter               bool       `json:"extract_footer"`
	ConfidenceScoresGranularity string     `json:"confidence_scores_granularity"`
}

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
			AveragePageConfidenceScore float64   `json:"average_page_confidence_score"`
			MinimumPageConfidenceScore float64   `json:"minimum_page_confidence_score"`
			WordConfidenceScores       []float64 `json:"word_confidence_scores"`
		} `json:"confidence_scores"`
	} `json:"pages"`
	Model     string `json:"model"`
	UsageInfo struct {
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
		TableFormat:                 "markdown",
		ExtractHeader:               true,
		ExtractFooter:               true,
		ConfidenceScoresGranularity: "word",
	}

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
	var allWordScores []float64
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
			if p.ConfidenceScores.WordConfidenceScores != nil {
				allWordScores = append(allWordScores, p.ConfidenceScores.WordConfidenceScores...)
			}
			if p.ConfidenceScores.MinimumPageConfidenceScore > 0 && p.ConfidenceScores.MinimumPageConfidenceScore < minConf {
				minConf = p.ConfidenceScores.MinimumPageConfidenceScore
			}
		}
	}

	// Concatenate per-page markdown with a blank line separator.
	result.Markdown = strings.Join(pageMarkdowns, "\n\n")

	// Compute minimum word confidence across the whole document.
	if len(allWordScores) > 0 {
		for _, s := range allWordScores {
			if s < minConf {
				minConf = s
			}
		}
	}
	if minConf == 1.0 {
		// No confidence data was returned.
		minConf = 0
	}
	result.MinConfidence = minConf
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
