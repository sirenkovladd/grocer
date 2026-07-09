package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

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

// ParseReceiptFromTextStream is not supported for the Zen Anthropic path.
func (q *ZenAnthropicProvider) ParseReceiptFromTextStream(ctx context.Context, ocr *OCRResult) (<-chan StreamChunk, error) {
	return nil, fmt.Errorf("streaming not supported for zen-anthropic provider")
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

func (q *ZenAnthropicProvider) callAnthropic(ctx context.Context, endpoint string, req zenAnthropicRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", q.baseURL+endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+q.apiKey)

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
