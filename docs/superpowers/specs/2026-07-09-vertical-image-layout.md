# Vertical Image Layout

## Problem

Receipts are always vertical (portrait, aspect ratio roughly 3:4 to 9:16).
The current UI treats them as if they were landscape or square, so they
either get cropped (hiding content), pushed to fill the whole screen
(wasting vertical space), or render at full size (slow + heavy). Three
pages are affected:

1. **Receipt detail page** (`/receipts/{id}`) — loads the full-size
   image, renders it as a 400px-tall block above the items table. On a
   1080×1920 receipt, the photo is the only thing on the first screen
   and the user has to scroll past it to see the data.
2. **Receipt cards** (list view + home page) — `aspect-ratio: 4/3`
   container with `object-fit: cover`. The cover crop hides the top and
   bottom of every receipt, so the user can't tell what receipt they're
   looking at.
3. **Proposal detail page** (`/proposals/{id}`) — already side-by-side
   (1fr 1fr), but the photo column is the same width as the items
   column, which makes a vertical image look tiny in half the page.

## Design

### 1. Receipt detail page — side-by-side, photo on the left

Replace the full-width photo block with a two-column layout:

- **Left column (~40% width)**: photo, loaded with `?size=thumb` (the
  thumb variant is already implemented in the backend).
  - `object-fit: contain` so the **whole receipt is visible**
  - `max-height: 500px`
  - Below the image: a "View full size" link that opens the full-size
    image in a new tab (`<a target="_blank">`).
- **Right column (~60% width)**: page header (merchant, date, total) +
  the Edit / Re-open buttons + the items table.
- On mobile (viewport < 768px): stack vertically, photo on top at
  `max-height: 320px` so it doesn't dominate the screen.
- The "Re-open as Proposal" button moves into the right column's
  actions row alongside Edit and Back (they were already in
  `page-header-actions`).

### 2. Receipt cards — horizontal rows

Convert the card from a vertical stack (thumb on top, text below) to a
horizontal row:

- **Left**: thumbnail at fixed small size (~72×96px, matches 3:4 aspect
  of typical receipts), `object-fit: contain` so the full receipt is
  visible. `background: var(--bg-tertiary)` to fill the empty space
  around the contained image.
- **Right**: merchant name (larger), date, item count, total.
- Card height drops from ~250px to ~110px. The home page (which uses
  the same component) gets the fix for free.
- The cards container switches from a CSS grid (3-4 columns) to a
  single column of rows, OR keeps a 2-3 column grid of horizontal
  cards. Decision below.

**Layout decision**: use a single column of horizontal cards on the
list view. A 2-3 column grid of horizontal cards wastes vertical space
when each card is short. The home page can keep a smaller grid (2
columns) of horizontal cards for visual variety.

### 3. Proposal detail page — keep side-by-side, rebalance columns

The existing layout is the right pattern. Just adjust proportions and
sizing:

- Change `grid-template-columns` from `1fr 1fr` to `1fr 1.4fr` so the
  items table has more room.
- Cap the zoomable image's container at `max-height: 540px` (down from
  600px) on a typical screen, so the items table doesn't have to scroll
  alongside the photo.
- Keep the existing zoom (pinch/scroll) behavior — the user does need
  to see the photo at full size here to verify OCR extraction.

## Files to change

- `client/pages/receipt.ts` — restructure the page layout
- `client/components/receipt-card.ts` — horizontal card layout
- `client/pages/proposal.ts` — minor column rebalance (no markup
  change, just CSS)
- `client/styles/main.css` — new `.receipt-detail-layout`,
  `.receipt-card` horizontal rules, updated `.proposal-layout`,
  updated `.receipt-thumb` (smaller, `object-fit: contain`)

No backend changes. The `?size=thumb` variant of `/api/photos/{id}`
is already implemented.

## Acceptance criteria

- [ ] On the receipt detail page, photo and items are visible without
      scrolling on a 1280×800 desktop viewport.
- [ ] The photo on the receipt detail page is the `?size=thumb`
      variant (verify via Network tab — request URL contains `size=thumb`).
- [ ] On a 1280×800 desktop, the receipt card list shows at least 6
      cards without scrolling.
- [ ] Receipt card thumbnails show the **entire** receipt (no
      cover-crop hiding the top or bottom).
- [ ] On mobile (375px width), the receipt detail page stacks photo
      above items.
- [ ] The proposal page's photo column is narrower than the items
      column, and the items table is fully visible without scrolling
      on a 1280×800 viewport.
- [ ] No existing functionality regresses (Edit, Re-open, navigation
      all still work).
