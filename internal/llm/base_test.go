package llm

import (
	"strings"
	"testing"
)

// TestReceiptPromptsContainCriticalRules asserts the parsing prompts carry
// the rules needed to handle real-world receipt quirks: weighted items,
// attached discounts, bottle deposits, transaction-metadata lines, and
// Canadian bottle-deposit / recycle-fee patterns.
//
// If a rule gets accidentally dropped, receipts start silently mis-parsing
// again (e.g. "Card $3.69 Save -2.00" becomes a separate negative item).
func TestReceiptPromptsContainCriticalRules(t *testing.T) {
	ocr := &OCRResult{
		Markdown: "test",
	}
	textPrompt := buildReceiptFromTextPrompt(ocr)
	imagePrompt := buildReceiptPrompt()

	// Every rule must appear in BOTH prompts so the two paths produce
	// consistent output regardless of which one is used.
	for _, rule := range []string{
		// "copy the printed price exactly" rule (the one that fixes 8.45)
		"copy the printed price",
		"EXACTLY as it appears",
		// weighted item unit-price line
		"@ $1.96/kg",
		// discount on the preceding item (Save / Coupon / More Rewards)
		`Card $X.XX Save`,
		"Coupon -$Y",
		"More Rewards -$Y",
		// bottle deposit / recycle fee
		"*DEPOSIT",
		"*RECYCLE FEE",
		// explicit example: milk + deposit + recycle
		"Dld 2% Fltrd Milk",
		// footer lines that must not become items
		"Balance Due",
		"Sub Total",
		// transaction metadata that must not become items
		"TRANSACTION RECORD",
		// loyalty / rewards noise
		"Points Earned",
		// confirms the discounted total (not a separate item)
		`"Card $X.XX"`,
	} {
		if !strings.Contains(textPrompt, rule) {
			t.Errorf("text prompt missing rule %q", rule)
		}
		if !strings.Contains(imagePrompt, rule) {
			t.Errorf("image prompt missing rule %q", rule)
		}
	}
}
