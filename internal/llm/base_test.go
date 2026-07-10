package llm

import (
	"strings"
	"testing"
)

// TestBuildReceiptFromTextPrompt_WithAnnotation verifies that when the
// OCR result carries a pre-extracted AnnotatedReceipt, the prompt:
//  1. Includes the structured block before the full OCR text
//  2. Renders it as a readable text block (not raw JSON)
//  3. Cross-references modifier lines to their owning item by 1-based index
//  4. Still includes the full markdown and parsing rules
func TestBuildReceiptFromTextPrompt_WithAnnotation(t *testing.T) {
	ocr := &OCRResult{
		Markdown: "Walmart\n\nBANANAS ORG  1.78\nMILK 2% 1GAL  4.49\n\nSUBTOTAL  6.27\nTOTAL  6.27",
		Header:   "Walmart\n123 Main St",
		Annotated: &AnnotatedReceipt{
			Merchant: "Walmart",
			Date:     "2026-07-10",
			LineItems: []AnnotatedLineItem{
				{Name: "BANANAS ORG", PriceText: "1.78"},
				{Name: "MILK 2% 1GAL", PriceText: "4.49"},
			},
			Modifiers: []AnnotatedModifier{
				{Text: "Card $3.69 Save -2.00", Kind: "discount", AppliesToIndex: 1},
				{Text: "*DEPOSIT 0.10", Kind: "deposit", AppliesToIndex: 0},
			},
			Totals: AnnotatedTotals{
				SubtotalText: "6.27",
				TotalText:    "6.27",
			},
		},
	}

	prompt := buildReceiptFromTextPrompt(ocr)

	// Header section still present.
	if !strings.Contains(prompt, "HEADER (likely merchant info):") {
		t.Error("prompt missing HEADER section")
	}

	// Annotation block present and clearly delineated.
	if !strings.Contains(prompt, "PRE-EXTRACTED STRUCTURE (use as primary source;") {
		t.Error("prompt missing PRE-EXTRACTED STRUCTURE block")
	}

	// Annotation fields rendered.
	for _, want := range []string{
		"Merchant: Walmart",
		"Date: 2026-07-10",
		"1. BANANAS ORG \u2014 printed price: 1.78",
		"2. MILK 2% 1GAL \u2014 printed price: 4.49",
		"Item 2: \"Card $3.69 Save -2.00\" [kind: discount]", // cross-ref to line item 2
		"Item 1: \"*DEPOSIT 0.10\" [kind: deposit]",
		"Subtotal: 6.27",
		"Total: 6.27",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}

	// Full markdown still present after the annotation block.
	annIdx := strings.Index(prompt, "PRE-EXTRACTED STRUCTURE")
	mdIdx := strings.Index(prompt, "FULL OCR TEXT:")
	rulesIdx := strings.Index(prompt, receiptParsingRules)
	if !(annIdx < mdIdx && mdIdx < rulesIdx) {
		t.Errorf("section order wrong: annIdx=%d mdIdx=%d rulesIdx=%d (expected ann < md < rules)", annIdx, mdIdx, rulesIdx)
	}

	// Critical parsing rules still present (regression guard for the
	// existing TestReceiptPromptsContainCriticalRules invariants).
	for _, rule := range []string{"copy the printed price", "*DEPOSIT", "Card $X.XX Save"} {
		if !strings.Contains(prompt, rule) {
			t.Errorf("prompt missing critical rule %q", rule)
		}
	}
}

// TestBuildReceiptFromTextPrompt_NoAnnotation verifies the markdown-only
// fallback path: when Annotated is nil, the prompt must not mention
// PRE-EXTRACTED STRUCTURE and must still work from the markdown alone.
func TestBuildReceiptFromTextPrompt_NoAnnotation(t *testing.T) {
	ocr := &OCRResult{
		Markdown: "Walmart\n\nBANANAS ORG  1.78\n\nTOTAL  1.78",
	}
	prompt := buildReceiptFromTextPrompt(ocr)
	if strings.Contains(prompt, "PRE-EXTRACTED STRUCTURE") {
		t.Error("prompt should not contain PRE-EXTRACTED STRUCTURE when Annotated is nil")
	}
	if !strings.Contains(prompt, "FULL OCR TEXT:") {
		t.Error("prompt missing FULL OCR TEXT block")
	}
	if !strings.Contains(prompt, "BANANAS ORG  1.78") {
		t.Error("prompt should still include the raw markdown")
	}
}

// TestFormatAnnotatedReceipt_EmptyModifierList ensures an empty modifiers
// slice is rendered cleanly (no "Modifier lines: (none detected)" line
// or noisy headers).
func TestFormatAnnotatedReceipt_EmptyModifierList(t *testing.T) {
	ann := &AnnotatedReceipt{
		Merchant:  "Costco",
		LineItems: []AnnotatedLineItem{{Name: "HOTDOG", PriceText: "1.50"}},
		Totals:    AnnotatedTotals{TotalText: "1.50"},
	}
	out := formatAnnotatedReceipt(ann)
	if !strings.Contains(out, "Merchant: Costco") {
		t.Errorf("missing merchant, got:\n%s", out)
	}
	if !strings.Contains(out, "1. HOTDOG \u2014 printed price: 1.50") {
		t.Errorf("missing line item, got:\n%s", out)
	}
	if !strings.Contains(out, "Total: 1.50") {
		t.Errorf("missing total, got:\n%s", out)
	}
	if strings.Contains(out, "Modifier lines") {
		t.Errorf("should omit modifier section when none, got:\n%s", out)
	}
}

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
