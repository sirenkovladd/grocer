package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"code.sirenko.ca/grocer/internal/domain"
)

// QwenProvider implements the LLM Provider interface using Qwen API
type QwenProvider struct {
	*BaseProvider
}

// NewQwenProvider creates a new Qwen provider
func NewQwenProvider(apiKey, model string) *QwenProvider {
	return &QwenProvider{
		BaseProvider: NewBaseProvider(apiKey, model, "https://opencode.ai/zen/go/v1"),
	}
}

type qwenRequest struct {
	Model     string        `json:"model"`
	MaxTokens int           `json:"max_tokens"`
	Messages  []qwenMessage `json:"messages"`
}

type qwenMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type qwenImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type qwenImageContent struct {
	Type   string          `json:"type"`
	Source qwenImageSource `json:"source"`
}

type qwenTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type qwenResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// ParseReceipt parses a receipt image using Qwen API
func (q *QwenProvider) ParseReceipt(ctx context.Context, photo []byte) (*ParsedReceipt, error) {
	b64 := encodeImageToBase64(photo)
	prompt := buildReceiptPrompt()

	req := qwenRequest{
		Model:     q.model,
		MaxTokens: 4096,
		Messages: []qwenMessage{
			{
				Role: "user",
				Content: []any{
					qwenImageContent{
						Type: "image",
						Source: qwenImageSource{
							Type:      "base64",
							MediaType: "image/jpeg",
							Data:      b64,
						},
					},
					qwenTextContent{
						Type: "text",
						Text: prompt,
					},
				},
			},
		},
	}

	respBody, err := q.doRequest(ctx, "/messages", req)
	if err != nil {
		return nil, fmt.Errorf("qwen request: %w", err)
	}

	var qwenResp qwenResponse
	if err := json.Unmarshal(respBody, &qwenResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(qwenResp.Content) == 0 {
		return nil, fmt.Errorf("no response from qwen")
	}

	var text string
	for _, c := range qwenResp.Content {
		if c.Type == "text" {
			text = c.Text
			break
		}
	}

	return ParseReceiptResponse(text)
}

// ParseReceiptStream is not implemented for Qwen; falls back to non-streaming.
func (q *QwenProvider) ParseReceiptStream(ctx context.Context, photo []byte) (<-chan StreamChunk, error) {
	return nil, fmt.Errorf("streaming not supported for qwen provider")
}

// CategorizeItem categorizes an item using Qwen API
func (q *QwenProvider) CategorizeItem(ctx context.Context, itemName string, existingCategories []domain.Category) (*Categorization, error) {
	prompt := buildCategorizationPrompt(itemName, existingCategories)

	req := qwenRequest{
		Model:     q.model,
		MaxTokens: 1024,
		Messages: []qwenMessage{
			{Role: "user", Content: prompt},
		},
	}

	respBody, err := q.doRequest(ctx, "/messages", req)
	if err != nil {
		return nil, fmt.Errorf("qwen request: %w", err)
	}

	var qwenResp qwenResponse
	if err := json.Unmarshal(respBody, &qwenResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(qwenResp.Content) == 0 {
		return nil, fmt.Errorf("no response from qwen")
	}

	var text string
	for _, c := range qwenResp.Content {
		if c.Type == "text" {
			text = c.Text
			break
		}
	}

	return parseCategorizationResponse(text)
}
