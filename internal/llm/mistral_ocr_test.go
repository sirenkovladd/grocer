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
		`"table_format":"markdown"`,
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

// TestComputeBlockConfidence covers the per-block confidence rollup from
// per-word scores: takes the min over matched tokens, is case-insensitive,
// strips punctuation, and returns 0 when no words match.
func TestComputeBlockConfidence(t *testing.T) {
	words := []WordScore{
		{Word: "BANANAS", Confidence: 0.94},
		{Word: "ORG", Confidence: 0.88},
		{Word: "1.78", Confidence: 0.91},
		{Word: "MILK", Confidence: 0.97},
	}
	cases := []struct {
		name    string
		content string
		wantMin float32 // expected min confidence
		want0   bool    // expected 0 (no match)
	}{
		{"all words present, take min", "BANANAS ORG  1.78", 0.88, false},
		{"case-insensitive", "bananas org  1.78", 0.88, false},
		{"punctuation differences", "BANANAS, ORG -- 1.78", 0.88, false},
		{"partial match: 1 of 3", "BANANAS", 0.94, false},
		{"no matching words", "TOTAL  6.27", 0, true},
		{"empty content", "", 0, true},
	}
	for _, c := range cases {
		got := computeBlockConfidence(c.content, words)
		if c.want0 {
			if got != 0 {
				t.Errorf("%s: got %f, want 0", c.name, got)
			}
			continue
		}
		if got < c.wantMin-0.001 || got > c.wantMin+0.001 {
			t.Errorf("%s: got %f, want ~%f", c.name, got, c.wantMin)
		}
	}
	// Empty word list → 0.
	if got := computeBlockConfidence("anything", nil); got != 0 {
		t.Errorf("empty words: got %f, want 0", got)
	}
}

// TestComputeBlockConfidence_WordAppearsMultipleTimes verifies the
// implementation keeps the worst confidence when the same word appears
// more than once in the word list (e.g., a label and an item).
func TestComputeBlockConfidence_WordAppearsMultipleTimes(t *testing.T) {
	words := []WordScore{
		{Word: "Total", Confidence: 0.95},
		{Word: "Total", Confidence: 0.60}, // same word, lower confidence
	}
	got := computeBlockConfidence("Total  13.53", words)
	if got < 0.59 || got > 0.61 {
		t.Errorf("expected min of repeated word (~0.60), got %f", got)
	}
}

// TestMistralOCR_ExtractsUsageInfo verifies that the parsed OCRResult
// preserves usage info (consumed only for logging today, but the field
// is part of the response so it's worth checking the round-trip).
func TestMistralOCR_ExtractsUsageInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"pages": [{"index": 0, "markdown": "x"}],
			"model": "mistral-ocr-4-0",
			"usage_info": {"pages_processed": 2, "doc_size_bytes": 12345}
		}`))
	}))
	defer srv.Close()

	ocr := &MistralOCR{apiKey: "k", model: "m", baseURL: srv.URL, client: srv.Client()}
	// We just want to make sure the request doesn't error and the
	// pages_processed count doesn't blow up; the actual log line is
	// exercised in integration testing.
	if _, err := ocr.Extract(context.Background(), []byte("img"), "image/jpeg"); err != nil {
		t.Fatalf("Extract: %v", err)
	}
}

// TestMistralOCR_RequestIncludesAnnotationPrompt verifies the
// document_annotation_prompt is sent alongside the schema so the
// annotation step has the grocery-receipt framing context.
func TestMistralOCR_RequestIncludesAnnotationPrompt(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pages":[{"index":0,"markdown":"x"}],"model":"m","usage_info":{"pages_processed":1}}`))
	}))
	defer srv.Close()

	ocr := &MistralOCR{apiKey: "k", model: "m", baseURL: srv.URL, client: srv.Client()}
	if _, err := ocr.Extract(context.Background(), []byte("img"), "image/jpeg"); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if !strings.Contains(gotBody, `"document_annotation_prompt":"`) {
		t.Errorf("request missing document_annotation_prompt\nbody: %s", gotBody)
	}
	// The prompt should mention "grocery" so Mistral knows the domain.
	if !strings.Contains(gotBody, "grocery") {
		t.Errorf("document_annotation_prompt should mention grocery domain\nbody: %s", gotBody)
	}
}

// TestMistralOCR_ReplacesTablePlaceholders verifies that when the API
// returns tables as a separate field with placeholders in the markdown,
// the placeholders are replaced with the actual table content so the
// LLM sees tables inline (not as opaque [tbl-3](tbl-3) IDs).
func TestMistralOCR_ReplacesTablePlaceholders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"pages": [{
				"index": 0,
				"markdown": "Header text\n\n[tbl-1](tbl-1)\n\nFooter text",
				"tables": [
					{"id": "tbl-1", "content": "| Item | Price |\n|------|-------|\n| Apple | 1.00 |"}
				]
			}],
			"model": "m",
			"usage_info": {"pages_processed": 1}
		}`))
	}))
	defer srv.Close()

	ocr := &MistralOCR{apiKey: "k", model: "m", baseURL: srv.URL, client: srv.Client()}
	result, err := ocr.Extract(context.Background(), []byte("img"), "image/jpeg")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if strings.Contains(result.Markdown, "[tbl-1]") {
		t.Errorf("markdown still contains placeholder: %q", result.Markdown)
	}
	if !strings.Contains(result.Markdown, "| Apple | 1.00 |") {
		t.Errorf("markdown missing table content: %q", result.Markdown)
	}
	if len(result.Tables) != 1 || result.Tables[0].ID != "tbl-1" {
		t.Errorf("Tables: got %+v, want one entry with ID tbl-1", result.Tables)
	}
}

// TestMistralOCR_ReplacesTablePlaceholders_ExtensionSuffix verifies the
// replacement works when the placeholder includes the file extension
// (e.g. "[tbl-1.html](tbl-1.html)" for HTML table_format) even though
// the table's id field is just "tbl-1".
func TestMistralOCR_ReplacesTablePlaceholders_ExtensionSuffix(t *testing.T) {
	result := replaceTablePlaceholders(
		"Before [tbl-2.html](tbl-2.html) after",
		[]Table{{ID: "tbl-2", Content: "TABLE CONTENT"}},
	)
	if !strings.Contains(result, "TABLE CONTENT") {
		t.Errorf("placeholder with .html suffix not replaced: %q", result)
	}
	if strings.Contains(result, "[tbl-2") {
		t.Errorf("placeholder still present: %q", result)
	}
}

// TestMistralOCR_LeavesUnresolvedPlaceholders verifies that placeholders
// without a matching table are left alone (we don't want to silently
// lose data the LLM might still be able to interpret).
func TestMistralOCR_LeavesUnresolvedPlaceholders(t *testing.T) {
	result := replaceTablePlaceholders(
		"Before [tbl-99](tbl-99) after",
		[]Table{{ID: "tbl-1", Content: "OTHER"}},
	)
	if !strings.Contains(result, "[tbl-99](tbl-99)") {
		t.Errorf("unresolved placeholder should be left in place: %q", result)
	}
}

// TestMistralOCR_PagesParam verifies the optional pages parameter is
// included in the request when set, and omitted otherwise.
func TestMistralOCR_PagesParam(t *testing.T) {
	cases := []struct {
		name     string
		pages    []int
		wantIn   []string
		wantNotIn []string
	}{
		{
			name:    "no pages restriction: omitted from request",
			pages:   nil,
			wantNotIn: []string{`"pages":`},
		},
		{
			name:  "single page",
			pages: []int{0},
			wantIn: []string{`"pages":[0]`},
		},
		{
			name:  "multiple pages, non-contiguous",
			pages: []int{0, 2, 4},
			wantIn: []string{`"pages":[0,2,4]`},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var gotBody string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				buf := make([]byte, r.ContentLength)
				_, _ = r.Body.Read(buf)
				gotBody = string(buf)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"pages":[{"index":0,"markdown":"x"}],"model":"m","usage_info":{"pages_processed":1}}`))
			}))
			defer srv.Close()

			ocr := (&MistralOCR{apiKey: "k", model: "m", baseURL: srv.URL, client: srv.Client()}).WithPages(c.pages...)
			if _, err := ocr.Extract(context.Background(), []byte("img"), "image/jpeg"); err != nil {
				t.Fatalf("Extract: %v", err)
			}
			for _, want := range c.wantIn {
				if !strings.Contains(gotBody, want) {
					t.Errorf("body missing %s\nbody: %s", want, gotBody)
				}
			}
			for _, wantNot := range c.wantNotIn {
				if strings.Contains(gotBody, wantNot) {
					t.Errorf("body should not contain %s\nbody: %s", wantNot, gotBody)
				}
			}
		})
	}
}

// TestMistralOCR_PerBlockConfidenceInResult verifies that the parsed
// OCRResult has per-block confidence populated when the API returns
// word scores.
func TestMistralOCR_PerBlockConfidenceInResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"pages": [{
				"index": 0,
				"markdown": "BANANAS ORG  1.78\nTOTAL  1.78",
				"blocks": [
					{"type": "text", "content": "BANANAS ORG  1.78", "top_left_x": 0, "top_left_y": 0, "bottom_right_x": 100, "bottom_right_y": 20},
					{"type": "footer", "content": "TOTAL  1.78", "top_left_x": 0, "top_left_y": 100, "bottom_right_x": 100, "bottom_right_y": 120}
				],
				"confidence_scores": {
					"minimum_page_confidence_score": 0.50,
					"word_confidence_scores": [
						{"word": "BANANAS", "confidence": 0.94},
						{"word": "ORG", "confidence": 0.70},
						{"word": "1.78", "confidence": 0.91}
					]
				}
			}],
			"model": "m",
			"usage_info": {"pages_processed": 1}
		}`))
	}))
	defer srv.Close()

	ocr := &MistralOCR{apiKey: "k", model: "m", baseURL: srv.URL, client: srv.Client()}
	result, err := ocr.Extract(context.Background(), []byte("img"), "image/jpeg")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(result.Blocks) != 2 {
		t.Fatalf("Blocks: got %d, want 2", len(result.Blocks))
	}
	// The text block has 3 matching words; the min is 0.70 (ORG).
	if got := result.Blocks[0].Confidence; got < 0.69 || got > 0.71 {
		t.Errorf("text block confidence: got %f, want ~0.70", got)
	}
	// The footer block has no matching words; confidence is 0 (no signal).
	if got := result.Blocks[1].Confidence; got != 0 {
		t.Errorf("footer block confidence: got %f, want 0 (no matching words)", got)
	}

	// ConfidenceForLine uses per-block signal when available, falls
	// back to MinConfidence (0.50 here) otherwise.
	if got := ConfidenceForLine(result, "BANANAS ORG"); got < 0.69 || got > 0.71 {
		t.Errorf("ConfidenceForLine BANANAS ORG: got %f, want ~0.70", got)
	}
	if got := ConfidenceForLine(result, "TOTAL"); got < 0.49 || got > 0.51 {
		t.Errorf("ConfidenceForLine TOTAL: got %f, want ~0.50 (page min fallback)", got)
	}
}
