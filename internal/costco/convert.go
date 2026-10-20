package costco

import (
	"math"
	"strconv"
	"strings"
	"time"

	"code.sirenko.ca/grocer/internal/llm"
)

// MerchantName is the canonical Grocer merchant for Costco warehouse
// imports. Warehouse location is preserved per-receipt in the import
// metadata (WarehouseName), not by forking merchants — otherwise the
// spending-by-merchant chart fragments into one slice per warehouse.
const MerchantName = "Costco Wholesale"

// ParsedLine is a normalized line item ready for catalog matching.
type ParsedLine struct {
	// Name is the display name, e.g. "KIRKLAND ALMOND BUTTER 1.2KG".
	Name string
	// SKU is the Costco item number, e.g. "1234567". Kept for
	// alias-matching and for the UI subtitle.
	SKU string
	// Quantity in retail units (1 for eaches, 0.875 for weighted).
	Quantity float64
	// UnitPriceCents and TotalCents are the extended-line economics.
	UnitPriceCents  int64
	TotalPriceCents int64
}

// ParsedReceipt is the normalized form of a DetailReceipt.
type ParsedReceipt struct {
	Barcode       string
	WarehouseName string
	Date          time.Time
	TotalCents    int64
	SubTotalCents int64
	TaxCents      int64
	Lines         []ParsedLine
	Tenders       []ParsedTender
}

// ParsedTender is a normalized payment line.
type ParsedTender struct {
	Description string
	AmountCents int64
}

// roundCents converts dollars to cents with banker's-intent rounding
// (math.Round, not truncation — see dollarsToCents in the receipt
// package for why truncation undercharges $19.99 by a cent).
func roundCents(dollars float64) int64 {
	return int64(math.Round(dollars * 100))
}

// ConvertDetail normalizes a Costco detail receipt into ParsedReceipt.
// Rules:
//   - Name prefers itemDescription01, falls back to itemDescription02,
//     then the French descriptions, then "Costco item <SKU>".
//   - Quantity defaults to 1. Costco's `unit` field is a free-text
//     quantity ("2", "0.875", "1CT"); unparseable values fall back to 1
//     and the economics still balance via total.
//   - Unit price prefers itemUnitPriceAmount; when absent or zero,
//     unit = total / qty. Fuel lines carry fuelUnitQuantity instead.
//   - Total is the printed extended amount; unit is derived so that
//     qty * unit == total after rounding.
func ConvertDetail(d *DetailReceipt, loc *time.Location) *ParsedReceipt {
	if loc == nil {
		loc = time.UTC
	}
	date, err := ParseCostcoDate(d.TransactionDateTime.String(), loc)
	if err != nil {
		if d.TransactionDate.String() != "" {
			date, _ = ParseCostcoDate(d.TransactionDate.String(), loc)
		}
	}
	if date.IsZero() {
		date = time.Now().In(loc)
	}

	out := &ParsedReceipt{
		Barcode:       d.TransactionBarcode.String(),
		WarehouseName: strings.TrimSpace(d.WarehouseName),
		Date:          date,
		TotalCents:    roundCents(d.Total.Float64()),
		SubTotalCents: roundCents(d.SubTotal.Float64()),
		TaxCents:      roundCents(d.Taxes.Float64()),
	}
	for _, it := range d.ItemArray {
		out.Lines = append(out.Lines, convertLine(it))
	}
	for _, t := range d.TenderArray {
		desc := strings.TrimSpace(t.TenderDescription)
		if desc == "" {
			desc = strings.TrimSpace(t.TenderTypeName.String())
		}
		if desc == "" {
			desc = strings.TrimSpace(t.TenderTypeCode.String())
		}
		out.Lines = append(out.Lines, ParsedLine{}) // placeholder removed below
		out.Lines = out.Lines[:len(out.Lines)-1]
		out.Tenders = append(out.Tenders, ParsedTender{
			Description: desc,
			AmountCents: roundCents(t.AmountTender.Float64()),
		})
	}
	return out
}

func convertLine(it DetailItem) ParsedLine {
	name := strings.TrimSpace(it.ItemDescription01)
	if name == "" {
		name = strings.TrimSpace(it.ItemDescription02)
	}
	if name == "" {
		name = strings.TrimSpace(it.FrenchItemDesc1)
	}
	if name == "" {
		name = strings.TrimSpace(it.FrenchItemDesc2)
	}
	sku := strings.TrimSpace(it.ItemNumber.String())
	if name == "" {
		if sku != "" {
			name = "Costco item " + sku
		} else {
			name = "Costco item"
		}
	}

	qty := parseQty(it.Unit.String(), it.FuelUnitQuantity.Float64())
	total := roundCents(it.Amount.Float64())
	var unit int64
	if up := roundCents(it.ItemUnitPriceAmount.Float64()); up != 0 {
		unit = up
		// Re-derive qty from economics when the printed unit quantity
		// disagrees (e.g. unit="1CT" with a 2-pack total): prefer a qty
		// that makes qty*unit == total so the line balances.
		if qty != 0 {
			if want := float64(total) / float64(up); want > 0 && !almostEqual(want, qty) && want < 1000 {
				// Keep the printed qty for weighted/fuel lines where the
				// unit price is per-kg/per-litre; only adjust eaches.
				if it.FuelUnitQuantity.Float64() == 0 && it.ItemUnitPriceAmount.Float64() != 0 && qty == 1 && want != 1 {
					qty = roundQty(want)
				}
			}
		}
	} else if qty != 0 {
		unit = int64(math.Round(float64(total) / qty))
	} else {
		qty = 1
		unit = total
	}
	if qty == 0 {
		qty = 1
	}
	return ParsedLine{
		Name:            name,
		SKU:             sku,
		Quantity:        qty,
		UnitPriceCents:  unit,
		TotalPriceCents: total,
	}
}

func parseQty(unit string, fuelQty float64) float64 {
	if fuelQty != 0 {
		return fuelQty
	}
	unit = strings.TrimSpace(unit)
	if unit == "" {
		return 1
	}
	// Common Costco forms: "2", "1CT", "0.875KG", "2 x 1.5L".
	// Extract the leading number.
	i := 0
	for i < len(unit) && (unit[i] == '.' || unit[i] == '-' || (unit[i] >= '0' && unit[i] <= '9')) {
		i++
	}
	if i == 0 {
		return 1
	}
	var v float64
	for _, f := range []func(string) (float64, error){parseFloatStrict} {
		if q, err := f(unit[:i]); err == nil && q != 0 {
			v = q
			break
		}
	}
	if v == 0 {
		return 1
	}
	return v
}

func parseFloatStrict(s string) (float64, error) {
	// Local helper to keep parseQty's fallback chain explicit.
	return parseFloat(s)
}

func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}

func almostEqual(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 0.001
}

func roundQty(q float64) float64 {
	return math.Round(q*1000) / 1000
}

// ToLLMParsed adapts a ParsedReceipt to the llm.ParsedReceipt shape so
// the existing matcher + categorizer pipeline (receipt.Parser) can be
// reused without forking item-matching logic.
func ToLLMParsed(p *ParsedReceipt, merchant string) *llm.ParsedReceipt {
	items := make([]llm.ParsedItem, len(p.Lines))
	for i, l := range p.Lines {
		items[i] = llm.ParsedItem{
			Name:       l.Name,
			Quantity:   l.Quantity,
			UnitPrice:  float64(l.UnitPriceCents) / 100.0,
			TotalPrice: float64(l.TotalPriceCents) / 100.0,
		}
	}
	return &llm.ParsedReceipt{
		Merchant: merchant,
		Date:     p.Date,
		Items:    items,
		Total:    float64(p.TotalCents) / 100.0,
	}
}

// SummaryLine is the list-view row returned by the preview endpoint.
type SummaryLine struct {
	Barcode        string  `json:"barcode"`
	Date           string  `json:"date"`
	WarehouseName  string  `json:"warehouseName"`
	Total          float64 `json:"total"`
	TotalItemCount int     `json:"totalItemCount"`
	AlreadyImported bool   `json:"alreadyImported"`
}

// SummarizeList converts list rows to preview rows. alreadyImported is
// filled by the API layer via the store lookup.
func SummarizeList(rows []ListReceipt) []SummaryLine {
	out := make([]SummaryLine, 0, len(rows))
	for _, r := range rows {
		out = append(out, SummaryLine{
			Barcode:        r.TransactionBarcode.String(),
			Date:           r.TransactionDateTime.String(),
			WarehouseName:  r.WarehouseName,
			Total:          r.Total.Float64(),
			TotalItemCount: r.TotalItemCount,
		})
	}
	return out
}
