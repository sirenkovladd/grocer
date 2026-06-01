package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"code.sirenko.ca/grocer/internal/domain"
)

// KimiProvider implements the LLM Provider interface using Kimi API
type KimiProvider struct {
	*BaseProvider
}

// NewKimiProvider creates a new Kimi provider
func NewKimiProvider(apiKey, model string) *KimiProvider {
	return &KimiProvider{
		BaseProvider: NewBaseProvider(apiKey, model, "https://opencode.ai/zen/go/v1"),
	}
}

type kimiChatRequest struct {
	Model    string         `json:"model"`
	Messages []kimiMessage  `json:"messages"`
}

type kimiMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type kimiImageContent struct {
	Type     string      `json:"type"`
	ImageURL kimiImageURL `json:"image_url"`
}

type kimiImageURL struct {
	URL string `json:"url"`
}

type kimiTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type kimiChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// ParseReceipt parses a receipt image using Kimi API
func (k *KimiProvider) ParseReceipt(ctx context.Context, photo []byte) (*ParsedReceipt, error) {
	b64 := encodeImageToBase64(photo)
	prompt := buildReceiptPrompt()

	req := kimiChatRequest{
		Model: k.model,
		Messages: []kimiMessage{
			{
				Role: "user",
				Content: []any{
					kimiImageContent{
						Type: "image_url",
						ImageURL: kimiImageURL{
							URL: "data:image/jpeg;base64," + b64,
						},
					},
					kimiTextContent{
						Type: "text",
						Text: prompt,
					},
				},
			},
		},
	}

	respBody, err := k.doRequest(ctx, "/chat/completions", req)
	if err != nil {
		return nil, fmt.Errorf("kimi request: %w", err)
	}

	var chatResp kimiChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("no response from kimi")
	}

	return parseReceiptResponse(chatResp.Choices[0].Message.Content)
}

// CategorizeItem categorizes an item using Kimi API
func (k *KimiProvider) CategorizeItem(ctx context.Context, itemName string, existingCategories []domain.Category) (*Categorization, error) {
	prompt := buildCategorizationPrompt(itemName, existingCategories)

	req := kimiChatRequest{
		Model: k.model,
		Messages: []kimiMessage{
			{Role: "user", Content: prompt},
		},
	}

	respBody, err := k.doRequest(ctx, "/chat/completions", req)
	if err != nil {
		return nil, fmt.Errorf("kimi request: %w", err)
	}

	var chatResp kimiChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("no response from kimi")
	}

	return parseCategorizationResponse(chatResp.Choices[0].Message.Content)
}
