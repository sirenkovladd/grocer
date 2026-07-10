package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMistralOCR_Extract verifies that the request body is shaped correctly
// and the response is parsed into the right OCRResult fields.
func TestMistralOCR_Extract(t *testing.T) {
	var gotBody string
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ocr" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"pages": [
				{
					"index": 0,
					"markdown": "# Walmart\n\nBANANAS ORG  1.78\nMILK 2%      4.49\n\nTOTAL  6.27",
					"header": "Walmart\n123 Main St",
					"footer": "TOTAL  6.27\nTHANK YOU",
					"dimensions": {"dpi": 200, "width": 1700, "height": 2200},
					"blocks": [
						{"type": "title", "content": "Walmart", "top_left_x": 100, "top_left_y": 50, "bottom_right_x": 400, "bottom_right_y": 100},
						{"type": "text", "content": "BANANAS ORG  1.78", "top_left_x": 50, "top_left_y": 200, "bottom_right_x": 800, "bottom_right_y": 230},
						{"type": "footer", "content": "TOTAL  6.27", "top_left_x": 50, "top_left_y": 600, "bottom_right_x": 400, "bottom_right_y": 630}
					],
					"confidence_scores": {
						"average_page_confidence_score": 0.95,
						"minimum_page_confidence_score": 0.82,
						"word_confidence_scores": [
							{"word": "Walmart", "confidence": 0.98},
							{"word": "BANANAS", "confidence": 0.94},
							{"word": "ORG", "confidence": 0.88},
							{"word": "1.78", "confidence": 0.91}
						]
					}
				}
			],
			"model": "mistral-ocr-4-0",
			"document_annotation": {
				"merchant": "Walmart",
				"transaction_date": "2026-07-10",
				"line_items": [
					{"name": "BANANAS ORG", "price_text": "1.78"},
					{"name": "MILK 2%", "price_text": "4.49"}
				],
				"modifiers": [],
				"totals": {
					"subtotal_text": "6.27",
					"tax_text": "",
					"total_text": "6.27"
				}
			},
			"usage_info": {"pages_processed": 1}
		}`))
	}))
	defer srv.Close()

	ocr := &MistralOCR{
		apiKey:  "test-key",
		model:   "mistral-ocr-4-0",
		baseURL: srv.URL,
		client:  srv.Client(),
	}

	result, err := ocr.Extract(context.Background(), []byte("fake-jpeg-bytes"), "image/jpeg")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Verify auth header
	if gotAuth != "Bearer test-key" {
		t.Errorf("auth header: got %q, want %q", gotAuth, "Bearer test-key")
	}

	// Verify required flags are in the request
	for _, want := range []string{
		`"include_blocks":true`,
		`"table_format":"null"`,
		`"extract_header":true`,
		`"extract_footer":true`,
		`"confidence_scores_granularity":"word"`,
		`"model":"mistral-ocr-4-0"`,
		`"type":"image_url"`,
		`"image_url":"data:image/jpeg;base64,ZmFrZS1qcGVnLWJ5dGVz"`,
		// document_annotation_format is sent so the API also returns
		// pre-segmented line items, modifiers, and totals.
		`"document_annotation_format":{`,
		`"type":"json_schema"`,
		`"name":"receipt_annotation"`,
		`"strict":true`,
		`"line_items"`,
		`"price_text"`,
		`"applies_to_index"`,
	} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("request body missing %s\nbody: %s", want, gotBody)
		}
	}

	// Verify response parsing
	if result.Model != "mistral-ocr-4-0" {
		t.Errorf("Model: got %q", result.Model)
	}
	if result.Header != "Walmart\n123 Main St" {
		t.Errorf("Header: got %q", result.Header)
	}
	if result.Footer != "TOTAL  6.27\nTHANK YOU" {
		t.Errorf("Footer: got %q", result.Footer)
	}
	if !strings.Contains(result.Markdown, "BANANAS ORG") {
		t.Errorf("Markdown missing content: %q", result.Markdown)
	}
	if len(result.Blocks) != 3 {
		t.Errorf("Blocks: got %d, want 3", len(result.Blocks))
	}
	if len(result.Pages) != 1 {
		t.Errorf("Pages: got %d, want 1", len(result.Pages))
	}
	if result.Pages[0].Width != 1700 || result.Pages[0].Height != 2200 {
		t.Errorf("Page dimensions: got %dx%d, want 1700x2200", result.Pages[0].Width, result.Pages[0].Height)
	}
	// MinConfidence should be the min of the word scores: 0.82
	if result.MinConfidence < 0.81 || result.MinConfidence > 0.83 {
		t.Errorf("MinConfidence: got %f, want ~0.82", result.MinConfidence)
	}

	// Verify document_annotation was parsed into the typed AnnotatedReceipt
	if result.Annotated == nil {
		t.Fatal("Annotated: got nil, want non-nil")
	}
	if result.Annotated.Merchant != "Walmart" {
		t.Errorf("Annotated.Merchant: got %q, want %q", result.Annotated.Merchant, "Walmart")
	}
	if result.Annotated.Date != "2026-07-10" {
		t.Errorf("Annotated.Date: got %q, want %q", result.Annotated.Date, "2026-07-10")
	}
	if len(result.Annotated.LineItems) != 2 {
		t.Fatalf("Annotated.LineItems: got %d, want 2", len(result.Annotated.LineItems))
	}
	if result.Annotated.LineItems[0].Name != "BANANAS ORG" || result.Annotated.LineItems[0].PriceText != "1.78" {
		t.Errorf("Annotated.LineItems[0]: got %+v", result.Annotated.LineItems[0])
	}
	if result.Annotated.LineItems[1].Name != "MILK 2%" || result.Annotated.LineItems[1].PriceText != "4.49" {
		t.Errorf("Annotated.LineItems[1]: got %+v", result.Annotated.LineItems[1])
	}
	if result.Annotated.Totals.TotalText != "6.27" {
		t.Errorf("Annotated.Totals.TotalText: got %q, want %q", result.Annotated.Totals.TotalText, "6.27")
	}
}

// TestMistralOCR_EmptyPhoto verifies that empty input is rejected.
func TestMistralOCR_EmptyPhoto(t *testing.T) {
	ocr := NewMistralOCR("test-key", "mistral-ocr-4-0")
	_, err := ocr.Extract(context.Background(), nil, "image/jpeg")
	if err == nil {
		t.Fatal("expected error for empty photo")
	}
}

// TestMistralOCR_NoAnnotationInResponse verifies graceful degradation when
// the API doesn't return a document_annotation field (older models, or
// when the model returns null). The result should still be usable; just
// without the pre-extracted structure.
func TestMistralOCR_NoAnnotationInResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// No document_annotation field in response.
		_, _ = w.Write([]byte(`{
			"pages": [{"index": 0, "markdown": "Walmart\nBANANAS  1.78\nTOTAL 1.78"}],
			"model": "mistral-ocr-4-0",
			"usage_info": {"pages_processed": 1}
		}`))
	}))
	defer srv.Close()

	ocr := &MistralOCR{
		apiKey:  "test-key",
		model:   "mistral-ocr-4-0",
		baseURL: srv.URL,
		client:  srv.Client(),
	}

	result, err := ocr.Extract(context.Background(), []byte("fake-jpeg"), "image/jpeg")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if result.Annotated != nil {
		t.Errorf("Annotated: got %+v, want nil (no annotation in response)", result.Annotated)
	}
	if !strings.Contains(result.Markdown, "BANANAS") {
		t.Errorf("Markdown should still be populated, got %q", result.Markdown)
	}
}

// TestMistralOCR_NullAnnotationInResponse verifies that an explicit null
// document_annotation (rather than absent) is treated the same as absent.
func TestMistralOCR_NullAnnotationInResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"pages": [{"index": 0, "markdown": "x"}],
			"model": "mistral-ocr-4-0",
			"document_annotation": null,
			"usage_info": {"pages_processed": 1}
		}`))
	}))
	defer srv.Close()

	ocr := &MistralOCR{
		apiKey:  "test-key",
		model:   "mistral-ocr-4-0",
		baseURL: srv.URL,
		client:  srv.Client(),
	}
	result, err := ocr.Extract(context.Background(), []byte("fake-jpeg"), "image/jpeg")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if result.Annotated != nil {
		t.Errorf("Annotated: got %+v, want nil (null annotation in response)", result.Annotated)
	}
}

// TestConfidenceForLine verifies the per-line confidence helper returns the
// receipt-level minimum when no block matches the item name.
func TestConfidenceForLine(t *testing.T) {
	ocr := &OCRResult{
		MinConfidence: 0.9,
		Blocks: []Block{
			{Type: "text", Content: "BANANAS ORG  1.78"},
			{Type: "text", Content: "MILK 2% 1GAL  4.49"},
		},
	}
	if got := ConfidenceForLine(ocr, "BANANAS ORG"); got < 0.89 || got > 0.91 {
		t.Errorf("ConfidenceForLine BANANAS ORG: got %f, want ~0.9", got)
	}
	if got := ConfidenceForLine(ocr, "UNKNOWN ITEM"); got < 0.89 || got > 0.91 {
		t.Errorf("ConfidenceForLine UNKNOWN: got %f, want ~0.9", got)
	}
	if got := ConfidenceForLine(ocr, ""); got < 0.89 || got > 0.91 {
		t.Errorf("ConfidenceForLine empty: got %f, want ~0.9", got)
	}
	if got := ConfidenceForLine(nil, "anything"); got != 0 {
		t.Errorf("ConfidenceForLine nil: got %f, want 0", got)
	}
}

func TestBlockTypeForLine(t *testing.T) {
	ocr := &OCRResult{
		Blocks: []Block{
			{Type: "title", Content: "Walmart"},
			{Type: "text", Content: "BANANAS ORG  1.78"},
			{Type: "footer", Content: "TOTAL  6.27"},
		},
	}
	cases := []struct {
		name string
		item string
		want string
	}{
		{"title match", "Walmart", "title"},
		{"text match", "BANANAS ORG", "text"},
		{"case insensitive", "walmart", "title"},
		{"no match", "MYSTERY", ""},
		{"empty", "", ""},
	}
	for _, c := range cases {
		got := BlockTypeForLine(ocr, c.item)
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	if got := BlockTypeForLine(nil, "BANANAS"); got != "" {
		t.Errorf("nil ocr: got %q, want \"\"", got)
	}
}
