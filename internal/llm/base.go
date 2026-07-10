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

// timezoneKey is the unexported context key under which the user's
// IANA timezone (as a *time.Location) is stored. Using a struct{}
// type (rather than a string) prevents accidental collisions with
// other context values per the standard Go context idiom.
type timezoneKey struct{}

// WithTimezone returns a derived context that carries the user's local
// timezone. Date-only strings parsed downstream (LLM date, user-pasted
// TOML/JSON) are anchored to noon in this timezone so the calendar
// date is always correct regardless of the server's clock. nil tz
// is a no-op — the returned context has no timezone, and parsers
// fall back to UTC.
func WithTimezone(ctx context.Context, tz *time.Location) context.Context {
	if tz == nil {
		return ctx
	}
	return context.WithValue(ctx, timezoneKey{}, tz)
}

// TimezoneFromContext returns the timezone stored in ctx by
// WithTimezone, or UTC if none is set. Always non-nil, so callers
// can pass the result directly to time.Date / time.ParseInLocation
// without a nil check.
func TimezoneFromContext(ctx context.Context) *time.Location {
	if ctx == nil {
		return time.UTC
	}
	if tz, ok := ctx.Value(timezoneKey{}).(*time.Location); ok && tz != nil {
		return tz
	}
	return time.UTC
}

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

// ParseReceiptResponse parses a receipt response from JSON.
//
// The user's local timezone is read from ctx via TimezoneFromContext
// (set by the API layer from the X-Timezone request header). For
// date-only strings (which the LLM/OCR typically return), the parsed
// time is set to noon in that timezone so the calendar date is correct
// in every timezone. Without this, parsing "2026-07-10" as midnight
// UTC causes a -1 day shift in negative-UTC zones (the bug this fixes).
// Falls back to UTC when no timezone is in ctx, preserving the legacy
// behavior for callers that haven't migrated.
func ParseReceiptResponse(ctx context.Context, content string) (*ParsedReceipt, error) {
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

	date, err := parseDateInTimezone(parsed.Date, TimezoneFromContext(ctx))
	if err != nil {
		// Same fallback as before: bad date is logged but not fatal,
		// otherwise a typo would fail the whole parse.
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

// parseDateInTimezone parses a date string from an LLM/OCR response.
// The behavior depends on whether the string includes a time component:
//
//   - Date-only strings ("2026-07-10", "07/10/2026", "2026/07/10"):
//     returned as noon in tz. This is the common case for receipts —
//     the LLM/OCR returns just a calendar date. Noon is the safest
//     time of day: it always falls on the intended calendar date in
//     every IANA timezone (midnight UTC, by contrast, is the PREVIOUS
//     day in negative-UTC zones).
//   - Full datetime strings (RFC 3339, "2026-07-10T15:30:00"): returned
//     as parsed, with the timezone offset carried in the result.
//
// tz may be nil, in which case UTC is used. A bad/unparseable string
// returns an error so the caller can decide on a fallback (the
// existing code uses time.Now()).
func parseDateInTimezone(s string, tz *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty date")
	}
	if tz == nil {
		tz = time.UTC
	}

	// Full datetimes first. RFC 3339 / RFC 3339 Nano carry their own
	// timezone offset, so tz is ignored for these (the LLM might
	// include "Z" or "+07:00" if it knows the offset). Receipts more
	// commonly print the transaction time in the store's local
	// timezone without any offset ("07/08/2026 20:42:30"), so for
	// non-RFC3339 formats we interpret the time in tz (the user's
	// local timezone, which for a family in one tz is the same as
	// the store's). Using time.Parse (without a location) would
	// silently treat these as UTC, shifting the displayed time by
	// the user's UTC offset.
	offsetFormats := []string{time.RFC3339, time.RFC3339Nano}
	for _, f := range offsetFormats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	localFormats := []string{
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"01/02/2006 15:04:05",
		"01/02/2006 3:04:05 PM",
		"2006-01-02 3:04:05 PM",
	}
	for _, f := range localFormats {
		if t, err := time.ParseInLocation(f, s, tz); err == nil {
			return t, nil
		}
	}

	// Date-only formats: anchor to noon in tz so the calendar date is
	// correct in every timezone. Using time.ParseInLocation (not
	// time.Parse, which forces UTC) means the parsed components are
	// interpreted in tz; the resulting time is then re-anchored to
	// noon so DST transitions on the boundary date don't shift us
	// into a different day.
	dateOnly := []string{"2006-01-02", "01/02/2006", "2006/01/02"}
	for _, f := range dateOnly {
		if t, err := time.ParseInLocation(f, s, tz); err == nil {
			return time.Date(t.Year(), t.Month(), t.Day(), 12, 0, 0, 0, tz), nil
		}
	}

	return time.Time{}, fmt.Errorf("unrecognized date format: %q", s)
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

DATE/TIME EXTRACTION:
- Extract the date AND time from the receipt if a time is printed (e.g. "DATE/TIME: 07/08/2026 20:42:30", "2026-08-07 8:42 PM"). The time is usually on the transaction record line at the bottom. Output as "YYYY-MM-DDTHH:MM:SS" (24-hour, zero-padded). Example: 07/08/2026 20:42:30 → "2026-08-07T20:42:30".
- If the receipt only shows a date (no time anywhere), output "YYYY-MM-DD" with no time component. The backend will anchor it to noon in the user's local timezone to keep the calendar date correct.
- Do NOT invent a time if the receipt doesn't print one. Omit the time component instead of guessing.
- Return ONLY the JSON, no other text.`

// buildReceiptPrompt builds the prompt for receipt parsing
func buildReceiptPrompt() string {
	return `Analyze this grocery receipt photo and extract the following information in JSON format:
{
  "merchant": "store name",
  "date": "YYYY-MM-DDTHH:MM:SS",
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
//
// When the OCR engine provides a pre-extracted AnnotatedReceipt (Mistral's
// document_annotation_format), it is rendered as a readable block above
// the full OCR text so the LLM can use it as a primary source and
// cross-check against the markdown. Falls back to markdown-only if no
// annotation is present.
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
	if ocr.Annotated != nil {
		sb.WriteString("PRE-EXTRACTED STRUCTURE (use as primary source; cross-check against the full OCR text below):\n")
		sb.WriteString(formatAnnotatedReceipt(ocr.Annotated))
		sb.WriteString("\n")
	}
	sb.WriteString("FULL OCR TEXT:\n")
	sb.WriteString(ocr.Markdown)
	sb.WriteString("\n\n")
	sb.WriteString("Extract the structured receipt data as JSON:\n")
	sb.WriteString("{\n")
	sb.WriteString(`  "merchant": "store name",` + "\n")
	sb.WriteString(`  "date": "YYYY-MM-DDTHH:MM:SS",` + "\n")
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

// formatAnnotatedReceipt renders the pre-extracted structure as a
// readable text block. Natural-language format (not JSON) because LLMs
// parse embedded JSON-in-prose less reliably than natural-language
// descriptions with explicit item references.
func formatAnnotatedReceipt(ann *AnnotatedReceipt) string {
	var sb strings.Builder
	if ann.Merchant != "" {
		fmt.Fprintf(&sb, "Merchant: %s\n", ann.Merchant)
	}
	if ann.Date != "" {
		fmt.Fprintf(&sb, "Date: %s\n", ann.Date)
	}
	if len(ann.LineItems) > 0 {
		sb.WriteString("\nLine items (in order they appear on the receipt):\n")
		for i, item := range ann.LineItems {
			fmt.Fprintf(&sb, "  %d. %s — printed price: %s\n", i+1, item.Name, item.PriceText)
		}
	} else {
		sb.WriteString("\nLine items: (none detected)\n")
	}
	if len(ann.Modifiers) > 0 {
		sb.WriteString("\nModifier lines (apply to a specific item above; do NOT output as separate items):\n")
		for _, m := range ann.Modifiers {
			if m.AppliesToIndex >= 0 {
				fmt.Fprintf(&sb, "  - Item %d: %q [kind: %s]\n", m.AppliesToIndex+1, m.Text, m.Kind)
			} else {
				fmt.Fprintf(&sb, "  - (unattached): %q [kind: %s]\n", m.Text, m.Kind)
			}
		}
	}
	if ann.Totals.SubtotalText != "" || ann.Totals.TaxText != "" || ann.Totals.TotalText != "" {
		sb.WriteString("\nTotals:\n")
		if ann.Totals.SubtotalText != "" {
			fmt.Fprintf(&sb, "  Subtotal: %s\n", ann.Totals.SubtotalText)
		}
		if ann.Totals.TaxText != "" {
			fmt.Fprintf(&sb, "  Tax: %s\n", ann.Totals.TaxText)
		}
		if ann.Totals.TotalText != "" {
			fmt.Fprintf(&sb, "  Total: %s\n", ann.Totals.TotalText)
		}
	}
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
