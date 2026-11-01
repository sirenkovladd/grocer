# Clickable category legend — short design

Intent: from the Analysis "Category Breakdown" pie, get to "everything I
bought in this category" in one click, and fix the cramped/cropped
canvas legend (20+ categories overflow the 384px card).

## Agreed behavior (approved 2026-11-01)

- Legend becomes HTML under the pie (scrollable, never cropped). Pie
  grows to fill the card (fixed canvas height, `maintainAspectRatio:
  false`).
- Click color box → hide/show that slice (`toggleDataVisibility` +
  `update()`). Same as today's canvas-legend behavior. Hidden rows dim.
- Hover legend row → matching slice highlights (`setActiveElements` +
  tooltip), cleared on mouse leave.
- Hover pie slice → matching legend row highlights (reverse sync via
  pie `onHover`, cleared with `onLeave`).
- Click name/total → Items page pre-filtered: `#/items?category=<id>`.
  Items page seeds its category dropdown from `?category=` (reactive,
  same pattern as Receipts `?item=`). Unknown id → existing
  "No items match your filters" empty state.

## Approaches considered

1. **Custom HTML legend (chosen).** Chart.js canvas legend cannot
   distinguish box-clicks from text-clicks or scroll, so all three
   interactions require HTML. Cost: we own legend rendering (~60 lines).
2. Keep canvas legend + `onClick` navigate. Rejected: cannot tell
   hide-intent from navigate-intent on one click target.
3. Separate "top categories" table with links. Rejected: duplicates the
   legend instead of fixing it; cropping remains.

## Non-goals

- No reverse highlight animation polish beyond a CSS class.
- Hidden-slice state resets on refetch (date/rollup change) — accepted.
- Chunk-size refactor of Chart.js stays out.

## Verification

`tsc --noEmit` clean on touched files, `vite build` clean, then check
in the user's Brave via CDP: legend rows present, click box hides
slice, click text navigates to filtered Items.
