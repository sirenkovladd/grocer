package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestZenAnthropic_ParseReceiptFromTextStream verifies that the streaming
// SSE parser correctly extracts text deltas from Anthropic-compatible
// chat completion responses and ignores non-text events.
func TestZenAnthropic_ParseReceiptFromTextStream(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotStreamHeader string
	var gotBody string
	var gotAnthropicVersion string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("x-api-key")
		gotStreamHeader = r.Header.Get("Accept")
		gotAnthropicVersion = r.Header.Get("anthropic-version")
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not support Flusher")
		}

		// Send a realistic Anthropic SSE sequence: message_start, content_block_start,
		// a ping event, three text deltas, content_block_stop, message_delta, message_stop.
		events := []string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"minimax-m3\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":120,\"output_tokens\":0}}}\n\n",
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
			"event: ping\ndata: {\"type\":\"ping\"}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"{\\\"merchant\\\"\"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\": \\\"Walmart\\\"\"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\", \\\"date\\\": \\\"2026-07-09\\\"}\"}}\n\n",
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":42}}\n\n",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		}
		for _, e := range events {
			_, _ = w.Write([]byte(e))
			flusher.Flush()
		}
	}))
	defer srv.Close()

	provider := &ZenAnthropicProvider{
		BaseProvider: NewBaseProvider("test-key", "minimax-m3", srv.URL),
	}

	ocr := &OCRResult{
		Markdown: "# Walmart\n\nBANANAS ORG 1.78\nMILK 4.49\n",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := provider.ParseReceiptFromTextStream(ctx, ocr)
	if err != nil {
		t.Fatalf("ParseReceiptFromTextStream: %v", err)
	}

	// Verify request shape
	if gotPath != "/messages" {
		t.Errorf("path: got %q, want /messages", gotPath)
	}
	if gotAuth != "test-key" {
		t.Errorf("x-api-key: got %q, want %q", gotAuth, "test-key")
	}
	if gotAnthropicVersion != "2023-06-01" {
		t.Errorf("anthropic-version: got %q, want 2023-06-01", gotAnthropicVersion)
	}
	if !strings.Contains(gotStreamHeader, "event-stream") {
		t.Errorf("Accept: got %q, want event-stream", gotStreamHeader)
	}
	if !strings.Contains(gotBody, `"stream":true`) {
		t.Errorf("request body missing stream:true: %s", gotBody)
	}
	if !strings.Contains(gotBody, `"model":"minimax-m3"`) {
		t.Errorf("request body missing model: %s", gotBody)
	}

	// Collect all chunks
	var chunks []string
	for c := range ch {
		if c.Error != nil {
			t.Fatalf("stream error: %v", c.Error)
		}
		chunks = append(chunks, c.Text)
	}

	// Only the three text deltas should be forwarded; the message_start,
	// content_block_start, ping, content_block_stop, message_delta, and
	// message_stop events should be ignored.
	want := "{\"merchant\": \"Walmart\", \"date\": \"2026-07-09\"}"
	got := strings.Join(chunks, "")
	if got != want {
		t.Errorf("streamed text: got %q, want %q", got, want)
	}
	if len(chunks) != 3 {
		t.Errorf("expected 3 text chunks, got %d: %q", len(chunks), chunks)
	}
}

// TestZenAnthropic_StreamHTTPError verifies that a non-200 response is
// surfaced as a Go error (not just a closed channel).
func TestZenAnthropic_StreamHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
	}))
	defer srv.Close()

	provider := &ZenAnthropicProvider{
		BaseProvider: NewBaseProvider("test-key", "minimax-m3", srv.URL),
	}

	_, err := provider.ParseReceiptFromTextStream(context.Background(), &OCRResult{Markdown: "x"})
	if err == nil {
		t.Fatal("expected error for HTTP 429, got nil")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error should mention 429: %v", err)
	}
}

// TestZenAnthropic_StreamNilOCR verifies the input validation.
func TestZenAnthropic_StreamNilOCR(t *testing.T) {
	provider := NewMinimaxProvider("test-key", "minimax-m3")
	_, err := provider.ParseReceiptFromTextStream(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil OCR result")
	}
}
