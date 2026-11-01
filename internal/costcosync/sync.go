// Package costcosync continues: the sync operation that pulls Costco
// warehouse receipts through the user's logged-in Brave tab and commits
// them directly as receipts. See cdp.go for the CDP client.
package costcosync

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"code.sirenko.ca/grocer/internal/costco"
	"code.sirenko.ca/grocer/internal/receipt"
	"code.sirenko.ca/grocer/internal/store"
)

// ordersURL is the Orders & Purchases SPA. Loading it in the tab warms
// the logged-in session (and its short-lived API token) that the
// in-tab GraphQL fetch relies on.
const ordersURL = "https://www.costco.ca/myaccount/#/app/e442e6e6-2602-4a39-937b-8b28b4457ed3/ordersandpurchases"

// gqlEndpoint is Costco's order-history GraphQL gateway.
const gqlEndpoint = "https://ecom-api.costco.com/ebusiness/order/v1/orders/graphql"

// tokenLookupJS finds the short-lived API token in the tab's session
// storage by prefix — the suffix is not stable, so it is never
// hardcoded. Evaluates to null when logged out.
const tokenLookupJS = `Object.keys(sessionStorage).find(k => k.indexOf('authToken_') === 0) || null`

// Cfg configures one sync run.
type Cfg struct {
	CDP      *CDP
	Store    *store.Store
	Parser   *receipt.Parser
	OwnerID  uint64
	Since    string // M/D/YYYY list start
	Until    string // M/D/YYYY list end
	Timezone *time.Location
	DryRun   bool // fetch + parse only; no store writes
}

// FailedBarcode is one receipt that could not be imported.
type FailedBarcode struct {
	Barcode string
	Err     string
}

// Summary counts one sync run.
type Summary struct {
	Created int
	Skipped int
	Failed  []FailedBarcode
}

// ShouldPush reports whether the run changed the store (a no-op run
// must not push a snapshot and churn the watcher).
func ShouldPush(created int) bool { return created > 0 }

// BuildListEval returns the in-tab JS that runs the date-range list
// query and resolves with the raw response text.
func BuildListEval(token, since, until string) string {
	return buildFetchEval(token, costco.ListQueryDoc, map[string]any{
		"startDate": since, "endDate": until,
		"text": "Last 3 Months", "documentType": "all", "documentSubType": "all",
	})
}

// BuildDetailEval returns the in-tab JS that runs the single-barcode
// detail query and resolves with the raw response text.
func BuildDetailEval(token, barcode string) string {
	return buildFetchEval(token, costco.DetailQueryDoc, map[string]any{
		"barcode": barcode, "documentType": "warehouse",
	})
}

func buildFetchEval(token, query string, vars map[string]any) string {
	must := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			panic(err)
		}
		return string(b)
	}
	headers := map[string]string{
		"Content-Type":           "application/json-patch+json",
		"costco-x-authorization": "Bearer " + token,
		"costco.env":             "ecom",
		"costco.service":         "restOrders",
		"costco-x-wcs-clientid":  costco.WCSClientID,
		"client-identifier":      costco.ClientIdentifier,
	}
	return `(async () => {
  const resp = await fetch(` + must(gqlEndpoint) + `, {
    method: 'POST',
    credentials: 'include',
    headers: ` + must(headers) + `,
    body: JSON.stringify({query: ` + must(query) + `, variables: ` + must(vars) + `}),
  });
  const text = await resp.text();
  if (!resp.ok) throw new Error('costco HTTP ' + resp.status + ' ' + text.slice(0, 200));
  return text;
})()`
}

// gqlError extracts the first GraphQL error message, or "".
func gqlError(raw string) string {
	var env struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal([]byte(raw), &env) == nil && len(env.Errors) > 0 {
		return env.Errors[0].Message
	}
	return ""
}

// pollToken waits for the login token to appear in the tab (the SPA
// needs a moment after navigation), polling every 500ms up to 30s.
func pollToken(ctx context.Context, tab *Tab) (string, error) {
	deadline := time.Now().Add(30 * time.Second)
	for {
		tok, err := tab.Evaluate(tokenLookupJS, false)
		if err != nil {
			return "", fmt.Errorf("costco session read: %w", err)
		}
		if tok != "" && tok != "null" {
			return tok, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("not logged into Costco in Brave (no session token after 30s) — log into costco.ca and retry")
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// Run pulls the receipt list + details through the Brave tab and
// imports each detail as a receipt (or counts it in DryRun mode).
func Run(ctx context.Context, cfg Cfg) (Summary, error) {
	var sum Summary

	// Remember the user's current tab so focus can go straight back.
	var prevActive string
	if targets, err := cfg.CDP.ListTargets(); err == nil {
		for _, t := range targets {
			if t.Type == "page" {
				prevActive = t.ID
				break
			}
		}
	}
	tab, err := cfg.CDP.NewTab()
	if err != nil {
		return sum, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = tab.Close()
		}
	}()
	if prevActive != "" {
		_ = cfg.CDP.ActivateTab(prevActive) // best effort; cosmetic only
	}
	if err := tab.Navigate(ordersURL); err != nil {
		return sum, err
	}
	token, err := pollToken(ctx, tab)
	if err != nil {
		return sum, err
	}
	listRaw, err := tab.Evaluate(BuildListEval(token, cfg.Since, cfg.Until), true)
	if err != nil {
		return sum, fmt.Errorf("costco list fetch: %w", err)
	}
	if msg := gqlError(listRaw); msg != "" {
		return sum, fmt.Errorf("costco list: %s", msg)
	}
	rows, err := costco.ParseList([]byte(listRaw))
	if err != nil {
		return sum, fmt.Errorf("costco list: %w", err)
	}
	type fetched struct{ barcode, raw string }
	raws := make([]fetched, 0, len(rows))
	for _, r := range rows {
		bc := r.TransactionBarcode.String()
		dRaw, err := tab.Evaluate(BuildDetailEval(token, bc), true)
		if err != nil {
			sum.Failed = append(sum.Failed, FailedBarcode{bc, err.Error()})
			continue
		}
		if msg := gqlError(dRaw); msg != "" {
			sum.Failed = append(sum.Failed, FailedBarcode{bc, msg})
			continue
		}
		raws = append(raws, fetched{bc, dRaw})
	}
	// Browser work is done: close the tab before any store write.
	// A close error here is best-effort (the tab may already be gone);
	// the fetched data is in hand either way.
	_ = tab.Close()
	closed = true

	loc := cfg.Timezone
	if loc == nil {
		loc = time.UTC
	}
	for _, it := range raws {
		d, err := costco.ParseDetail([]byte(it.raw))
		if err != nil {
			sum.Failed = append(sum.Failed, FailedBarcode{it.barcode, err.Error()})
			continue
		}
		parsed := costco.ConvertDetail(d, loc)
		if parsed.Barcode == "" {
			sum.Failed = append(sum.Failed, FailedBarcode{it.barcode, "missing transactionBarcode"})
			continue
		}
		ext := costco.ExternalID(parsed.Barcode)
		if _, err := cfg.Store.FindReceiptByExternalID(ext); err == nil {
			sum.Skipped++
			continue
		}
		if _, err := cfg.Store.FindProposalByExternalID(ext); err == nil {
			sum.Skipped++
			continue
		}
		if cfg.DryRun {
			sum.Created++
			continue
		}
		llmParsed := costco.ToLLMParsed(parsed, costco.MerchantName)
		items, merchantID, _ := cfg.Parser.BuildForImport(ctx, llmParsed)
		if _, err := cfg.Parser.CommitImportAsReceipt(ctx, cfg.OwnerID, merchantID, parsed.Date.Unix(), parsed.TotalCents, ext, items); err != nil {
			sum.Failed = append(sum.Failed, FailedBarcode{parsed.Barcode, err.Error()})
			continue
		}
		sum.Created++
	}
	return sum, nil
}
