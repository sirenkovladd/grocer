# Proposal Page — Reparse Options & External LLM Override

**Date:** 2026-07-09
**Status:** Planning
**Scope:** Backend (`internal/api`, `internal/receipt`, `internal/llm`) + Web client (`client/pages/proposal.ts`, `client/styles/main.css`)

## Problem

When the auto-parse pipeline produces a bad proposal — wrong items, missed items, garbled prices, total off — the user has limited recourse:

1. They can edit items inline (one at a time, tedious for a long receipt)
2. They can click "Retry Parsing" which re-runs the full pipeline (OCR + LLM) with the same photo
3. They can delete the proposal and start over

What's missing:

- **Visibility into the OCR markdown** — the pipeline always runs Mistral OCR, but the markdown is stored and never shown. When the LLM gets the parse wrong, the user has no way to verify whether OCR was clean and the LLM misread it, or whether OCR itself was wrong.
- **Bypass OCR** — for difficult receipts (wrinkled, weird fonts, faded), the LLM may do better receiving the image directly. The current pipeline can only go OCR-first.
- **External LLM fallback** — the user might want to use a different LLM (ChatGPT, Claude with vision) for hard receipts, send the receipt image, get a structured response, and paste it back. Today there is no way to do this; they'd have to delete the proposal and start over.
- **Non-deterministic LLM retry** — LLM extraction is stochastic. Re-running just the LLM step (with the same OCR input) often gives a different result. The current "Retry Parsing" re-runs OCR too, which is wasted work and may change the OCR markdown for the worse.

## Goals

1. Show OCR markdown in a collapsible details panel on the proposal page (default hidden), with a copy button.
2. Provide three explicit reparse engine options: **Full**, **LLM (existing OCR)**, **LLM (image)**. All three always re-run the LLM step (no "OCR only" mode — the user explicitly asked for this).
3. Provide two copy-to-clipboard utilities: **Copy schema** (TOML template) and **Copy prompt to LLM** (self-contained prompt the user can paste into ChatGPT/Claude with the image).
4. Provide a paste-back textarea where the user can paste the external LLM's TOML/JSON response and click Apply. The backend parses it, runs the matcher + categorizer, and updates the proposal in place.
5. The new "Reparse & Override" section is **always visible** on the proposal page (every state: loading, parsing, pending, failed, approved). Buttons are disabled during `loading`/`parsing`.

## Non-goals

- A standalone "OCR only" mode that stops after OCR (deliberately omitted per user direction)
- Re-running just OCR (the user asked for OCR + LLM together, since OCR is usually deterministic)
- Multi-receipt parsing in one go
- An "undo" / version history of parse attempts
- Backend storage of the prompt content (it lives in the client; if the rules change, we ship a client update)
- Showing OCR markdown during `parsing` state — it's not relevant mid-parse

## Design

### TOML schema (canonical, used for both the "Copy schema" template and the "Copy prompt to LLM" content)

```toml
# Receipt parser output
merchant = "store name as printed on the receipt"
date = "YYYY-MM-DD"
total = 25.99

[[items]]
name = "item name as printed on the receipt"
quantity = 1
unit_price = 2.99
total_price = 2.99
```

The backend also accepts JSON in the same shape, for users who already have the legacy LLM JSON response. Detection: try TOML first, fall back to JSON.

### Prompt to LLM (literal text the "Copy prompt to LLM" button copies)

```
You are a grocery receipt parser. I will attach a photo of a receipt.
Extract the contents in TOML format. Output ONLY the TOML — no commentary, no code fences, no explanation.

Schema:
merchant = "store name as printed on the receipt"
date = "YYYY-MM-DD"
total = 25.99

[[items]]
name = "item name as printed on the receipt"
quantity = 1
unit_price = 2.99
total_price = 2.99

Rules (must follow):

PRICE EXTRACTION (most important):
- For non-weighted items (quantity 1), copy the printed price EXACTLY as it appears on the receipt into BOTH unit_price and total_price. If the receipt says $8.45, output $8.45 (not $8.44, not $8.4, not $8.5).
- For weighted items, the printed line total (the number on the same line as the item name) goes in total_price. Copy it exactly. The per-kg/lb number from the next line goes in unit_price. Example: "BANANAS 1.72" then "0.875 kg @ $1.96/kg" → quantity 0.875, unit_price 1.96, total_price 1.72.

ATTACHED LINES (consume into the preceding item, do NOT output as separate items):
- "Card $X.XX Save -Y" / "Save -$Y" / "Coupon -$Y" → discount on preceding item. Reduce that item's total_price by Y.
- "*DEPOSIT", "*RECYCLE FEE", "*ENV FEE", "*BOTTLE DEPOSIT" → price adder on preceding item. ADD to total_price.
- "0.875 kg @ $1.96/kg" or "$1.96/lb" → unit-price info for preceding item, NOT a separate item.

EXCLUDE entirely (do not emit):
- "Sub Total", "Subtotal", "Tax", "GST", "PST", "HST", "Total", "Balance Due", "Credit", "Cash", "Change", "Payment", "VISA", "MASTERCARD", "DEBIT".
- Card numbers (e.g. "XXXXX6431"), transaction IDs, "TYPE: Purchase", "ACCT:", "REF#", "AUTHOR#", "AID:", "APPROVED", "NO SIGNATURE".
- Loyalty / rewards, store numbers, addresses, phone numbers.

GENERAL:
- quantity can be a decimal for weighted items (e.g. 0.875 for 875g).
- If unsure about a line, skip it rather than guess.
- Return ONLY the TOML.
```

This prompt is the same content as the server-side `buildReceiptPrompt` rules, reformatted to ask for TOML output and bundled with the schema. The parsing rules text is a near-verbatim copy of the server's `receiptParsingRules` constant — kept identical so server-extracted and externally-extracted results follow the same constraints.

### Backend

#### `internal/receipt/parser.go` — refactor + new helper

Extract a `parseWithEngine` helper from `runParsePipeline` and `ParseReceiptAsync`:

```go
// parseWithEngine dispatches to the right LLM call based on engine.
// ocr may be nil for engine=llm_image. For engine=llm_text, ocr must be
// non-nil (caller validates and returns 400 otherwise).
func (p *Parser) parseWithEngine(ctx context.Context, photo []byte, ocr *llm.OCRResult, engine string) (*llm.ParsedReceipt, error) {
    switch engine {
    case "llm_image":
        return p.llm.ParseReceiptFromImage(ctx, photo) // new thin wrapper, or use existing ParseReceipt
    case "llm_text":
        if ocr == nil { return nil, errors.New("llm_text requires OCR markdown") }
        return p.llm.ParseReceiptFromText(ctx, ocr)
    case "full", "":
        // full path: re-run OCR + LLM
        if p.ocrEngine != nil {
            fresh, err := p.ocrEngine.Extract(ctx, photo, "image/jpeg")
            if err != nil { return nil, fmt.Errorf("OCR: %w", err) }
            return p.llm.ParseReceiptFromText(ctx, fresh)
        }
        return p.llm.ParseReceiptFromImage(ctx, photo)
    default:
        return nil, fmt.Errorf("unknown engine: %q", engine)
    }
}
```

The existing `ParseReceiptAsync` is updated to call `parseWithEngine` and to support a `streaming` SSE pipeline for each engine. The streaming path uses the same per-chunk JSON partial parse as today but reads from the appropriate stream.

Add a new public method:

```go
// ApplyUserInput builds a proposal from user-supplied TOML/JSON (the
// result of a manual LLM session). The matcher + categorizer run on the
// parsed items so the new proposal has the same item-matching quality as
// the auto pipeline.
func (p *Parser) ApplyUserInput(ctx context.Context, proposalID uint64, parsed *llm.ParsedReceipt, ocr *llm.OCRResult) (*domain.Proposal, error) {
    merchant, err := p.findOrCreateMerchant(parsed.Merchant)
    if err != nil { return nil, err }
    items := p.buildProposalItems(ctx, parsed.Items, ocr)
    proposal := &domain.Proposal{
        ProposalID: proposalID, OwnerID: /* loaded from store */,
        MerchantID: merchant.MerchantID, Merchant: merchant.Name,
        Date: parsed.Date.Unix(), Items: items,
        TotalCents: dollarsToCents(parsed.Total), Status: StatusPending,
    }
    if ocr != nil {
        proposal.OcrMarkdown = ocr.Markdown
        proposal.OcrMinConfidence = float32(ocr.MinConfidence)
    }
    if err := p.store.UpdateProposalParseResult(proposalID, merchant.MerchantID, merchant.Name, parsed.Date.Unix(), dollarsToCents(parsed.Total), items); err != nil {
        return nil, err
    }
    return p.store.GetProposal(proposalID)
}
```

Note: `ApplyUserInput` reuses `buildProposalItems`, so items get the same string-similarity + OCR-confidence auto-match behavior as the auto pipeline. New items get categorized via the LLM the same way.

#### `internal/llm/llm.go` — add user-input parser

Add a helper that takes TOML or JSON bytes and returns a `*ParsedReceipt`:

```go
// ParseUserInput parses a TOML or JSON blob into a ParsedReceipt.
// TOML is tried first; on parse failure, JSON is tried.
func ParseUserInput(content []byte) (*ParsedReceipt, error) {
    if p, err := parseUserInputTOML(content); err == nil { return p, nil }
    return parseUserInputJSON(content)
}
```

Both helpers share the same internal struct shape. A new `github.com/BurntSushi/toml` dependency is added to `go.mod`.

The internal `ParsedReceipt` struct already has the right fields (`Merchant`, `Date`, `Items`, `Total`). The TOML schema mirrors it 1:1. Date is parsed as RFC3339-or-YYYY-MM-DD (be lenient — try multiple formats).

#### `internal/api/proposals.go` — endpoint changes

**Extend `handleReparseProposal`:**

```go
type reparseRequest struct {
    Engine string `json:"engine"` // "full" | "llm_text" | "llm_image", default "full"
}
```

Validation:
- `engine=llm_text` requires `proposal.OcrMarkdown != ""`. Otherwise 400 `{"error":"no OCR result; use engine=full or engine=llm_image"}`.
- For other engines, behavior as before. Engine `"full"` (or empty) re-runs OCR + LLM.

The handler logic:
1. Validate engine
2. Reset proposal via `store.ResetProposalForReparse(id)`
3. Load photo bytes via `photoStore.Get(photoURL)`
4. For `engine=llm_text`: reuse existing `OcrMarkdown`; skip OCR call. For others: pass nil OCR; the pipeline handles it.
5. Kick off `parser.ParseReceiptAsync(...)` in a goroutine as before
6. Return `{ "id": "..." }`

**Add `handleApplyExternal`:**

```go
type applyExternalRequest struct {
    Content string `json:"content"`
}

func (r *Router) handleApplyExternal(w http.ResponseWriter, req *http.Request) {
    id := ... // from path
    var body applyExternalRequest
    json.NewDecoder(req.Body).Decode(&body)
    proposal, err := r.store.GetProposal(id)
    if err != nil { 404 }
    parsed, err := llm.ParseUserInput([]byte(body.Content))
    if err != nil { 400 with the parse error }
    // Use existing OCR result if present, so item matching uses the
    // OCR-confidence gating.
    var ocr *llm.OCRResult
    if proposal.OcrMarkdown != "" {
        ocr = &llm.OCRResult{Markdown: proposal.OcrMarkdown, MinConfidence: float64(proposal.OcrMinConfidence)}
    }
    updated, err := r.parser.ApplyUserInput(req.Context(), id, parsed, ocr)
    if err != nil { 500 }
    writeJSON(w, 200, updated)
}
```

**Routes (in `internal/api/router.go`):**

```go
r.mux.HandleFunc("POST /api/proposals/{id}/apply-external", r.withCORS(r.withAuth(r.withAuditLogging("apply_external")(r.handleApplyExternal))))
```

The existing `POST /api/proposals/{id}/reparse` route stays; the body schema just gets a new optional `engine` field.

### Frontend

#### `client/pages/proposal.ts` — new tools panel

Add a new `renderToolsPanel()` function. It is rendered as a child of the page wrapper, after the main content of every state (loading, parsing, pending, failed, approved). It is wrapped in a function-child so the panel re-renders when state changes.

State additions:
- `showOcrDetails = van.state(false)` — controls details panel expansion
- `tomlInput = van.state("")` — textarea content
- `applyingExternal = van.state(false)` — apply button loading state
- `reparsing = van.state(false)` — generic "any reparse in progress" flag (disabled button group)
- `lastEngine = van.state<string | null>(null)` — which engine is currently running, for spinner placement

`handleRetry` is replaced with `handleReparse(engine: "full" | "llm_text" | "llm_image")`:

```ts
const handleReparse = async (engine: string) => {
  if (!id) return
  error.val = ""
  reparseError.val = ""
  reparsing.val = true
  lastEngine.val = engine
  status.val = "loading"
  streamingItems.val = []
  progressMsg.val = ""
  try {
    await api.post(`/proposals/${id}/reparse`, { engine })
    status.val = "parsing"
    fetchSSE()
  } catch (err) {
    error.val = err instanceof Error ? err.message : "Reparse failed"
    status.val = "failed"
  } finally {
    reparseError.val = ""  // separate from main error
  }
}
```

(The `reparsing` state is reset when the SSE stream reaches `done` or `error`; that requires hooking into the SSE state machine. Implementation: when `status.val` transitions back to `pending` or `failed` after a reparse, the function-child that re-runs on `status.val` clears `reparsing.val`. Simpler: reset `reparsing.val` inside the SSE `done` and `error` event handlers, gated on a "we initiated this" flag.)

`handleApplyExternal()`:

```ts
const handleApplyExternal = async () => {
  if (!id) return
  applyingExternal.val = true
  error.val = ""
  try {
    const updated = await api.post(`/proposals/${id}/apply-external`, { content: tomlInput.val })
    proposal.val = updated
    streamingItems.val = updated.items
    status.val = "pending"
    tomlInput.val = ""
  } catch (err) {
    error.val = err instanceof Error ? err.message : "Apply failed"
  } finally {
    applyingExternal.val = false
  }
}
```

`copyToClipboard(text)` helper: uses `navigator.clipboard.writeText`. Graceful fallback: if `clipboard.writeText` fails, show a temporary "Copy failed" error.

The prompt and schema strings are defined as module-level constants:

```ts
const TOML_SCHEMA = `merchant = "..."
...`
const LLM_PROMPT = `You are a grocery receipt parser. ...
...`
```

Buttons:
- `Copy schema` → `copyToClipboard(TOML_SCHEMA)`
- `Copy prompt to LLM` → `copyToClipboard(LLM_PROMPT)`
- `Copy OCR` (in the OCR details panel) → `copyToClipboard(proposal.val?.ocrMarkdown || "")`

The OCR details panel uses a `<details>`/`<summary>` HTML element for native collapse behavior, or a manual van.state toggle. I'll use a manual `van.state` to keep state in one place and match the rest of the page's style (no native HTML disclosure widget for styling consistency).

#### `client/styles/main.css` — new styles

```css
.tools-panel {
  margin-top: 2rem;
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 1rem 1.25rem;
  background: var(--bg-secondary);
}
.tools-section { margin-top: 1rem; }
.tools-section:first-child { margin-top: 0; }
.tools-section h3 {
  font-size: 0.95rem;
  margin-bottom: 0.5rem;
  color: var(--text-secondary);
  text-transform: uppercase;
  letter-spacing: 0.05em;
}
.reparse-buttons {
  display: flex;
  gap: 0.5rem;
  flex-wrap: wrap;
}
.reparse-buttons button { flex: 0 0 auto; }
.reparse-buttons button[disabled] { opacity: 0.5; cursor: not-allowed; }
.ocr-markdown {
  background: var(--bg-tertiary);
  padding: 0.75rem;
  border-radius: 6px;
  max-height: 300px;
  overflow: auto;
  font-family: var(--font-mono, monospace);
  font-size: 0.85rem;
  white-space: pre-wrap;
}
.external-llm-helpers textarea {
  width: 100%;
  min-height: 140px;
  font-family: var(--font-mono, monospace);
  font-size: 0.85rem;
  padding: 0.5rem;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg-tertiary);
  color: var(--text-primary);
}
.copy-buttons {
  display: flex;
  gap: 0.5rem;
  flex-wrap: wrap;
}
.copy-confirm {
  font-size: 0.8rem;
  color: var(--success, #6ab04c);
  margin-left: 0.5rem;
}
```

(All `var(--*)` references are existing CSS custom properties — no new tokens needed.)

## Files changed

| File | Change |
|------|--------|
| `internal/llm/llm.go` | Add `ParseUserInput` helper |
| `internal/llm/llm_user_input.go` (new) | TOML+JSON parsing into `ParsedReceipt` |
| `internal/receipt/parser.go` | Extract `parseWithEngine`; add `ApplyUserInput` |
| `internal/api/proposals.go` | Extend reparse handler; add `handleApplyExternal` |
| `internal/api/router.go` | Register new route |
| `go.mod` | Add `github.com/BurntSushi/toml` |
| `client/pages/proposal.ts` | New tools panel, `handleReparse`, `handleApplyExternal` |
| `client/styles/main.css` | Styles for tools panel |

## Edge cases

| Case | Behavior |
|------|----------|
| Reparse with `engine=llm_text` but no OCR markdown on proposal | 400 error, frontend shows it inline |
| Reparse during `parsing` | Buttons disabled |
| `apply-external` with empty content | 400 "empty content" |
| `apply-external` with both TOML and JSON parse failing | 400 with the actual parser error |
| `apply-external` when proposal is mid-parse | Allowed (overrides in-flight results). If a real parse completes later it wins — but the SSE stream is owned by the original parse, so a re-trigger is needed to "win". Documented behavior: apply-external commits immediately and is the new source of truth. |
| OCR markdown is empty (e.g. old proposal with no OCR engine at the time) | Details panel shows "No OCR result yet" placeholder |
| `navigator.clipboard.writeText` rejects (insecure context, permissions) | Fallback: select-and-prompt-message OR show a copyable textbox the user can manually select+copy. For now: show a clear inline error and the prompt text in a read-only box they can select from. |
| LLM/OCR backend temporarily unavailable | Existing error path; the inline error in the tools panel shows it. |
| LLM (existing OCR) re-run on a proposal with empty `OcrMarkdown` | 400 from backend; frontend surfaces the message |
| LLM (image) re-run when photo bytes are missing (photo deleted from GCS) | 500 from backend; frontend shows the error inline |
| Date in TOML/JSON in non-ISO format | Lenient: try `2006-01-02`, then RFC3339, then fall back to `time.Now()` (same as server's `ParseReceiptResponse`) |

## Testing

- **Unit test (Go):** `internal/llm/llm_user_input_test.go` covers TOML parse, JSON parse, both-fail, both-succeed (TOML wins), date format variants
- **Unit test (Go):** `internal/receipt/parser_test.go` covers `parseWithEngine` dispatch (with mocked LLM/OCR)
- **Unit test (Go):** `internal/api/proposals_test.go` covers reparse with each engine, apply-external success/failure/empty/invalid
- **Manual (web):**
  - Trigger each of the 3 reparse options on a real proposal
  - Toggle OCR details panel
  - Click each copy button, paste into a text editor to confirm content
  - Paste a real external LLM TOML response, click Apply, verify the items update
  - Apply external on a failed-state proposal
  - Apply external on a pending-state proposal

## Out of scope

- Versioning / undo for apply-external
- Showing diff between previous and new items after apply
- A "regenerate schema" or "regenerate prompt" button (the content is static)
- Syncing the LLM prompt with server-side `buildReceiptPrompt` automatically (the client owns its copy; if the server rules change, the next client build picks them up)
- Multi-photo proposals (always one photo per proposal today)
- Per-engine LLM selection (we use whatever `LLM_PROVIDER` is configured server-side)
