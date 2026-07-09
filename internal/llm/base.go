package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"code.sirenko.ca/grocer/internal/domain"
)

// BaseProvider contains common functionality for all LLM providers
type BaseProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

// NewBaseProvider creates a new base provider with common configuration
func NewBaseProvider(apiKey, model, baseURL string) *BaseProvider {
	return &BaseProvider{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 180 * time.Second,
		},
	}
}

// doRequest performs an HTTP request with common headers and error handling
func (b *BaseProvider) doRequest(ctx context.Context, endpoint string, reqBody interface{}) ([]byte, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", b.baseURL+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+b.apiKey)

	resp, err := b.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: %d %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// encodeImageToBase64 encodes image data to base64
func encodeImageToBase64(photo []byte) string {
	return base64.StdEncoding.EncodeToString(photo)
}

// ParseReceiptResponse parses a receipt response from JSON
func ParseReceiptResponse(content string) (*ParsedReceipt, error) {
	content = trimMarkdownCodeBlock(content)

	// Use flexible struct that accepts both int and float for quantity
	var parsed struct {
		Merchant string `json:"merchant"`
		Date     string `json:"date"`
		Items    []struct {
			Name       string  `json:"name"`
			Quantity   float64 `json:"quantity"`
			UnitPrice  float64 `json:"unit_price"`
			TotalPrice float64 `json:"total_price"`
		} `json:"items"`
		Total float64 `json:"total"`
	}

	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, fmt.Errorf("parse receipt JSON: %w", err)
	}

	date, err := time.Parse("2006-01-02", parsed.Date)
	if err != nil {
		date = time.Now()
	}

	items := make([]ParsedItem, len(parsed.Items))
	for i, item := range parsed.Items {
		items[i] = ParsedItem{
			Name:       item.Name,
			Quantity:   item.Quantity,
			UnitPrice:  item.UnitPrice,
			TotalPrice: item.TotalPrice,
		}
	}

	return &ParsedReceipt{
		Merchant: parsed.Merchant,
		Date:     date,
		Items:    items,
		Total:    parsed.Total,
	}, nil
}

// parseCategorizationResponse parses a categorization response from JSON
func parseCategorizationResponse(content string) (*Categorization, error) {
	content = trimMarkdownCodeBlock(content)

	var parsed struct {
		CategoryID    uint64 `json:"category_id"`
		IsNew         bool   `json:"is_new"`
		SuggestedName string `json:"suggested_name"`
	}

	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, fmt.Errorf("parse categorization JSON: %w", err)
	}

	return &Categorization{
		CategoryID:    parsed.CategoryID,
		IsNew:         parsed.IsNew,
		SuggestedName: parsed.SuggestedName,
	}, nil
}

// trimMarkdownCodeBlock removes markdown code block markers if present
func trimMarkdownCodeBlock(content string) string {
	content = trimSpace(content)
	if len(content) > 3 && content[:3] == "```" {
		// Find the end of the first line
		endOfFirstLine := 3
		for endOfFirstLine < len(content) && content[endOfFirstLine] != '\n' {
			endOfFirstLine++
		}

		// Find the closing ```
		closingIdx := lastIndex(content, "```")
		if closingIdx > endOfFirstLine {
			content = content[endOfFirstLine+1 : closingIdx]
		}
	}
	return content
}

// buildReceiptPrompt builds the prompt for receipt parsing
func buildReceiptPrompt() string {
	return `Analyze this grocery receipt photo and extract the following information in JSON format:
{
  "merchant": "store name",
  "date": "YYYY-MM-DD",
  "items": [
    {
      "name": "item name as shown on receipt",
      "quantity": 1,
      "unit_price": 2.99,
      "total_price": 2.99
    }
  ],
  "total": 25.99
}

Note: quantity can be a decimal for weighted items (e.g. 0.445 for 445g, 1.5 for 1.5kg).
Return ONLY the JSON, no other text.`
}

// buildReceiptFromTextPrompt is the second-stage prompt used after OCR has
// already extracted text. The model receives the OCR markdown and produces
// the same JSON shape as buildReceiptPrompt.
func buildReceiptFromTextPrompt(ocr *OCRResult) string {
	var sb strings.Builder
	sb.WriteString("Below is OCR-extracted text from a grocery receipt.\n\n")
	if ocr.Header != "" {
		sb.WriteString("HEADER (likely merchant info):\n")
		sb.WriteString(ocr.Header)
		sb.WriteString("\n\n")
	}
	if ocr.Footer != "" {
		sb.WriteString("FOOTER (likely totals/tax):\n")
		sb.WriteString(ocr.Footer)
		sb.WriteString("\n\n")
	}
	sb.WriteString("FULL OCR TEXT:\n")
	sb.WriteString(ocr.Markdown)
	sb.WriteString("\n\n")
	sb.WriteString("Extract the structured receipt data as JSON:\n")
	sb.WriteString("{\n")
	sb.WriteString(`  "merchant": "store name",` + "\n")
	sb.WriteString(`  "date": "YYYY-MM-DD",` + "\n")
	sb.WriteString(`  "items": [` + "\n")
	sb.WriteString("    {\n")
	sb.WriteString(`      "name": "item name as shown on receipt",` + "\n")
	sb.WriteString(`      "quantity": 1,` + "\n")
	sb.WriteString(`      "unit_price": 2.99,` + "\n")
	sb.WriteString(`      "total_price": 2.99` + "\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ],\n")
	sb.WriteString(`  "total": 25.99` + "\n")
	sb.WriteString("}\n\n")
	sb.WriteString("Notes:\n")
	sb.WriteString("- quantity can be a decimal for weighted items (e.g. 0.445 for 445g, 1.5 for 1.5kg)\n")
	sb.WriteString("- skip subtotals, taxes, change, payment method, loyalty IDs, cashier names\n")
	sb.WriteString("- discounts and coupons are items with negative total_price\n")
	sb.WriteString("- ignore any text that is clearly not a purchased product\n")
	sb.WriteString("Return ONLY the JSON, no other text.")
	return sb.String()
}

// buildCategorizationPrompt builds the prompt for item categorization
func buildCategorizationPrompt(itemName string, categories []domain.Category) string {
	categoriesJSON, _ := json.Marshal(categories)

	return fmt.Sprintf(`Given the item "%s" and these existing categories: %s

Determine the best category. If no existing category fits, suggest a new one.

Return JSON:
{
  "category_id": 123,
  "is_new": false,
  "suggested_name": ""
}

If creating a new category, set category_id to 0 and is_new to true.
Return ONLY the JSON.`, itemName, string(categoriesJSON))
}

// Helper functions
func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

func lastIndex(s, substr string) int {
	for i := len(s) - len(substr); i >= 0; i-- {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
