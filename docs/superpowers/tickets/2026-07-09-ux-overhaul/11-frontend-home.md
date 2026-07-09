# Ticket 11 — Frontend: Home page improvements

**Type:** Frontend (page)
**Files:** `client/pages/home.ts`
**Depends on:** Tickets 04, 05
**Blocks:** —

## Goal

Use relative dates on the home page, add skeletons during initial load, and clean up the proposals/receipts cards to use the new utilities (money format, relative date).

## Current state

`client/pages/home.ts` (full file, ~160 lines):
- Two sections: pending proposals + recent receipts
- Receipt cards: same as `receipts.ts` page (raw ID, etc.)
- Proposal cards: merchant, date, item count, total — already use `toLocaleDateString` and `(cents/100).toFixed(2)`
- "View All" button on receipts section

## What the new page should do

### Layout
- Page header: `h1("Home")` + "Upload Receipt" button (existing)
- Pending proposals section: same structure, but
  - Dates as relative ("3 days ago") when recent, absolute when old
  - Money as `formatMoney`
  - Skeleton during initial load
- Recent receipts section: same structure, but
  - Use the same `ReceiptCard` pattern as `receipts.ts` (merchant name, date, money)
  - Use the enriched endpoint

### Data
- `/api/proposals` (existing) — already has merchant, date, total
- `/api/receipts/enriched` (from ticket 03) — for the recent receipts section

## Implementation sketch

```ts
import { formatDate, formatMoney, formatRelativeDate, indexBy } from "../utils"

// In ProposalCard:
const itemCount = items.length
const total = formatMoney(proposal.totalCents)
const dateStr = proposal.date ? formatRelativeDate(proposal.date) : ""

// In ReceiptCard:
// Reuse the enhanced card from ticket 07.
// To avoid duplication, extract ReceiptCard into a shared component
// (e.g. client/components/receipt-card.ts) and import here.
// OR — copy the card here. Trade-off: duplication vs. one more file.
// Recommend: copy here for now. Future: extract when there are 3+ uses.
```

## Code reuse decision

The `ReceiptCard` component will be needed in three places: `home.ts`, `receipts.ts`, and potentially the item detail page. The cleanest refactor is a shared component file.

**Option A: Extract to `client/components/receipt-card.ts`** — clean, but adds a new file and indirection.
**Option B: Duplicate the small card** — pragmatic, but DRY violation.

**Recommend Option A** if the card body is non-trivial (>30 lines). It's currently small, so Option B is also fine. **Default to Option A** — extract.

If extracting, the component signature:
```ts
export const ReceiptCard = (r: EnrichedReceiptSummary, onClick: () => void) => Element
```

Then `client/pages/receipts.ts` and `client/pages/home.ts` both import it.

## Acceptance criteria

- [ ] Proposal cards show relative dates when recent ("3 days ago"), absolute when older.
- [ ] All money values use `formatMoney`.
- [ ] Recent receipts section uses the enriched card with merchant name.
- [ ] Skeleton rows during initial load (both sections).
- [ ] `mise run build_client` passes.
- [ ] No regression in the proposal approval flow (buttons still work).

## Open questions (brainstorm in fresh session)

- **Skeleton count:** How many skeleton rows to show? **Recommend 3 per section** — feels real without being too long.
- **Section ordering:** Pending proposals first, then recent receipts. Keep that order.
- **"View All" button:** Keep on the home page for receipts (existing behavior).
- **Empty home:** When both sections are empty, what shows? The proposal section already has "No pending proposals" and the receipts section has "No receipts yet". Add a combined empty state with one big "Upload your first receipt" CTA? **Defer — current empty states are fine.**
- **ReceiptCard extraction:** See above. Decide: extract or duplicate.
- **Limit on "Recent" receipts:** Currently 10. Keep that.

## Verification commands

```bash
mise run build_client
```

Manual: visit `/`, verify the layout, click through to a receipt.

## Decisions log

_(Append decisions made during implementation. Format: `- YYYY-MM-DD: <decision> — <reason>`)_
