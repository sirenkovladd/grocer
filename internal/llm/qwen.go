package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"code.sirenko.ca/grocer/internal/domain"
)

// ZenAnthropicProvider is a generic Anthropic-compatible client for the
// opencode.ai/zen/go gateway. It works for any model served via the
// /v1/messages endpoint with the Anthropic API format, including:
//
//   - Qwen 3.6 Plus / 3.7 Plus / 3.7 Max
//   - MiniMax M2.7 / M3
//   - MiMo V2.5 / V2.5 Pro
//   - GLM 5.1 / 5.2
//
// Construct it via NewQwenProvider or NewMinimaxProvider.
type ZenAnthropicProvider struct {
	*BaseProvider
}

// NewQwenProvider returns a Zen Anthropic client configured for a Qwen model.
// The model is taken from the constructor argument (typically the LLM_MODEL env var).
func NewQwenProvider(apiKey, model string) *ZenAnthropicProvider {
	return &ZenAnthropicProvider{
		BaseProvider: NewBaseProvider(apiKey, model, "https://opencode.ai/zen/go/v1"),
	}
}

// NewMinimaxProvider returns a Zen Anthropic client configured for a
// MiniMax model. Default model is "minimax-m3" if the argument is empty.
func NewMinimaxProvider(apiKey, model string) *ZenAnthropicProvider {
	if model == "" {
		model = "minimax-m3"
	}
	return &ZenAnthropicProvider{
		BaseProvider: NewBaseProvider(apiKey, model, "https://opencode.ai/zen/go/v1"),
	}
}

type zenAnthropicRequest struct {
	Model     string            `json:"model"`
	MaxTokens int               `json:"max_tokens"`
	Messages  []zenAnthropicMsg `json:"messages"`
	System    string            `json:"system,omitempty"`
	Stream    bool              `json:"stream,omitempty"`
}

type zenAnthropicMsg struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type zenAnthropicImage struct {
	Type   string                  `json:"type"`
	Source zenAnthropicImageSource `json:"source"`
}

type zenAnthropicImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type zenAnthropicText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type zenAnthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// ParseReceipt parses a receipt image. Legacy image-in entry point.
func (q *ZenAnthropicProvider) ParseReceipt(ctx context.Context, photo []byte) (*ParsedReceipt, error) {
	b64 := encodeImageToBase64(photo)
	prompt := buildReceiptPrompt()

	req := zenAnthropicRequest{
		Model:     q.model,
		MaxTokens: 4096,
		System:    "You are a receipt parser. Return only valid JSON.",
		Messages: []zenAnthropicMsg{
			{
				Role: "user",
				Content: []any{
					zenAnthropicImage{
						Type: "image",
						Source: zenAnthropicImageSource{
							Type:      "base64",
							MediaType: "image/jpeg",
							Data:      b64,
						},
					},
					zenAnthropicText{Type: "text", Text: prompt},
				},
			},
		},
	}

	text, err := q.callAnthropic(ctx, "/messages", req)
	if err != nil {
		return nil, fmt.Errorf("zen-anthropic request: %w", err)
	}
	return ParseReceiptResponse(text)
}

// ParseReceiptStream is not supported for the Zen Anthropic path.
func (q *ZenAnthropicProvider) ParseReceiptStream(ctx context.Context, photo []byte) (<-chan StreamChunk, error) {
	return nil, fmt.Errorf("streaming not supported for zen-anthropic provider")
}

// ParseReceiptFromText extracts structured JSON from pre-OCR'd text.
func (q *ZenAnthropicProvider) ParseReceiptFromText(ctx context.Context, ocr *OCRResult) (*ParsedReceipt, error) {
	if ocr == nil {
		return nil, fmt.Errorf("nil OCR result")
	}
	prompt := buildReceiptFromTextPrompt(ocr)
	text, err := q.callAnthropicText(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("zen-anthropic text request: %w", err)
	}
	return ParseReceiptResponse(text)
}

// ParseReceiptFromTextStream extracts structured JSON from pre-OCR'd text
// using a streaming text-only chat completion. The Anthropic API streams
// text as SSE events of the form:
//
//	event: content_block_delta
//	data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"..."}}
//
// We forward only the `text` field of each `content_block_delta` to the
// channel, in reading order. The connection is closed on `message_stop`
// or any parse / transport error.
func (q *ZenAnthropicProvider) ParseReceiptFromTextStream(ctx context.Context, ocr *OCRResult) (<-chan StreamChunk, error) {
	if ocr == nil {
		return nil, fmt.Errorf("nil OCR result")
	}
	prompt := buildReceiptFromTextPrompt(ocr)
	return q.streamMessages(ctx, zenAnthropicRequest{
		Model:     q.model,
		MaxTokens: 4096,
		System:    "You are a receipt parser. Return only valid JSON.",
		Messages: []zenAnthropicMsg{
			{Role: "user", Content: prompt},
		},
		Stream: true,
	})
}

// CategorizeItem is unchanged.
func (q *ZenAnthropicProvider) CategorizeItem(ctx context.Context, itemName string, existingCategories []domain.Category) (*Categorization, error) {
	prompt := buildCategorizationPrompt(itemName, existingCategories)
	text, err := q.callAnthropicText(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("zen-anthropic request: %w", err)
	}
	return parseCategorizationResponse(text)
}

// setAnthropicHeaders sets the headers required by the Anthropic Messages API.
// Both are mandatory per the spec: x-api-key for auth and anthropic-version
// to pin the API behavior. Some Anthropic-compatible proxies (like the
// opencode.ai Zen gateway) reject requests missing either, typically with 401.
func setAnthropicHeaders(req *http.Request, apiKey string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
}

func (q *ZenAnthropicProvider) callAnthropic(ctx context.Context, endpoint string, req zenAnthropicRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", q.baseURL+endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	setAnthropicHeaders(httpReq, q.apiKey)

	resp, err := q.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("API error: %d %s", resp.StatusCode, string(respBody))
	}

	var ar zenAnthropicResponse
	if err := json.Unmarshal(respBody, &ar); err != nil {
		return "", fmt.Errorf("unmarshal: %w", err)
	}
	for _, c := range ar.Content {
		if c.Type == "text" {
			return c.Text, nil
		}
	}
	return "", fmt.Errorf("no text content in response")
}

func (q *ZenAnthropicProvider) callAnthropicText(ctx context.Context, userPrompt string) (string, error) {
	req := zenAnthropicRequest{
		Model:     q.model,
		MaxTokens: 4096,
		System:    "You are a receipt parser. Return only valid JSON.",
		Messages: []zenAnthropicMsg{
			{Role: "user", Content: userPrompt},
		},
	}
	return q.callAnthropic(ctx, "/messages", req)
}

// streamMessages runs an Anthropic streaming chat completion. It returns a
// channel of StreamChunk carrying the text deltas in reading order. The
// channel is closed when the upstream emits message_stop (or any terminal
// event) or when an error occurs.
//
// Anthropic's SSE stream looks like:
//
//	event: message_start
//	data: {"type":"message_start", ...}
//
//	event: content_block_start
//	data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}
//
//	event: content_block_delta
//	data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"..."}}
//
//	... more content_block_delta events ...
//
//	event: content_block_stop
//	data: {"type":"content_block_stop","index":0}
//
//	event: message_stop
//	data: {"type":"message_stop"}
//
// We emit one StreamChunk per content_block_delta with a non-empty text
// payload. ping events and unknown events are ignored. The terminal
// message_stop / message_delta event causes the goroutine to exit and the
// channel to close. Errors are emitted as StreamChunk{Error: ...}.
func (q *ZenAnthropicProvider) streamMessages(ctx context.Context, req zenAnthropicRequest) (<-chan StreamChunk, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", q.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	setAnthropicHeaders(httpReq, q.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := q.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: %d %s", resp.StatusCode, string(errBody))
	}

	ch := make(chan StreamChunk, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		// Anthropic events can be up to a few KB each.
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		var eventType string
		for scanner.Scan() {
			line := scanner.Text()

			// SSE event/data lines are separated by blank lines.
			if line == "" {
				eventType = ""
				continue
			}
			if strings.HasPrefix(line, "event: ") {
				eventType = strings.TrimPrefix(line, "event: ")
				continue
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "" || data == "[DONE]" {
				continue
			}

			// Only forward text deltas. message_stop terminates the stream.
			if eventType == "content_block_delta" {
				var ev struct {
					Type  string `json:"type"`
					Delta struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"delta"`
				}
				if err := json.Unmarshal([]byte(data), &ev); err != nil {
					ch <- StreamChunk{Error: fmt.Errorf("decode stream event: %w", err)}
					return
				}
				if ev.Delta.Text != "" {
					ch <- StreamChunk{Text: ev.Delta.Text}
				}
				continue
			}
			if eventType == "message_stop" || eventType == "error" {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			ch <- StreamChunk{Error: fmt.Errorf("stream read: %w", err)}
		}
	}()

	return ch, nil
}
