# Ticket 12 — Frontend: Mobile responsive tables (CSS + minor markup)

**Type:** Frontend (CSS, plus markup updates on the pages that use `.responsive-table`)
**Files:** `client/styles/main.css`, `client/pages/receipts.ts` (card grid), `client/pages/items.ts` (already in ticket 09), `client/pages/item-detail.ts` (already in ticket 10)
**Depends on:** Ticket 05 (CSS), 09, 10
**Blocks:** —

## Goal

Make all data tables usable on mobile (≤ 768px wide). Tables collapse to a stacked "card" layout where each row is a block with labeled fields.

## Current state

The CSS in ticket 05 already includes:
```css
@media (max-width: 768px) {
  .responsive-table thead { display: none; }
  .responsive-table tr { display: block; ... }
  .responsive-table td { display: flex; justify-content: space-between; ... }
  .responsive-table td::before { content: attr(data-label); ... }
}
```

This ticket makes sure all data tables use the `.responsive-table` class and that every `<td>` has a `data-label` attribute.

## Pages to verify

| Page | Has table? | Has responsive-table class? | Has data-labels? |
|------|-----------|---------------------------|------------------|
| `/receipts` | No (cards) | N/A | N/A |
| `/receipts/{id}` | Yes (items) | Set in ticket 08 | Set in ticket 08 |
| `/proposals/{id}` | Yes (items) | Out of scope (proposal flow unchanged) | N/A |
| `/items` | Yes | Set in ticket 09 | Set in ticket 09 |
| `/items/{id}` | Yes (purchase history) | Set in ticket 10 | Set in ticket 10 |
| `/merchants` | Yes (price history) | **Needs to be added** | **Needs data-labels** |
| `/categories` | No (tree) | N/A | N/A |
| `/analysis` | No (charts) | N/A | N/A |

## Implementation

### 1. `client/pages/merchants.ts` (table update)
Add `class: "responsive-table"` to the existing `<table>` and `data-label` to each `<td>`.

The current table:
```ts
table(
  tr(th("Date"), th("Price")),
  ...comparison.val.map((c: any) =>
    tr(
      td(new Date(c.date).toLocaleDateString()),
      td(`$${c.price.toFixed(2)}`),
    )
  ),
),
```

New:
```ts
table({ class: "responsive-table" },
  tr(th("Date"), th("Price")),
  ...comparison.val.map((c: any) =>
    tr(
      td({ "data-label": "Date" }, formatDate(new Date(c.date).getTime() / 1000)),
      td({ "data-label": "Price", class: "money" }, formatMoney(c.price * 100)),
    )
  ),
),
```

### 2. CSS additions (if needed)

Test the existing CSS in browser at 375px width. If issues are found, add:
- Padding adjustments for the stacked card
- Hide the `.receipts-grid` columns on small screens (single column)
- Hide the `.filter-bar` labels on small screens (use placeholders only)

## Acceptance criteria

- [ ] At 375px wide (mobile), every data table is readable.
- [ ] No horizontal scroll on any data table.
- [ ] Stacked cards show field labels clearly.
- [ ] Desktop layout (≥ 769px) is unchanged.
- [ ] `mise run build_client` passes.

## Open questions (brainstorm in fresh session)

- **Filter bar on mobile:** The new filter bar (tickets 07, 09) has multiple inputs. Do they stack on mobile? **Recommend: yes, `flex-wrap: wrap` already handles it but verify.**
- **Cards on mobile:** The receipt card grid (1 col on desktop) should stay 1 col on mobile. No change.
- **Sidebar on mobile:** The 200px sidebar eats half the screen on mobile. **Known issue, defer** to a future mobile layout ticket.
- **Charts on mobile:** Chart.js handles its own responsiveness. Verify on mobile.
- **Touch targets:** All buttons should be ≥ 44px tall on mobile for accessibility. Check.

## Verification commands

```bash
mise run build_client
```

Manual: open the app in a mobile-sized browser window (DevTools responsive mode at 375px). Check each page.

## Decisions log

- 2026-07-09: **Only `merchants.ts` needed markup changes** (per the audit table in the ticket). Other tables were already covered in tickets 08 (receipt detail), 09 (items list), 10 (item detail).
- 2026-07-09: **Merchants page also gets `formatDate` and `formatMoney`** (consistency win beyond the ticket's strict scope). Same dollar→cents conversion as item-detail (analysis endpoint wart).
- 2026-07-09: **No new CSS needed.** The `.responsive-table` rules added in ticket 05 are sufficient. The `.filter-bar { flex-wrap: wrap }` rule handles stacked filter inputs on small screens.
- 2026-07-09: **Mobile sidebar and touch target accessibility are out of scope** (known follow-ups, per the ticket's open questions).
