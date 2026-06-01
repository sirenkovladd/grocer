package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"code.sirenko.ca/grocer/internal/domain"
)

type QwenProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

func NewQwenProvider(apiKey, model string) *QwenProvider {
	return &QwenProvider{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://opencode.ai/zen/go/v1",
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
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

func (q *QwenProvider) ParseReceipt(ctx context.Context, photo []byte) (*ParsedReceipt, error) {
	b64 := base64.StdEncoding.EncodeToString(photo)

	prompt := `Analyze this grocery receipt photo and extract the following information in JSON format:
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

Return ONLY the JSON, no other text.`

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

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", q.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+q.apiKey)

	resp, err := q.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("qwen API error: %d %s", resp.StatusCode, string(respBody))
	}

	var qwenResp qwenResponse
	if err := json.Unmarshal(respBody, &qwenResp); err != nil {
		return nil, err
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

	return parseReceiptJSON(text)
}

func (q *QwenProvider) CategorizeItem(ctx context.Context, itemName string, existingCategories []domain.Category) (*Categorization, error) {
	categoriesJSON, _ := json.Marshal(existingCategories)

	prompt := fmt.Sprintf(`Given the item "%s" and these existing categories: %s

Determine the best category. If no existing category fits, suggest a new one.

Return JSON:
{
  "category_id": 123,
  "is_new": false,
  "suggested_name": ""
}

If creating a new category, set category_id to 0 and is_new to true.
Return ONLY the JSON.`, itemName, string(categoriesJSON))

	req := qwenRequest{
		Model:     q.model,
		MaxTokens: 1024,
		Messages: []qwenMessage{
			{Role: "user", Content: prompt},
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", q.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+q.apiKey)

	resp, err := q.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("qwen API error: %d %s", resp.StatusCode, string(respBody))
	}

	var qwenResp qwenResponse
	if err := json.Unmarshal(respBody, &qwenResp); err != nil {
		return nil, err
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

	return parseCategorizationJSON(text)
}
