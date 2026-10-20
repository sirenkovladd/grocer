package costco

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ParseList parses the raw body of the date-range receiptsWithCounts
// query. Accepts either the full GraphQL envelope
// ({"data":{"receiptsWithCounts":{...}}}) or the inner
// receiptsWithCounts object (for hand-trimmed pastes).
func ParseList(raw []byte) ([]ListReceipt, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, fmt.Errorf("empty list response")
	}
	var env ListResponse
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("parse list envelope: %w", err)
	}
	if len(env.Data.ReceiptsWithCounts.Receipts) > 0 {
		return env.Data.ReceiptsWithCounts.Receipts, nil
	}
	// Fallback: caller pasted the inner object directly.
	var inner struct {
		Receipts []ListReceipt `json:"receipts"`
	}
	if err := json.Unmarshal(raw, &inner); err == nil && len(inner.Receipts) > 0 {
		return inner.Receipts, nil
	}
	// Fallback 2: caller pasted a bare array.
	var arr []ListReceipt
	if err := json.Unmarshal(raw, &arr); err == nil && len(arr) > 0 {
		return arr, nil
	}
	return nil, fmt.Errorf("no receipts found in list response (want data.receiptsWithCounts.receipts)")
}

// ParseDetail parses the raw body of the single-barcode query. Returns
// the first (and normally only) receipt in the envelope. Accepts the
// full envelope, the inner object, or a bare DetailReceipt for
// hand-trimmed pastes.
func ParseDetail(raw []byte) (*DetailReceipt, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, fmt.Errorf("empty detail response")
	}
	var env DetailResponse
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("parse detail envelope: %w", err)
	}
	if len(env.Data.ReceiptsWithCounts.Receipts) > 0 {
		r := env.Data.ReceiptsWithCounts.Receipts[0]
		return &r, nil
	}
	var inner struct {
		Receipts []DetailReceipt `json:"receipts"`
	}
	if err := json.Unmarshal(raw, &inner); err == nil && len(inner.Receipts) > 0 {
		r := inner.Receipts[0]
		return &r, nil
	}
	var single DetailReceipt
	if err := json.Unmarshal(raw, &single); err == nil && single.TransactionBarcode.String() != "" {
		return &single, nil
	}
	return nil, fmt.Errorf("no receipt found in detail response (want data.receiptsWithCounts.receipts[0])")
}

// ParseMultiDetail parses a paste that contains several detail envelopes
// concatenated or wrapped in an array. The import UI lets users paste
// multiple detail responses at once; this splits them leniently:
//   - JSON array of envelopes -> each element
//   - newline-delimited JSON objects -> each line
//   - single envelope -> one element
func ParseMultiDetail(raw []byte) ([]*DetailReceipt, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, fmt.Errorf("empty detail response")
	}
	// Try array first.
	if strings.HasPrefix(trimmed, "[") {
		var arr []json.RawMessage
		if err := json.Unmarshal(raw, &arr); err != nil {
			return nil, fmt.Errorf("parse detail array: %w", err)
		}
		out := make([]*DetailReceipt, 0, len(arr))
		for i, m := range arr {
			r, err := ParseDetail(m)
			if err != nil {
				return nil, fmt.Errorf("detail[%d]: %w", i, err)
			}
			out = append(out, r)
		}
		return out, nil
	}
	// Try NDJSON (one envelope per line).
	lines := strings.Split(trimmed, "\n")
	if len(lines) > 1 {
		var out []*DetailReceipt
		for _, ln := range lines {
			ln = strings.TrimSpace(ln)
			if ln == "" {
				continue
			}
			r, err := ParseDetail([]byte(ln))
			if err != nil {
				// Not NDJSON — fall through to single parse.
				out = nil
				break
			}
			out = append(out, r)
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	r, err := ParseDetail(raw)
	if err != nil {
		return nil, err
	}
	return []*DetailReceipt{r}, nil
}

// ParseCostcoDate parses the transaction timestamps Costco returns.
// Observed formats include "08/01/2026 14:23:11", "08/01/2026",
// "2026-08-01T14:23:11", and "2026-08-01". Date-only values anchor to
// noon in loc (same convention as the LLM pipeline) so the calendar
// date survives UTC conversion. Full datetimes are interpreted in loc
// when they carry no offset.
func ParseCostcoDate(s string, loc *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty date")
	}
	if loc == nil {
		loc = time.UTC
	}
	withTime := []string{
		"01/02/2006 15:04:05",
		"1/2/2006 15:04:05",
		"01-02-2006 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		time.RFC3339,
		time.RFC3339Nano,
	}
	for _, f := range withTime {
		if t, err := time.ParseInLocation(f, s, loc); err == nil {
			return t, nil
		}
	}
	dateOnly := []string{"01/02/2006", "1/2/2006", "2006-01-02", "2006/01/02"}
	for _, f := range dateOnly {
		if t, err := time.ParseInLocation(f, s, loc); err == nil {
			return time.Date(t.Year(), t.Month(), t.Day(), 12, 0, 0, 0, loc), nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized Costco date %q", s)
}

// ExternalID returns the stable dedupe key for a barcode.
func ExternalID(barcode string) string {
	return "costco:" + strings.TrimSpace(barcode)
}
