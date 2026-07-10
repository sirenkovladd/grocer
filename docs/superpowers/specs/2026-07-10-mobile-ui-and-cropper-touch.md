# Mobile UI and Cropper Touch Fix

**Date:** 2026-07-10
**Type:** Frontend (CSS + TypeScript)
**Status:** Approved

## Problem

Two related symptoms that together make the Grocer app unusable from a phone:

1. **Cropper resize is broken on touch.** The receipt cropper has both `mousedown`/`mousemove` (desktop) and `touchstart`/`touchmove` (mobile) handlers, but the touch handlers only support **moving** the crop box, not resizing it. The desktop code has four corner handles (NW/NE/SW/SE) that you drag to resize; the touch code never checks for those handles and never branches into the resize math. Result: on a phone, you can drag the crop box around but cannot resize it, so a receipt photo can only be cropped to whatever default rectangle the cropper starts with.

2. **No mobile UI.** The app has exactly one `@media (max-width: 768px)` rule, which only collapses the proposal-page layout to a single column. The top nav, table lists, filter bars, and forms are all sized for desktop. On a 390px-wide phone, the nav overflows, tables are unreadable, and the primary action buttons (Approve, Re-parse) are small enough to be hard to tap accurately.

The existing 2026-07-09 UX overhaul spec already accounts for the table side of this (ticket 12, "Mobile responsive tables") but explicitly deferred it as a non-goal. The cropper fix is a separate, unreported bug.

## Goals

- Make the receipt cropper fully usable on a touch device: drag any of the four corners to resize the crop box.
- Make the existing pages usable on a phone: top nav collapses to a hamburger, data tables become stacked cards on narrow screens, primary action buttons are thumb-sized.
- Stay within ~200 lines of change. Larger mobile UI improvements (full-width forms, full-screen modals, 44px touch targets everywhere, sticky bottom action bars) are explicitly deferred.

## Non-goals (out of scope, see follow-up section)

- Full-width form inputs on mobile
- Full-screen modals / sheets on mobile
- 44px touch target minimum for every button (we only bump the proposal page's primary actions)
- Sticky bottom action bar on the proposal page
- Filter bar redesign on narrow screens (current `flex-wrap: wrap` is acceptable for the most common case)
- Charts on mobile (Chart.js handles its own responsiveness; we only verify in manual test)
- PWA / install to home screen
- iPad-specific layout (768px is the breakpoint; iPad portrait gets the desktop layout, which has the screen space for it)

## Design

### 1. Cropper touch resize (fix bug)

**File:** `client/components/cropper.ts`

The desktop code in `mousedown`/`mousemove` handles four corner handles (NW/NE/SW/SE) with a 12px hit area. The `touchstart`/`touchmove` handlers exist but only implement moving the crop box — they skip the corner-handle detection entirely and only branch on `isMoving`.

Changes:

- Port the corner-handle hit detection from `mousedown` into `touchstart`. Iterate the four handles, check if the touch is within the hit area, and if so set `isDragging = true`, `dragHandle = "nw" | "ne" | "sw" | "se"`, record start position, return early.
- Port the four per-handle resize branches from `mousemove` into `touchmove`. The `case "nw" | "ne" | "sw" | "se"` switch that adjusts `cropRect` based on `dx`/`dy` is currently only reached from `mousemove`; add the same logic to `touchmove`.
- Bump the touch hit area to 32px via a separate `touchHandleSize` constant. Keep the desktop hit area at 12px (it's already the right size for a mouse pointer).
- Attach `touchstart` listeners to the four `handle-NW/NE/SW/SE` elements in addition to the crop box, so corner touches are detected even when the hit area extends outside the box bounds.

Output: same blob from `exportCroppedImage` as before. No new error paths — the existing `try/catch` in `processAndUpload` covers the resize math.

### 2. Hamburger menu

**Files:** `client/main.ts`** (Sidebar component), **`client/styles/main.css`**

The current "sidebar" is actually a top nav rendered as `<nav class="sidebar">` with 7 inline links (Home, Receipts, Items, Merchants, Categories, Analysis, Sign out). On a 390px-wide screen these overflow horizontally.

Changes:

- Add a `navOpen` VanJS state in `main.ts` (top-level, next to `currentPath`).
- On screens ≥ 769px: render the existing inline nav (no change from current behavior).
- On screens < 769px: render a hamburger button (`☰`) on the left of the nav row; the nav items move into a slide-in drawer that opens on click.
- The drawer closes on: link click, click outside the drawer, or `Esc` key press.
- The current page is still highlighted via the existing `aria-current` logic — no new state plumbing.

The layout today is `nav.sidebar + div.content`. The hamburger is part of `nav.sidebar`, so the simplest implementation is a CSS-only switch via the existing `@media (max-width: 768px)` block: hide the inline nav, show the button. JS only adds the open/close toggle. About 50 lines of CSS + ~20 lines of JS.

The drawer is a positioned overlay (`position: fixed; top: 0; left: 0; height: 100vh; width: 240px;`) with a semi-transparent backdrop covering the rest of the screen. No slide-in animation — opens/closes instantly. (A 200ms transform could be added later if it feels jarring; for a tool used a few times a week, instant is fine.)

### 3. Apply `.responsive-table` to merchants

**File:** `client/pages/merchants.ts`**

Per ticket 12 from the 2026-07-09 UX overhaul spec, the audit table explicitly lists merchants as the only outstanding page where `.responsive-table` hasn't been applied. The CSS for `.responsive-table` already exists in `main.css` (stacked card layout with `data-label` attributes).

Single page change: add `class: "responsive-table"` to the price-history table and `data-label` to each `<td>`. ~10 lines of markup.

### 4. Primary action button sizing on mobile

**File:** `client/styles/main.css`**

Bump `.approve-btn` and the other primary action buttons on the proposal page to ≥44px tall on mobile (current ~32-36px). Add a new rule to the existing `@media (max-width: 768px)` block:

```css
@media (max-width: 768px) {
  .approve-btn,
  .btn-primary {
    min-height: 44px;
    padding: 0.75rem 1.5rem;
    font-size: 1rem;
  }
}
```

Doesn't change the desktop look.

## Data flow

No backend changes. No new endpoints. No new state shapes. The cropper fix is a pure DOM event handler change; the hamburger is a UI rewire; the table application is a markup change; the button sizing is a CSS addition.

## Error handling

- **Hamburger drawer:** `Esc` key, click outside, or click a link closes it. No new error paths.
- **Cropper touch:** existing `try/catch` in `processAndUpload` covers the resize math. New code paths reuse the existing resize logic.
- **Marquee table:** no error paths; existing `.responsive-table` CSS already handles the markup change.

## Testing

Manual verification in Chrome DevTools responsive mode at 375px and 390px wide:

1. **Cropper:** upload a receipt, verify the four corner handles can be dragged to resize the crop box. Also verify the desktop behavior (12px handles, mouse drag) is unchanged.
2. **Hamburger:** verify the nav items collapse to a single button; tapping the button opens a drawer; the drawer contains all 7 items + sign out; tapping a link navigates and closes the drawer; `Esc` closes the drawer; the current page is highlighted.
3. **Merchants page:** verify the price-history table is readable as stacked cards on mobile.
4. **Approve button:** verify it's at least 44px tall on a phone-sized screen and reachable without scrolling on the proposal page.

Build commands that must pass:

- `mise run build_client`
- `go build ./...`
- `go test ./...`

No new code paths that need unit tests — the changes are CSS, DOM event handlers, and a small UI rewire.

## Out-of-scope follow-up (deferred to a future spec)

After this lands, the most valuable next-round improvements (in rough order of impact for phone use):

1. **Sticky bottom action bar on the proposal page** — keep Approve / Re-parse / Add item always reachable while scrolling items.
2. **44px touch target minimum for all interactive elements** (buttons, selects, filter inputs), not just the primary actions.
3. **Full-width forms on mobile** — `.form-row` and similar rules so manual-receipt entry doesn't feel cramped.
4. **Full-screen modal/sheet on mobile** — currently modals are desktop-sized and can extend past the viewport.
5. **Filter bar disclosure** — collapse the 4-input filter on the receipts list behind a "Filters" button on mobile.

## Risks

- **Touch event handler changes** can introduce regressions in the desktop path. Mitigation: the touch changes only fire on `touchstart`; mouse events are untouched. Manual verification at desktop viewport catches any regression.
- **Hamburger drawer z-index** could conflict with future modals. Mitigation: the drawer uses a high z-index (`1000`) and modals would go above it; this is a one-time risk.
- **The existing `.responsive-table` CSS** was designed before the merchants page was finalized; if the data shape changed, the data-label mapping may be wrong. Mitigation: manual test on the merchants page.

## Open questions

None blocking implementation.
