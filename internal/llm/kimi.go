package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

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
	Model    string        `json:"model"`
	Stream   bool          `json:"stream,omitempty"`
	Thinking kimiThinking  `json:"thinking"`
	Messages []kimiMessage `json:"messages"`
}

type kimiThinking struct {
	Type string `json:"type"`
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

type kimiStreamDelta struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

// ParseReceipt parses a receipt image using Kimi API
func (k *KimiProvider) ParseReceipt(ctx context.Context, photo []byte) (*ParsedReceipt, error) {
	b64 := encodeImageToBase64(photo)
	prompt := buildReceiptPrompt()

	req := kimiChatRequest{
		Model:    k.model,
		Thinking: kimiThinking{Type: "disabled"},
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

	return ParseReceiptResponse(chatResp.Choices[0].Message.Content)
}

// ParseReceiptStream starts a streaming receipt parse and returns a channel of text chunks.
func (k *KimiProvider) ParseReceiptStream(ctx context.Context, photo []byte) (<-chan StreamChunk, error) {
	b64 := encodeImageToBase64(photo)
	prompt := buildReceiptPrompt()
	log.Printf("KIMI_STREAM: starting, model=%s, image=%d chars", k.model, len(b64))

	req := kimiChatRequest{
		Model:  k.model,
		Stream: true,
		Thinking: kimiThinking{Type: "enabled"},
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

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", k.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+k.apiKey)

	resp, err := k.client.Do(httpReq)
	if err != nil {
		log.Printf("KIMI_STREAM: request error: %v", err)
		return nil, fmt.Errorf("do request: %w", err)
	}
	log.Printf("KIMI_STREAM: got response, status=%d", resp.StatusCode)

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
		lineCount := 0
		for scanner.Scan() {
			line := scanner.Text()
			lineCount++
			if lineCount%20 == 0 {
				log.Printf("KIMI_STREAM: read %d lines", lineCount)
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return
			}

			var delta kimiStreamDelta
			if err := json.Unmarshal([]byte(data), &delta); err != nil {
				continue
			}
			if len(delta.Choices) > 0 && delta.Choices[0].Delta.Content != "" {
				ch <- StreamChunk{Text: delta.Choices[0].Delta.Content}
			}
		}
		if err := scanner.Err(); err != nil {
			ch <- StreamChunk{Error: err}
		}
	}()

	return ch, nil
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
