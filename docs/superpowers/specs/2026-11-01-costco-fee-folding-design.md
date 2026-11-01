# Costco fee folding — design

Date: 2026-11-01. Status: proposed. Approach picked: fold into parent
(see Approaches).

## Intent / why

Costco warehouse details carry per-line adjustments — `ENVIRO FEE`,
`DEPOSIT`, `TPD/...` instant discounts, bare `/<sku>` lines — that the
importer turns into standalone catalog items (e.g. `ENVIRO FEE
CLN/1377112` under Produce). Across 29 receipts that is dozens of junk
items scattering cents across category analytics. The user wants an
item-linked charge to live in that item's price (milk shows $7.01, not
$6.89 + two junk lines), while charges tied to no item stay separate.

## Goals

- Adjustment lines with an identifiable parent product fold into the
  parent's unit price: `newUnit = round((parentTotal + Σ fees) / qty)`.
- No new catalog items for resolvable adjustments; no money lost
  (Σ lines + receipt taxes == receipt total, as today).
- Price history reflects the price actually paid ($7.01 milk, $5.99
  bagels after a $2 instant saving).

## Non-goals

- Receipt-level sales tax stays receipt-level (explicitly rejected).
- No shared "Fees" catalog item; no protobuf schema change.

## Anti-goals (failure even if it "works")

- Dropping an adjustment whose parent cannot be found (it stays a
  separate line, current behavior — totals must reconcile).
- Changing photo-receipt behavior or the existing qty economics for
  weighted/fuel/count lines (pinned by `TestConvertDetail` et al).
- Deleting a catalog item still referenced by any receipt or proposal.

## Constraints

- `ReceiptItem` is `{ItemID, Quantity, UnitPriceCents}` (protobuf) — no
  fee field exists, so folding must go through the unit price.
- Parent quantity resolves first with the existing economics rules;
  folding divides by that qty (works for weighted parents too).
- Detection signal (verified over all 29 real receipts): adjustment
  lines have unit price 0 with nonzero amount; every real product line
  has a nonzero unit price.
- Parent linkage: trailing `/<digits>` → sibling with equal SKU;
  trailing `/<text>` → first sibling (line order) whose name contains
  the text, case-insensitive; no match → orphan, keep current behavior.
  A parent that is itself an adjustment line does not count (orphan).

## Approaches considered

1. **Fold into parent unit price (PICKED).** No schema change, no new
   items for resolvable adjustments, totals reconcile. Accepted cost:
   unit prices become price-paid (user explicitly chose $7.01).
2. **Shared "Fees & deposits" catalog item.** Rejected by the user:
   still a separate receipt line, still skews categories.
3. **Per-line fee field (protobuf + migration).** Rejected: migration
   machinery to move cents around.
4. **Distribute receipt-level sales tax across lines.** Rejected by the
   user: tax stays at receipt level.

## Decided (with operator)

- $7.01 semantics: fees bake into unit price, incl. negative TPD
  discounts lowering it.
- Orphan adjustments (no identifiable parent, e.g. entrance-type fees)
  keep today's behavior: own catalog item + own line.
- Receipt-level taxes unchanged.
- All 29 already-imported receipts rewritten via delete + resync
  (snapshot backup first — established pattern); now-unreferenced fee
  items removed after a reference scan.

## Left to the builder

- Exact code shape in `internal/costco/convert.go` (fold pass after qty
  resolution, before `ToLLMParsed`).
- Orphan-item sweep: reference scan across receipts + proposals, delete
  only zero-reference fee items.
- Verification: full-dataset sweep (all 29 raw details balance and
  reconcile) + focused regression tests (fee fold, TPD discount,
  orphan keeps, weighted parent with fee).
