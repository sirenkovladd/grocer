# Costco auto-sync helper Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a `costco-sync` helper that pulls Costco warehouse receipts through the user's Brave session and commits them directly as receipts, with no server running.

**Architecture:** New Go command `cmd/costco-sync` reuses `internal/store` (snapshot pull/push), `internal/costco` (parse/convert), and `receipt.Parser` (match/categorize/commit) — the same code path as the server's `mode=receipt` import. A small new CDP client drives Brave over `localhost:9222`: throwaway tab → Orders & Purchases → GraphQL in-page → close tab → import → push snapshot. A running server picks the new snapshot up via its existing Pub/Sub watcher.

**Tech Stack:** Go 1.25, `gorilla/websocket` (already in go.mod as indirect — promote to direct), Chrome DevTools Protocol over HTTP + WebSocket, mise tasks.

**Spec:** `docs/superpowers/specs/2026-11-01-costco-sync-design.md`

## Global Constraints

- Single operator: no lock, no concurrent-write handling; last-write-wins is accepted (spec).
- Never commit secrets: Grocer/LLM credentials come from `.env` via mise only; Costco auth is never stored, only read live from the tab.
- No fixed sleeps: all browser waits poll tab state with a timeout and fail loudly.
- Reuse, don't duplicate: import must go through `receipt.Parser.BuildForImport` + `CommitImportAsReceipt`, never a copy of that logic.
- `mise run start_server` is forbidden from agent sessions; verify with `go build` / `go test` only.
- Commits: imperative message, one logical change per commit, only your own files (`git status` first).

## Review Focus

- Brave closed or CDP port unreachable: helper exits non-zero within seconds naming the fix ("open Brave"). Pinned by Task 2 connection-refused test.
- Costco logged out (no `authToken_*` in sessionStorage before timeout): zero imports, message says "log into Costco". Pinned by Task 3 `TestRunPollTimeout`.
- Costco returns a GraphQL `errors` envelope or a list with no `receipts`: fail loudly, import nothing partial. Pinned by Task 3 error-envelope test.
- Re-run with everything already imported: zero store writes and no snapshot push (no watcher churn). Pinned by Task 3 all-skipped test.
- Multiple Grocer users and no `--owner`: refuse and list usernames rather than misattribute receipts. Pinned by Task 4 owner-resolution test.

---

## File structure

- `internal/costco/costco.go` (modify): export the GraphQL documents and header IDs the browser fetch needs. One responsibility: shared Costco API constants.
- `internal/costcosync/cdp.go` (create): minimal CDP client (targets HTTP API + page WebSocket). One responsibility: talk to Brave.
- `internal/costcosync/cdp_test.go` (create): stub CDP server (HTTP + WebSocket) tests.
- `internal/costcosync/sync.go` (create): tab lifecycle, state-wait poll, list/detail fetch, import loop, summary counts. One responsibility: the sync operation.
- `internal/costcosync/sync_test.go` (create): fetch-JS builders, import idempotency, dry-run, push decision.
- `cmd/costco-sync/main.go` (create): flags, env/store/parser wiring, owner/timezone resolution, exit codes. One responsibility: CLI orchestration.
- `mise.toml` (modify): `sync_costco` task.

## Task 1: Export shared Costco query documents

**Files:**
- Modify: `internal/costco/client.go`
- Test: `internal/costco/costco_test.go` (append)

**Interfaces:**
- Consumes: existing unexported `listQuery`, `detailQuery` consts and the two header-ID literals in `doGraphQL`.
- Produces: `costco.ListQueryDoc string`, `costco.DetailQueryDoc string`, `costco.WCSClientID string` (`"e442e6e6-2602-4a39-937b-8b28b4457ed3"`), `costco.ClientIdentifier string` (`"481b1aec-aa3b-454b-b81b-48187e28f205"`) for use by Task 3's JS builders. `client.go` keeps using the same values (no behavior change).

- [ ] **Step 1: Write the failing test** — in `internal/costco/costco_test.go`, assert the four exported symbols are non-empty and the query docs contain `receiptsWithCounts`.
- [ ] **Step 2: Run it to verify it fails.** Run: `go test ./internal/costco/ -run TestExportedQueryDocs -v`. Expected: FAIL (undefined symbols).
- [ ] **Step 3: Implement the exports in `internal/costco/client.go`.** Rename the consts (or alias them) so `doGraphQL` and the new symbols share one literal each; zero behavior change.
- [ ] **Step 4: Run the full package tests.** Run: `go test ./internal/costco/ -v`. Expected: PASS (all 7 existing + 1 new).
- [ ] **Step 5: Commit.** `git add internal/costco/` + `git commit -m "feat: export Costco GraphQL docs and header IDs for reuse"`.

## Task 2: Minimal CDP client with stubbed tests

**Files:**
- Create: `internal/costcosync/cdp.go`
- Test: `internal/costcosync/cdp_test.go`

**Interfaces:**
- Consumes: `costco.*` nothing; stdlib `net/http` + `gorilla/websocket` (promote to direct with `go get github.com/gorilla/websocket@v1.4.2` inside this task).
- Produces: `costcosync.DialCDP(baseURL string) (*CDP, error)`; methods `(*CDP).ListTargets()`, `(*CDP).NewTab() (*Tab)`, `(*Tab).Navigate(url)`, `(*Tab).Evaluate(expr, awaitPromise) (string)`, `(*Tab).Close()`, `(*CDP).ActivateTab(id)`, `(*CDP).Close()` — consumed by Task 3. `Evaluate` returns the CDP `result.value` JSON-encoded (or raw string); CDP-level errors return Go errors naming the method.

- [ ] **Step 1: Write failing tests** against an in-process stub: HTTP handlers for `/json/version`, `/json/list`, `PUT /json/new`, `/json/close/{id}`, plus a `gorilla/websocket` stub speaking CDP (`Page.enable`, `Page.navigate`, `Runtime.evaluate` echo, `Target.activateTarget` — the stub records `activateTarget` params). Tests: `TestTabLifecycle` (list → new → navigate → evaluate returns value → `ActivateTab` forwards the recorded id → close) and `TestDialRefused` (dial `127.0.0.1:1` → error within seconds mentioning the address).
- [ ] **Step 2: Run to verify they fail.** Run: `go test ./internal/costcosync/ -v`. Expected: FAIL (no package).
- [ ] **Step 3: Implement `cdp.go`.** Plain `encoding/json` CDP framing with incrementing numeric ids; 30s HTTP timeout; WebSocket read loop dispatching by id. `Evaluate` sets `returnByValue: true` and passes through `awaitPromise`.
- [ ] **Step 4: Run tests.** Run: `go test ./internal/costcosync/ -v`. Expected: PASS.
- [ ] **Step 5: Commit.** `git add go.mod go.sum internal/costcosync/` + `git commit -m "feat: add minimal CDP client for driving Brave"`.

## Task 3: Sync operation (fetch + import)

**Files:**
- Create: `internal/costcosync/sync.go`
- Test: `internal/costcosync/sync_test.go`

**Interfaces:**
- Consumes: Task 1 (`costco.ListQueryDoc`, `DetailQueryDoc`, `WCSClientID`, `ClientIdentifier`, plus existing `ParseDetail`, `ConvertDetail(d, loc)`, `ToLLMParsed(p, MerchantName)`, `ExternalID(barcode)`); Task 2 (`CDP`/`Tab`); `receipt.Parser.BuildForImport(ctx, *llm.ParsedReceipt)` and `CommitImportAsReceipt(ctx, ownerID, merchantID, dateUnix, totalCents, externalID, items)`; `store` `FindReceiptByExternalID` / `FindProposalByExternalID`.
- Produces: `costcosync.Run(ctx, Cfg) (Summary, error)` with `Cfg{CDP *CDP, Store *store.Store, Parser *receipt.Parser, OwnerID uint64, Since, Until string /* M/D/YYYY */, Timezone *time.Location, DryRun bool}`, `Summary{Created, Skipped int, Failed []FailedBarcode}`, and `FailedBarcode{Barcode, Err string}`; plus pure helpers `BuildListEval(token, since, until)`, `BuildDetailEval(token, barcode)`, `ShouldPush(created int) bool`. Consumed by Task 4.

Fixed values (from recon + `internal/costco/client.go`, mirror verbatim): orders URL `https://www.costco.ca/myaccount/#/app/e442e6e6-2602-4a39-937b-8b28b4457ed3/ordersandpurchases`; GraphQL endpoint `https://ecom-api.costco.com/ebusiness/order/v1/orders/graphql`; headers `Content-Type: application/json-patch+json`, `costco-x-authorization: Bearer <token>`, `costco.env: ecom`, `costco.service: restOrders`, wcs/client IDs from Task 1; list variables `{startDate, endDate, text: "Last 3 Months", documentType: "all", documentSubType: "all"}`; detail variables `{barcode, documentType: "warehouse"}`; token lookup JS `Object.keys(sessionStorage).find(k => k.indexOf('authToken_') === 0)` (prefix match — never hardcode the full key); fetch uses `credentials: 'include'` and surfaces GraphQL `errors` as Go errors; state-wait polls token presence every 500ms up to 30s.

- [ ] **Step 1: Write failing tests.** All `Run`-level tests use the Task 2 stub CDP extended with canned `Runtime.evaluate` responses (list JSON with 2 barcodes; detail JSON per barcode in the `costcoDetailJSON` shape from `internal/api/costco_test.go`), a `store.NewStore()` plus `receipt.NewParser(s, stubProvider{})` where `stubProvider` mirrors `internal/api/costco_test.go` (`CategorizeItem` → `&llm.Categorization{CategoryID: 0, IsNew: false}`, no network), and one seeded `Groceries` category. Tests: `TestBuildListEval` (contains since/until dates, endpoint, Bearer token, `receiptsWithCounts`); `TestRunImports` (first `Run` → `Created:2`); `TestRunIdempotent` (second `Run` → `Created:0, Skipped:2`); `TestRunPollTimeout` (stub never yields a token → error mentioning Costco login, zero imports); `TestRunErrorEnvelope` (list returns `{"errors":[{"message":"denied"}]}` → error, nothing imported); `TestDryRunWritesNothing` (receipt count unchanged after `DryRun:true` run); `TestShouldPush` (`ShouldPush(0) == false`, `ShouldPush(2) == true`).
- [ ] **Step 2: Run to verify they fail.** Run: `go test ./internal/costcosync/ -run 'TestBuild|TestImport|TestError|TestAllSkipped|TestDryRun' -v`. Expected: FAIL (undefined).
- [ ] **Step 3: Implement `sync.go`.** Flow: record current page targets, open tab, immediately re-activate the previously active tab (never steal focus), navigate orders URL, poll token, list fetch, per-barcode detail fetch (sequential), close tab, then for each detail: `ParseDetail` → skip-check via both `Find*ByExternalID` → `ConvertDetail` → `ToLLMParsed` → `BuildForImport` → `CommitImportAsReceipt`. `DryRun` stops after parse/convert (no store writes). Tab closes before any store write.
- [ ] **Step 4: Run package tests.** Run: `go test ./internal/costcosync/ ./internal/costco/ -v`. Expected: PASS.
- [ ] **Step 5: Commit.** `git add internal/costcosync/` + `git commit -m "feat: add Costco sync fetch-and-import operation"`.

## Task 4: CLI wiring, mise task, manual end-to-end

**Files:**
- Create: `cmd/costco-sync/main.go`
- Modify: `mise.toml`
- Test: `internal/costcosync/sync_test.go` (append owner-resolution test) — or a `_test.go` beside main if pure helpers live there; prefer pure `resolveOwner(store, flag) (uint64, error)` in `internal/costcosync` so it is unit-testable.

**Interfaces:**
- Consumes: Task 3 (`Run`, `Cfg`, `Summary`); `store.NewStore`, `store.NewGCloudStorage(ctx, credsFile, bucket, prefix)` (prefix defaults to `"snapshots/"`), `SetSnapshotStorage`, `LoadSnapshot` (fatal on error — fail fast per spec), `SaveSnapshot` (only when `ShouldPush`); `llm.NewKimiProvider/NewQwenProvider/NewMinimaxProvider` keyed off `LLM_PROVIDER` (fatal on unknown — mirror `cmd/server/main.go`); `store.GetUserByUsername`, `store.ListUsers`.
- Produces: flags `--owner` (username; default: the single user when exactly one exists, else fatal listing usernames), `--since` (default `1/01/2025`), `--until` (default today), `--timezone` (default system local), `--brave-port` (default `9222`), `--dry-run`; exit 0 with `created/skipped/failed` summary, exit 1 on any failure; mise task `sync_costco` = `go run ./cmd/costco-sync` (env comes from top-level `[env] _.file = ".env"`).

- [ ] **Step 1: Write the failing test** for `resolveOwner`: single user + empty flag → that user; two users + empty flag → error naming both; unknown `--owner` → error.
- [ ] **Step 2: Run to verify it fails.** Run: `go test ./internal/costcosync/ -run TestResolveOwner -v`. Expected: FAIL.
- [ ] **Step 3: Implement `resolveOwner` + `cmd/costco-sync/main.go`.** Orchestration order: flags → env/store/snapshot pull → LLM provider → parser → CDP dial → `Run` → `SaveSnapshot` iff `ShouldPush(summary.Created)` → print summary. No step requires the Grocer server.
- [ ] **Step 4: Run unit tests + vet + build.** Run: `go test ./internal/costcosync/ ./internal/costco/ -v && go vet ./... && go build ./...`. Expected: all PASS.
- [ ] **Step 5: Manual end-to-end (the real verification).** With Brave open (logged into Costco) and server stopped: `mise run sync_costco --dry-run` (or `go run ./cmd/costco-sync --dry-run`) → expect receipt counts, zero store writes; then live run → expect `created N`; start server, confirm receipts + spending-by-merchant show Costco. Record results in the commit message body.
- [ ] **Step 6: Commit.** `git add cmd/costco-sync/ mise.toml internal/costcosync/` + `git commit -m "feat: add costco-sync helper"`.
