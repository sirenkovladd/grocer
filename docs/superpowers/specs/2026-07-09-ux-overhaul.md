# Grocer — UX Overhaul

**Date:** 2026-07-09
**Status:** Planning
**Scope:** Web client (`client/`) + minimal backend additions for enriched data

## Problem

The web client currently displays raw numeric IDs everywhere instead of human-readable names. Looking at a receipt shows `Item #935128556887867392` instead of "Bananas" or "Whole Milk". Looking at the items list shows `categoryId: 935...` instead of "Produce". This makes the app unusable for daily use, even though the data is all in the backend store.

### Concrete examples observed

| Page | Current | Should show |
|------|---------|-------------|
| `/receipts` | `Receipt #935128556887867392` | Merchant name, owner name, date, total, item count |
| `/receipts/{id}` | `Item #935128556887867392` (×7) | Item names + categories + clickable links |
| `/items` | `Category: 935...` | Category name |
| `/item/{id}` | No category name shown | Category breadcrumb |

Other issues:
- Sidebar has no active-page indicator
- Loading states are plain `Loading...` text
- Dates use `toLocaleDateString()` which is locale-dependent
- Money columns aren't right-aligned or monospaced
- No breadcrumbs
- Tables aren't mobile-responsive

## Goals

1. Every ID in the UI is replaced with its name (or a clear "Unknown" fallback).
2. Navigation is obvious — current page is highlighted, breadcrumb shows where you are.
3. Loading and empty states are designed, not afterthoughts.
4. The app is usable on mobile (tables don't break).
5. Owner/user is **not shown in the UI yet** (per product decision — the data is fetched but display is deferred).

## Non-goals (deferred)

- User/owner display anywhere in the UI (data is fetched, display deferred)
- Real-time updates / live proposal streaming changes (already in place)
- Search backend (client-side filtering for now is acceptable at family scale)
- Pagination (not needed at current data volumes; add when count > ~200)
- Accessibility audit beyond `aria-current` (defer to a dedicated pass)
- Dark/light theme toggle (single dark theme is fine)

## Tier breakdown

### Tier 1 — Names everywhere, sidebar state, skeletons, money/date polish
- Client-side: render names from lookup maps; sidebar active state; loading skeletons; `Intl.DateTimeFormat` for dates; right-align + monospace money columns.
- **Depends on:** Tier 2 (the lookup data comes from the enriched endpoint).

### Tier 2 — Backend additions (no user in UI yet)
- `GET /api/users` — list all users (needed to seed the client cache even though UI doesn't display names yet)
- `GET /api/receipts/enriched` — receipts with embedded `merchantName`, `ownerName`, items with `name` + `categoryName`
- `GET /api/receipts/{id}` (existing) — return enriched form (same shape as the list endpoint)
- `GET /api/items` (existing) — already returns item names; no change needed
- `GET /api/categories` (existing) — already returns category names; no change needed

### Tier 3 — Polish
- Breadcrumbs on detail pages (`Receipts › 9351...`)
- Better empty states with clear next-step CTAs
- Relative dates on the home page (`3 days ago`)
- Mobile responsive: tables become stacked card lists under 768px

## Risks & open questions (product-team discussion)

### Risk 1: Enriched endpoint payload size
**Concern:** Embedding item names and category names inside every receipt triples the JSON size. At 500 receipts × 7 items × 200 bytes each, that's ~700 KB per list response.

**Mitigation:**
- Only embed name fields; IDs stay so the client can still navigate.
- For the list endpoint, return a *summary* enrichment (merchant + owner only) — full enrichment only on the detail endpoint. Saves bandwidth for the list view.
- Server-side: batch-load items/categories/merchants/users in one pass; no N+1.

**Decision:** Two endpoints:
- `GET /api/receipts/enriched` → list summary (merchantName, ownerName, itemCount, no per-item names)
- `GET /api/receipts/{id}/enriched` → full detail (every item with name + categoryName)

### Risk 2: User endpoint is loaded but not shown
**Concern:** We add `GET /api/users` but don't show owner names. Wasted work, or premature endpoint?

**Reasoning:**
- The client will cache the user map once at app startup. Cost is one ~500-byte response.
- When owner display is later enabled, no backend work is needed.
- Avoids a follow-up endpoint addition (cheaper to do it now).
- The user list response should **exclude** `passwordHash` (the `domain.User` struct already does this via `json:"-"` — confirm in handler).

**Decision:** Add the endpoint, wire it into the client cache, but don't display anywhere yet. Add a code comment `// Owner display deferred per UX ticket #X`.

### Risk 3: Should we modify the existing `/api/receipts` response or add a new endpoint?
**Options considered:**
- **(a) Modify existing:** Single source of truth, but breaks any client that already depends on the shape.
- **(b) New `/enriched` endpoint:** No breaking change, slight duplication, two response shapes to maintain.
- **(c) Query param `?enriched=true`:** Single endpoint, parameter-controlled, but mixes two shapes in one handler.

**Decision:** Option (b) — separate endpoints. The proposal flow and bot links already use `/api/receipts` (existing shape) and we don't want to change them. New enriched endpoints are clearly opt-in.

### Risk 4: Date formatting choices
**Options:**
- Locale-dependent `toLocaleDateString()` (current — inconsistent)
- Hardcoded `en-US` (consistent but not user-configurable)
- Auto-detect from browser locale (consistent if user has consistent locale, but JS can be weird)

**Decision:** Hardcoded `en-US` for now via `Intl.DateTimeFormat("en-US", {...})` with format `Mon DD, YYYY` (e.g. "May 30, 2026"). Add a config switch later if multilingual is needed.

### Risk 5: Money display
**Options:**
- `$36.70` (current, hand-formatted)
- `$36.7` (broken when zeros are stripped)
- `$36.70` always with `toFixed(2)` (safe)
- `Intl.NumberFormat("en-US", { style: "currency", currency: "USD" })` (locale-aware, handles commas, etc.)

**Decision:** `Intl.NumberFormat` with USD. Centralize in a `formatMoney(cents: number): string` helper in `client/utils.ts` so every page uses the same formatter. Use monospace tabular-nums CSS for columns so digits line up.

### Risk 6: Skeleton vs spinner loading
The proposal page already uses skeletons. Be consistent: skeleton for list/table loads, spinner for in-place state changes (e.g. approve button). Document this convention in the CSS file header.

### Risk 7: Search implementation
**Current scope:** Client-side filter on the loaded receipt list. Fine at family scale (low hundreds of receipts).

**Future:** Server-side search via `/api/search/receipts` (already exists as a route — not yet implemented per `internal/api/search.go`). Defer wiring UI to that endpoint.

### Risk 8: Photo thumbnail on receipt list
Each card would need a small (~80×80px) image. Options:
- (a) `GET /api/photos/{id}` already exists, but each call is a full-size JPEG (~200KB).
- (b) Add a thumbnail endpoint or `?size=thumb` query param.
- (c) Generate on-the-fly via query param, cache by query.

**Decision for now:** Show a placeholder icon on cards (no photo). Detail page shows full photo via existing `/api/photos/{id}`. Thumbnail optimization is a follow-up — it would need cache-invalidation discipline.

### Risk 9: Stale data after enrichment
Enrichment is a *join* at read time. If a merchant is renamed or a category is merged, all receipts show the new name. This is actually correct behavior, but worth noting in case the team expected "snapshot at receipt time" semantics.

**Decision:** Live join. Document in the API spec. If historical name preservation is ever needed, snapshot the name into the receipt record (schema change).

## Implementation plan

### Order (backend first, then frontend in dependency order)

| # | Ticket | Type | Files | Depends on |
|---|--------|------|-------|-----------|
| 1 | Add `GET /api/users` | backend | `internal/api/users.go`, `internal/api/router.go` | — |
| 2 | Enriched receipt DTOs | backend | `internal/api/types.go` | — |
| 3 | `GET /api/receipts/enriched` + `GET /api/receipts/{id}/enriched` | backend | `internal/api/receipts.go`, `internal/api/router.go` | 2 |
| 4 | Shared frontend utility helpers | frontend | `client/utils.ts` (new) | — |
| 5 | New CSS (skeletons, sidebar, breadcrumbs, money, weighted items) | frontend | `client/styles/main.css` | — |
| 6 | Sidebar active state | frontend | `client/main.ts` | 5 |
| 7 | Receipts list page overhaul | frontend | `client/pages/receipts.ts` | 1, 2, 3, 4, 5 |
| 8 | Receipt detail page overhaul | frontend | `client/pages/receipt.ts` | 1, 2, 3, 4, 5 |
| 9 | Items list page overhaul | frontend | `client/pages/items.ts` | 4, 5 |
| 10 | Item detail page overhaul | frontend | `client/pages/item-detail.ts` | 4, 5 |
| 11 | Home page (relative dates, skeletons) | frontend | `client/pages/home.ts` | 4, 5 |
| 12 | Mobile responsive tables (CSS-only) | frontend | `client/styles/main.css` | 5 |

Each ticket is one self-contained implementation unit designed to be picked up in a fresh session. See `docs/superpowers/tickets/2026-07-09-ux-overhaul/` for the individual ticket files.

### Session workflow

For each ticket, a fresh session should:
1. Read the ticket file (it has goal, context, acceptance criteria, dependencies, open questions).
2. **Brainstorm** to fill in any gaps the ticket leaves open (concrete CSS values, exact copy, edge cases).
3. Implement the change.
4. Verify against the acceptance criteria.
5. Run the build (`mise run build_client`) and ensure no TypeScript errors.
6. If a backend ticket: run `go build ./...` and existing tests.
7. Update the ticket with any decisions made during the session (add a "Decisions" section).

## Success criteria

- `go build ./...` and `mise run build_client` both succeed.
- Visiting `/receipts` shows merchant + date + total, not raw IDs.
- Visiting `/receipts/{id}` shows item names like "Bananas" and "Whole Milk", not "Item #935...".
- Sidebar highlights the current page.
- Loading any list page shows a skeleton, not just "Loading..." text.
- Receipts list has working date-range and owner filters.
- Item detail page shows category name (currently only ID).
- No regression: existing flows (login, upload, proposal approval, retry, search) still work.
- No owner/user names appear anywhere in the UI yet (intentional).

## Out of scope / follow-ups

- Real owner display (once product confirms the model)
- Server-side search
- Photo thumbnails on list cards
- Pagination (defer until ~200+ receipts)
- Full a11y audit
- i18n
- Light theme
- Bulk actions (delete multiple receipts)
