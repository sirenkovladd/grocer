# Costco fee folding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fold item-linked Costco fee/discount lines into their parent product's unit price so no junk catalog items are created.

**Architecture:** One pure pass in `internal/costco/convert.go`: after per-line qty resolution, detect adjustment lines (zero unit price, nonzero amount), link each to its parent (`/<sku>` or name-contains fallback), and reprice the parent as `round((parentTotal + Σ fees) / qty)`, dropping absorbed lines. Same `ConvertDetail` entry point, so the sync helper and the paste-import API both get it with no other changes.

**Tech Stack:** Go 1.25, existing `internal/costco` + `internal/store` only, no new dependencies.

**Spec:** `docs/superpowers/specs/2026-11-01-costco-fee-folding-design.md`

## Global Constraints

- Single operator: no lock, no concurrent-write handling; last-write-wins is accepted.
- Never commit secrets: credentials come from `.env` via mise only.
- Reuse, don't duplicate: folding lives in `ConvertDetail`'s path, never in the sync helper or API layer.
- `mise run start_server` is forbidden from agent sessions; verify with `go test` / `go build` only.
- Never drop money: an adjustment whose parent cannot be found keeps today's behavior (own item + own line).
- Receipt-level sales tax stays receipt-level; photo receipts are untouched.
- Commits: imperative message, one logical change per commit, only your own files (`git status` first). Work continues on branch `costco-sync`.

## Review Focus

- A fee line whose `/<sku>` points at nothing in this receipt (split shipment, data quirk) stays a separate junk item while its siblings fold; totals still reconcile. Pinned by Task 1's orphan test.
- A text link matching two siblings (`TPD/SOCKS` with two sock lines) folds into the first in line order; receipt total still reconciles but the discount may sit on the wrong product. Pinned by Task 1's multi-match test.
- Weighted parent plus fee rounding: `(total + fees) / qty` rounded to cents can drift 1¢ from printed economics on odd weights. Pinned by Task 1's weighted-fee rounding test asserting ±1¢.
- Costco changes fee-line shape (nonzero unit price on fees, renames without slash) and fees silently become standalone items again; totals still reconcile. Pinned by Task 2's full-dataset sweep, which fails loudly on any unbalanced line.
- The orphan-item sweep deletes a catalog item that a concurrent photo approval just referenced. Pinned by Task 3's reference scan across receipts AND proposals immediately before each delete.

---

### Task 1: Fold pass in the converter with unit tests

**Files:**
- Modify: `internal/costco/convert.go`
- Test: `internal/costco/costco_test.go` (append)

**Interfaces:**
- Consumes: `DetailItem` fields (`ItemNumber`, `ItemDescription01/02`, `Unit`, `Amount`, `ItemUnitPriceAmount`, `FuelUnitQuantity`), existing `convertLine`, `roundCents`, `ParsedLine` / `ParsedReceipt` shapes.
- Produces: `foldAdjustments(lines []ParsedLine, raw []DetailItem) []ParsedLine` in `convert.go`, called at the end of `ConvertDetail` before return; `ConvertDetail` keeps its signature and still returns one `ParsedLine` per input line minus absorbed adjustments.

- [ ] **Step 1: Write the failing tests.** In `costco_test.go`, all at `ConvertDetail` level with small inline JSON envelopes (same style as `sampleDetail`):
```go
func TestFoldFeeIntoParent(t *testing.T) // milk 6.89 + fee 0.02 + deposit 0.10 (unit 1, up 0) → 1 line: qty 1, unit 701, total 701
func TestFoldDiscountLowersParent(t *testing.T) // bagels 7.99 + "/ 116218" -2.00 → 1 line: qty 1, unit 599
func TestFoldOrphanStaysSeparate(t *testing.T) // fee with link "9999999" matching nothing → 2 lines out, fee line unchanged (qty 1, unit 2, total 2)
func TestFoldNameFallback(t *testing.T) // "TPD/SOCKS" -5.00 + "CHAMP SOCKS" 19.99 → 1 line @ 1499
func TestFoldMultiMatchFirstWins(t *testing.T) // "TPD/SOCKS" -5.00 + "CHAMP SOCKS" 19.99 + "WOOL SOCKS" 12.99 → SOCKS discount on CHAMP (first), WOOL untouched
func TestFoldWeightedParent(t *testing.T) // thighs 36.39 @ 15.89 (qty 2.29) + deposit 0.60 → unit 1615, qty 2.29, total 3639+60=3699
```
- [ ] **Step 2: Run tests to verify they fail.** Run: `go test ./internal/costco/ -run 'TestFold' -v`. Expected: FAIL (no folding; milk stays 3 lines).
- [ ] **Step 3: Implement `foldAdjustments(lines []ParsedLine, raw []DetailItem) []ParsedLine` + call it in `ConvertDetail`.** Detect: raw `ItemUnitPriceAmount == 0 && Amount != 0`. Link: trailing `/<digits>` → sibling (non-adjustment) with equal `ItemNumber` string; trailing `/<text>` → first non-adjustment sibling whose name contains text (case-insensitive); no `/` or no match, or match is itself an adjustment → orphan (kept verbatim). Reprice: `newUnit = round((parentTotal + Σ feeTotals) / parentQty)` via `math.Round`, `roundCents` for fee amounts. Drop absorbed lines, preserve order of survivors.
- [ ] **Step 4: Run tests to verify they pass.** Run: `go test ./internal/costco/ -v`. Expected: PASS (all existing + 6 new).
- [ ] **Step 5: Commit.** `git add internal/costco/` + `git commit -m "feat: fold item-linked Costco adjustments into parent unit price"`.

### Task 2: Rewrite the 29 imported receipts and verify

**Files:** None in repo (throwaway helper created and deleted within the task).

**Interfaces:**
- Consumes: Task 1's `ConvertDetail` via the unchanged `mise run sync_costco` path; `cmd/backup export` for the pre-rewrite snapshot backup; `store` `List/DeleteReceipt`, `List/DeleteProposal` (same calls as the established wipe pattern).

- [ ] **Step 1: Back up the snapshot.** Run: `go run ./cmd/backup export /tmp/grocer-backup-fee-folding.pb.gz`. Expected: prints size ~15KB.
- [ ] **Step 2: Wipe Costco data and resync.** Write a throwaway `tmp_wipecostco_main.go` (list receipts/proposals, delete those with `ExternalID` prefix `costco:`, save), run it, delete the file, then run `mise run sync_costco`. Expected: `created 29, skipped 0, failed 0`.
- [ ] **Step 3: Verify.** Dump and check: no receipt line named `ENVIRO*`/`DEPOSIT*`/`TPD*`/`BC PST*`/`/ *`; the milk receipt shows `NAT PLUS 2%` qty 1 @ $7.01; full-suite `go test ./...` PASS. Then re-fetch all 29 raw details and run the balance sweep (every line `qty*unit ≈ amount`, every receipt `Σ lines + taxes == total`); any failure stops the task for a ruling.
- [ ] **Step 4: Record.** No commit (data-only task); append the verification output to the task's completion note.

### Task 3: Sweep orphaned fee items, final green

**Files:** None in repo (throwaway helper created and deleted within the task).

**Interfaces:**
- Consumes: Task 2's rewritten snapshot; `store` `ListItems`, `ListReceipts`, `ListProposals`, `DeleteItem` (verify exact names against `internal/store/memdb.go` before use).

- [ ] **Step 1: Reference-scan and delete.** Write a throwaway program: list items whose name starts with `ENVIRO FEE`, `DEPOSIT`, `TPD/`, `BC PST`, or `/`; for each, scan all receipts and proposals for a reference; delete only zero-reference items; save. Run it, delete the file. Expected: prints each deleted name (expect the fee/deposit/TPD set, nothing else).
- [ ] **Step 2: Final verification.** Run: `go test ./... && go vet ./... && go build ./...`. Expected: all PASS (112+ tests), fee names absent from a fresh `snapshot-dump` items list.
- [ ] **Step 3: Record.** No commit (data-only task); completion note carries the deleted-names list.
