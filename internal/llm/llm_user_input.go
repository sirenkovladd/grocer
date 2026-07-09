package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/BurntSushi/toml"
)

// userInputReceipt is the on-the-wire shape used for both the TOML
// template that ships with the "Copy schema" button and the JSON fallback
// the backend accepts. Field names match the TOML keys 1:1 so the same
// struct works for both decoders.
type userInputReceipt struct {
	Merchant string             `toml:"merchant" json:"merchant"`
	Date     string             `toml:"date" json:"date"`
	Total    float64            `toml:"total" json:"total"`
	Items    []userInputItem    `toml:"items" json:"items"`
	Header   string             `toml:"header,omitempty" json:"header,omitempty"`
}

type userInputItem struct {
	Name       string  `toml:"name" json:"name"`
	Quantity   float64 `toml:"quantity" json:"quantity"`
	UnitPrice  float64 `toml:"unit_price" json:"unit_price"`
	TotalPrice float64 `toml:"total_price" json:"total_price"`
}

// ParseUserInput parses a TOML or JSON blob into a ParsedReceipt.
// TOML is tried first; on parse failure, JSON is attempted. This matches
// the "Copy schema" UX — the user pastes back what they got from the LLM
// (which was prompted for TOML) but JSON from older LLM sessions is also
// accepted.
func ParseUserInput(content []byte) (*ParsedReceipt, error) {
	if len(bytes.TrimSpace(content)) == 0 {
		return nil, fmt.Errorf("empty content")
	}

	if parsed, err := parseUserInputTOML(content); err == nil {
		return parsed, nil
	} else if parsed, err2 := parseUserInputJSON(content); err2 == nil {
		return parsed, nil
	} else {
		return nil, fmt.Errorf("not valid TOML (%v) or JSON (%v)", err, err2)
	}
}

func parseUserInputTOML(content []byte) (*ParsedReceipt, error) {
	var r userInputReceipt
	if err := toml.Unmarshal(content, &r); err != nil {
		return nil, err
	}
	return toParsedReceipt(&r)
}

func parseUserInputJSON(content []byte) (*ParsedReceipt, error) {
	var r userInputReceipt
	if err := json.Unmarshal(content, &r); err != nil {
		return nil, err
	}
	return toParsedReceipt(&r)
}

// toParsedReceipt converts the wire shape into a ParsedReceipt, applying
// the same fallbacks as ParseReceiptResponse so the matcher/categorizer
// see a consistent shape.
func toParsedReceipt(r *userInputReceipt) (*ParsedReceipt, error) {
	if r.Merchant == "" {
		return nil, fmt.Errorf("merchant is required")
	}
	if len(r.Items) == 0 {
		return nil, fmt.Errorf("at least one item is required")
	}

	date := parseFlexibleDate(r.Date)

	items := make([]ParsedItem, len(r.Items))
	for i, it := range r.Items {
		if it.Name == "" {
			return nil, fmt.Errorf("item %d: name is required", i)
		}
		items[i] = ParsedItem{
			Name:       it.Name,
			Quantity:   it.Quantity,
			UnitPrice:  it.UnitPrice,
			TotalPrice: it.TotalPrice,
		}
		// Same fallback as the auto LLM path: if total is missing, derive
		// from unit * qty. The user might paste a schema where they only
		// filled in one of the two price fields.
		if items[i].TotalPrice == 0 && items[i].UnitPrice != 0 {
			items[i].TotalPrice = items[i].UnitPrice * items[i].Quantity
		}
		if items[i].Quantity == 0 {
			items[i].Quantity = 1
		}
	}

	return &ParsedReceipt{
		Merchant: r.Merchant,
		Date:     date,
		Items:    items,
		Total:    r.Total,
	}, nil
}

// parseFlexibleDate accepts a few common date formats so users don't have
// to guess the right one. Falls back to time.Now() on failure (same as
// ParseReceiptResponse) so a typo doesn't fail the whole apply.
func parseFlexibleDate(s string) time.Time {
	s = trimSpace(s)
	if s == "" {
		return time.Now()
	}
	formats := []string{
		"2006-01-02",
		time.RFC3339,
		"2006-01-02T15:04:05",
		"01/02/2006",
		"2006/01/02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Now()
}
