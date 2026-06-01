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

type KimiProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

func NewKimiProvider(apiKey, model string) *KimiProvider {
	return &KimiProvider{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://opencode.ai/zen/go/v1",
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type imageContent struct {
	Type     string   `json:"type"`
	ImageURL imageURL `json:"image_url"`
}

type imageURL struct {
	URL string `json:"url"`
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (k *KimiProvider) ParseReceipt(ctx context.Context, photo []byte) (*ParsedReceipt, error) {
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

	req := chatRequest{
		Model: k.model,
		Messages: []message{
			{
				Role: "user",
				Content: []any{
					imageContent{
						Type: "image_url",
						ImageURL: imageURL{
							URL: "data:image/jpeg;base64," + b64,
						},
					},
					textContent{
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

	httpReq, err := http.NewRequestWithContext(ctx, "POST", k.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+k.apiKey)

	resp, err := k.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kimi API error: %d %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, err
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("no response from kimi")
	}

	return parseReceiptJSON(chatResp.Choices[0].Message.Content)
}

func (k *KimiProvider) CategorizeItem(ctx context.Context, itemName string, existingCategories []domain.Category) (*Categorization, error) {
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

	req := chatRequest{
		Model: k.model,
		Messages: []message{
			{Role: "user", Content: prompt},
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", k.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+k.apiKey)

	resp, err := k.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kimi API error: %d %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, err
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("no response from kimi")
	}

	return parseCategorizationJSON(chatResp.Choices[0].Message.Content)
}

func parseReceiptJSON(content string) (*ParsedReceipt, error) {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		if len(lines) > 2 {
			content = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

	var parsed struct {
		Merchant string `json:"merchant"`
		Date     string `json:"date"`
		Items    []struct {
			Name       string  `json:"name"`
			Quantity   uint32  `json:"quantity"`
			UnitPrice  float64 `json:"unit_price"`
			TotalPrice float64 `json:"total_price"`
		} `json:"items"`
		Total float64 `json:"total"`
	}

	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, fmt.Errorf("parse receipt JSON: %w", err)
	}

	date, err := time.Parse("2006-01-02", parsed.Date)
	if err != nil {
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
	}

	return &ParsedReceipt{
		Merchant: parsed.Merchant,
		Date:     date,
		Items:    items,
		Total:    parsed.Total,
	}, nil
}

func parseCategorizationJSON(content string) (*Categorization, error) {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		if len(lines) > 2 {
			content = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

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
