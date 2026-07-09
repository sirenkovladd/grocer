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
		// Some LLM responses omit total_price or set it equal to unit_price.
		// For non-weighted items they're identical; for weighted items the
		// LLM should always provide total_price (= quantity * unit_price
		// as printed on the receipt). Fall back to total = unit * qty when
		// total is missing so the UI always has a value to display.
		if items[i].TotalPrice == 0 && items[i].UnitPrice != 0 {
			items[i].TotalPrice = items[i].UnitPrice * items[i].Quantity
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

// receiptParsingRules is the shared set of rules for parsing a grocery
// receipt. It's embedded in both the image-in and text-only prompts so the
// two paths produce consistent output. Keep rules specific and verifiable —
// the LLM has to read these against ambiguous OCR text.
const receiptParsingRules = `Critical parsing rules:

PRICE EXTRACTION (most important):
- For non-weighted items (quantity 1), copy the printed price EXACTLY as it appears on the receipt into BOTH unit_price and total_price. Do not perform any arithmetic. If the receipt says $8.45, output $8.45 (not $8.44, not $8.4, not $8.5).
- For weighted items, the printed line total (the number on the same line as the item name) is total_price. Copy it exactly. The per-kg/lb number from the next line is unit_price. Example: "BANANAS 1.72" then "0.875 kg @ $1.96/kg" → quantity 0.875, unit_price 1.96, total_price 1.72.
- Round total_price to the nearest cent as printed. Never invent a different number.

ATTACHED LINES (consume into the preceding item, do NOT output as separate items):
- "Card $X.XX Save -Y" / "Save -$Y" / "Coupon -$Y" / "More Rewards -$Y" → discount on preceding item. Reduce that item's total_price by Y. Example: "ASTRO YOGURT 5.69" then "Card $3.69 Save -2.00" → single item ASTRO YOGURT, total_price 3.69.
- "*DEPOSIT", "*RECYCLE FEE", "*ENV FEE", "*BOTTLE DEPOSIT" → price adder on preceding item. ADD to total_price. Example:
    Dld 2% Fltrd Milk 6.89
    *DEPOSIT 0.10
    *RECYCLE FEE 0.02
  → single item Dld 2% Fltrd Milk, total_price 7.01 (6.89 + 0.10 + 0.02).
- "0.875 kg @ $1.96/kg" or "$1.96/lb" → unit-price info for preceding item, NOT a separate item.
- "Card $X.XX" alone (no "Save"/"Coupon") → confirms the discounted total of the preceding item, NOT a separate item.

EXCLUDE entirely (these are not items, not prices, do not emit them):
- "Sub Total", "Subtotal", "Tax", "GST", "PST", "HST", "Total", "Balance Due", "Credit", "Cash", "Change", "Payment", "VISA", "MASTERCARD", "DEBIT".
- Card numbers (e.g. "XXXXX6431"), transaction IDs, "TRANSACTION RECORD", "TYPE: Purchase", "ACCT:", "REF#", "AUTHOR#", "AID:", "APPROVED", "NO SIGNATURE", "FF/DT".
- Loyalty / rewards: "Your Savings Today", "Points Earned", "Opening Balance", "More Rewards Card", "Card $$ pts".
- "FALSE CREEK", "B.C. OWNED AND OPERATED", "Visit www....", "G.S.T #R...", "IMPORTANT:", "retain this copy", "CUSTOMER COPY", store numbers, addresses, phone numbers.

GENERAL:
- quantity can be a decimal for weighted items (e.g. 0.875 for 875g).
- If unsure about a line, skip it rather than guess.
- Return ONLY the JSON, no other text.`

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

` + receiptParsingRules
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
	sb.WriteString(receiptParsingRules)
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
