# Costco auto-sync (local Brave script) — design

Date: 2026-11-01. Status: proposed. Approach picked: standalone script (option 1).

## Intent / why

Costco warehouse receipts should show up in Grocer analytics with zero manual
copying. The paste-first flow works but needs DevTools copy-paste per receipt;
server-side fetch is blocked by Costco's bot check plus a ~10-minute bearer
expiry. Recon on 2026-11-01 proved the way through: drive the user's
already-open Brave via its remote-debug port (`localhost:9222`) and run
Costco's own order queries inside the logged-in tab — no bot blocking, 29
warehouse receipts since Jan 2025, line-item details parse cleanly.

## Goals

- One command on the MacBook pulls the full Costco warehouse history and
  commits each visit directly as a receipt (no proposal review), so spending
  and category analytics include Costco.
- Works with the server stopped: the helper opens the same snapshot store
  the server uses (GCS snapshot via `internal/store`), imports with the
  same parser code, and pushes the snapshot back. No running server
  required.
- Re-runs are safe: `costco:<barcode>` idempotency (already server-side)
  skips imported receipts.
- No passwords, bearers, or cookies stored anywhere; auth happens live in the
  user's browser session.

## Non-goals

- No server changes; the remote (luiscup) deployment is untouched and out of
  scope for this flow.
- No background daemon or scheduler; the user runs it periodically.
- No proposal/review path for these imports; no UI changes.

## Anti-goals (failure even if it "works")

- Copying the Brave profile into a second headless browser: profile lock plus
  bot-check binding make it fragile (considered and rejected during recon).
- Leaving stray tabs open in the user's Brave, or hammering Costco with
  parallel detail fetches.
- Secrets in the repo: Grocer credentials via env/flags only.

## Constraints

- Brave open on the Mac with remote debugging (already on `:9222`), logged
  into Costco; Grocer server running locally.
- The fetch trick depends on Costco's current internals, all confirmed live:
  `sessionStorage` `authToken_…` as the `costco-x-authorization` bearer,
  session cookies attached, `costco-x-wcs-clientid` / `client-identifier`
  headers, same GraphQL documents as `internal/costco/client.go`. If Costco
  changes this, the script must fail loudly ("log into Costco and retry"),
  not silently import partial data.
- Import reuses the same Go code as `POST /api/costco/import` with
  `mode=receipt` (`receipt.Parser` + `internal/costco`), so the existing
  matcher + LLM categorizer keep learning aliases/categories. No API
  server involved, no Grocer login needed.
- "Notify the backend" needs no new code: pushing the snapshot to GCS
  triggers the existing `PubSubWatcher` — a running server reloads within
  ~1s; a stopped server picks it up on next boot.
- Concurrency (settled with operator): single user, never two writes at
  once, so the inherited last-write-wins model is a non-issue. The helper
  may run whether the server is up or down; a running server picks up the
  new snapshot via its existing watcher within ~1s.

## Approaches considered

1. **Standalone Bun script driving Brave via CDP (PICKED).** No server
   changes, no stored secrets, matches the "run it periodically" model.
   Needs Brave open and Costco logged in at run time.
2. **Server auto-syncs on startup.** Rejected: couples boot to Brave/login
   state and is dead weight on the remote deploy, which can never reach the
   Mac's browser.
3. **"Sync Costco now" button in the Grocer UI (Mac-only endpoint).**
   Rejected for now: nicest UX but needs a new endpoint plus
   Brave-not-open/logged-out handling; can reuse this script's logic later.

## Decided

- Go helper command + mise task (e.g. `mise run sync_costco`), NOT a Bun
  script: only Go reuses the store/parser/snapshot code without duplicating
  it. CDP via `gorilla/websocket` (already a dependency); throwaway
  `costco.ca` tab closed right after the last Costco fetch (before the
  store import). Focus is returned to the previously active tab
  immediately after opening, so it never steals attention.
- Date range: full history by default (cheap — 29 receipts), optional
  `--since` flag.
- Import `mode=receipt`; owner via `--owner <username>`, defaulting to the
  Grocer user the script logs in as (author's call).
- No fixed sleeps: after navigating to Orders & Purchases, poll tab state
  (auth token present in `sessionStorage`, order rows rendered) and proceed
  as soon as ready, with a timeout that fails loudly.
- "Right access" check at startup, in order: GCS credentials/snapshot
  readable (same `.env` as the server) → Brave reachable on `:9222` →
  Costco logged in (token present). Fail fast with a plain message naming
  what to fix.
- Per-run summary (created / skipped / failed barcodes); non-zero exit on
  failures.

## Left to the builder

- Exact script path, task name, flag parsing; credential input (prefer env
  `GROCER_USER` / `GROCER_PASS`, never committed).
- Detail-fetch pacing and error taxonomy with actionable messages (Brave
  closed / Costco logged out / server down / GraphQL shape changed).
- Manual test run verifying imports land in receipts + spending-by-merchant
  analytics.
