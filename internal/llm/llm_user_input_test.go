package llm

import (
	"strings"
	"testing"
)

func TestParseUserInput_TOML(t *testing.T) {
	input := `merchant = "Costco"
date = "2026-07-09"
total = 142.87

[[items]]
name = "Organic Bananas"
quantity = 1.234
unit_price = 0.99
total_price = 1.22

[[items]]
name = "Whole Milk 2L"
quantity = 1
unit_price = 4.99
total_price = 4.99
`
	p, err := ParseUserInput([]byte(input))
	if err != nil {
		t.Fatalf("ParseUserInput: %v", err)
	}
	if p.Merchant != "Costco" {
		t.Errorf("merchant = %q, want Costco", p.Merchant)
	}
	if len(p.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(p.Items))
	}
	if p.Items[0].Name != "Organic Bananas" {
		t.Errorf("item[0].name = %q", p.Items[0].Name)
	}
	if p.Items[0].Quantity != 1.234 {
		t.Errorf("item[0].quantity = %v", p.Items[0].Quantity)
	}
	if p.Items[0].UnitPrice != 0.99 {
		t.Errorf("item[0].unit_price = %v", p.Items[0].UnitPrice)
	}
	if p.Items[0].TotalPrice != 1.22 {
		t.Errorf("item[0].total_price = %v", p.Items[0].TotalPrice)
	}
	if p.Total != 142.87 {
		t.Errorf("total = %v", p.Total)
	}
}

func TestParseUserInput_JSON(t *testing.T) {
	input := `{
		"merchant": "Walmart",
		"date": "2026-07-08",
		"total": 25.99,
		"items": [
			{"name": "Milk", "quantity": 1, "unit_price": 4.49, "total_price": 4.49}
		]
	}`
	p, err := ParseUserInput([]byte(input))
	if err != nil {
		t.Fatalf("ParseUserInput: %v", err)
	}
	if p.Merchant != "Walmart" {
		t.Errorf("merchant = %q", p.Merchant)
	}
	if len(p.Items) != 1 {
		t.Fatalf("items = %d", len(p.Items))
	}
	if p.Items[0].Name != "Milk" {
		t.Errorf("item.name = %q", p.Items[0].Name)
	}
}

func TestParseUserInput_TOMLTriesFirstAndFallsBackToJSON(t *testing.T) {
	// If TOML parsing fails, we silently try JSON. Make sure that path works.
	jsonInput := `{"merchant": "Test", "date": "2026-01-01", "items": [{"name": "x", "quantity": 1, "unit_price": 1.0, "total_price": 1.0}], "total": 1.0}`
	p, err := ParseUserInput([]byte(jsonInput))
	if err != nil {
		t.Fatalf("ParseUserInput: %v", err)
	}
	if p.Merchant != "Test" {
		t.Errorf("merchant = %q", p.Merchant)
	}
}

func TestParseUserInput_RejectsMalformed(t *testing.T) {
	_, err := ParseUserInput([]byte("this is not toml or json { broken"))
	if err == nil {
		t.Fatal("expected error for malformed input")
	}
	if !strings.Contains(err.Error(), "not valid TOML") {
		t.Errorf("error should mention TOML/JSON: %v", err)
	}
}

func TestParseUserInput_RejectsEmpty(t *testing.T) {
	_, err := ParseUserInput([]byte(""))
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestParseUserInput_RejectsMissingMerchant(t *testing.T) {
	input := `date = "2026-07-09"
total = 1.0
[[items]]
name = "x"
quantity = 1
unit_price = 1.0
total_price = 1.0
`
	_, err := ParseUserInput([]byte(input))
	if err == nil {
		t.Fatal("expected error for missing merchant")
	}
}

func TestParseUserInput_RejectsNoItems(t *testing.T) {
	input := `merchant = "x"
date = "2026-07-09"
total = 0
`
	_, err := ParseUserInput([]byte(input))
	if err == nil {
		t.Fatal("expected error for empty items")
	}
}

func TestParseUserInput_FillsInTotalFromUnitAndQuantity(t *testing.T) {
	input := `merchant = "x"
date = "2026-07-09"
total = 9.98
[[items]]
name = "y"
quantity = 2
unit_price = 4.99
`
	p, err := ParseUserInput([]byte(input))
	if err != nil {
		t.Fatalf("ParseUserInput: %v", err)
	}
	if p.Items[0].TotalPrice != 9.98 {
		t.Errorf("expected total_price 9.98 from unit*qty, got %v", p.Items[0].TotalPrice)
	}
}

func TestParseUserInput_FillsInQuantityWhenZero(t *testing.T) {
	input := `merchant = "x"
date = "2026-07-09"
total = 4.99
[[items]]
name = "y"
unit_price = 4.99
total_price = 4.99
`
	p, err := ParseUserInput([]byte(input))
	if err != nil {
		t.Fatalf("ParseUserInput: %v", err)
	}
	if p.Items[0].Quantity != 1 {
		t.Errorf("expected quantity 1 fallback, got %v", p.Items[0].Quantity)
	}
}

func TestParseUserInput_AcceptsRFC3339Date(t *testing.T) {
	input := `merchant = "x"
date = "2026-07-09T15:30:00Z"
total = 0
[[items]]
name = "y"
quantity = 1
unit_price = 1.0
total_price = 1.0
`
	p, err := ParseUserInput([]byte(input))
	if err != nil {
		t.Fatalf("ParseUserInput: %v", err)
	}
	if p.Date.Year() != 2026 || p.Date.Month() != 7 || p.Date.Day() != 9 {
		t.Errorf("date = %v, want 2026-07-09", p.Date)
	}
}

func TestParseUserInput_FallsBackToNowOnBadDate(t *testing.T) {
	input := `merchant = "x"
date = "not a date"
total = 0
[[items]]
name = "y"
quantity = 1
unit_price = 1.0
total_price = 1.0
`
	p, err := ParseUserInput([]byte(input))
	if err != nil {
		t.Fatalf("ParseUserInput should not fail on bad date: %v", err)
	}
	if p.Date.IsZero() {
		t.Error("expected non-zero date (fallback to now)")
	}
}
