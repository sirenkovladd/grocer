# Ticket 04 — Frontend: Shared utility helpers

**Type:** Frontend (foundational)
**Files:** `client/utils.ts` (new)
**Depends on:** —
**Blocks:** Tickets 07, 08, 09, 10, 11 (pages that use formatters)

## Goal

Create a small `client/utils.ts` module with shared formatters and helpers so every page renders dates, money, and IDs consistently. Centralizing here means one place to change locale/format later.

## Context

Currently each page does its own ad-hoc formatting:
- `new Date(timestamp * 1000).toLocaleDateString()` — locale-dependent, inconsistent
- `(cents / 100).toFixed(2)` then prefix with `$` — works but not internationalizable
- `Receipt #${id}` — hardcoded label

There's no existing `client/utils.ts`. The closest shared code is `client/main.ts` which exports `api` and `navigate`. New helpers should be exported from a separate file to keep `main.ts` focused on routing.

## Functions to add

```ts
// Money: cents (integer) → "$36.70"
export const formatMoney = (cents: number): string => {
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
  }).format(cents / 100)
}

// Date: unix seconds → "May 30, 2026"
export const formatDate = (unixSeconds: number): string => {
  if (!unixSeconds) return "Unknown date"
  return new Intl.DateTimeFormat("en-US", {
    year: "numeric",
    month: "short",
    day: "numeric",
  }).format(new Date(unixSeconds * 1000))
}

// Date: unix seconds → "3 days ago" (relative, for home page)
export const formatRelativeDate = (unixSeconds: number): string => {
  if (!unixSeconds) return "Unknown"
  const date = new Date(unixSeconds * 1000)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24))
  if (diffDays === 0) return "Today"
  if (diffDays === 1) return "Yesterday"
  if (diffDays < 7) return `${diffDays} days ago`
  if (diffDays < 30) return `${Math.floor(diffDays / 7)} weeks ago`
  if (diffDays < 365) return `${Math.floor(diffDays / 30)} months ago`
  return `${Math.floor(diffDays / 365)} years ago`
}

// Quantity: 0.875 → "0.875", 1 → "1" (strips trailing zeros for whole numbers)
export const formatQuantity = (qty: number): string => {
  // Avoid floating-point display: 1.0 should show as "1", 0.875 as "0.875"
  return Number.isInteger(qty) ? qty.toString() : qty.toString()
  // (toString already strips trailing zeros; integers show without decimal)
}

// ID: 935128... → shortened form for display (first 8 chars + ellipsis)
export const shortId = (id: number | string): string => {
  const s = String(id)
  return s.length > 10 ? `${s.slice(0, 8)}…` : s
}

// Lookup helper: build a map from list
export const indexBy = <T>(items: T[], keyFn: (item: T) => string | number): Record<string, T> => {
  const result: Record<string, T> = {}
  for (const item of items) {
    result[String(keyFn(item))] = item
  }
  return result
}
```

## Open questions (brainstorm in fresh session)

- **Date locale:** Hardcode `"en-US"` or read from `navigator.language`? Plan says hardcode for consistency. **Stick with plan.**
- **Relative date threshold:** When to switch from relative to absolute? Current draft: after 1 year. Maybe switch after 30 days to "May 30, 2026" since relative loses precision. **Decide: switch at 30 days.**
- **Negative diff (future date):** What if `unixSeconds` is in the future (shouldn't happen but just in case)? Return `formatDate` (absolute) as a safe fallback. **Decide: fallback to absolute.**
- **TypeScript strictness:** Use `number` or `bigint` for IDs? The API returns IDs as strings (see `json:"...,string"` tags in Go). Make all ID-accepting helpers take `string` or `number`? **Decide: accept `string | number`, normalize internally.**
- **Where to put the file:** `client/utils.ts` is the natural spot. Confirm no naming conflict.

## Acceptance criteria

- [ ] `client/utils.ts` exists and exports all helpers above.
- [ ] Helpers are unit-testable (pure functions, no side effects). Add a small `client/utils.test.ts` if there's a test runner set up; otherwise skip and rely on visual verification.
- [ ] No page imports from this file yet (that's done in tickets 07–11).
- [ ] `mise run build_client` passes with no TypeScript errors.

## Verification commands

```bash
mise run build_client
# Should succeed with no errors.
# Open dist/index.html or the dev server and confirm nothing broke.
```

You can also write a quick scratch script in `index.html` temporarily to test the formatters, or just `bun -e 'import {formatMoney} from "./client/utils"; console.log(formatMoney(3670))'` to sanity check.

## Decisions log

- 2026-07-09: **Format locale: hardcoded `"en-US"`** — per spec Risk 4, consistent across users.
- 2026-07-09: **Relative date switch points: 7d→weeks, 30d→months, 365d→years, >365→absolute year count.** Matches spec "switch at 30 days" for the days→weeks transition. Resolved in grilling review (see `00-grill-review.md`).
- 2026-07-09: **Future dates: fall back to absolute `formatDate`.** Prevents "−1 days ago" / "Yesterday" for timestamps ahead of now. Bug found in grill.
- 2026-07-09: **`formatQuantity`: round to 3 decimals before stringification** — `(0.1+0.2).toString() === "0.30000000000000004"`. Clamp with `Math.round(qty*1000)/1000`. Bug found in grill.
- 2026-07-09: **ID-accepting helpers take `string | number`, normalize to string internally.** Reason: backend returns IDs as JSON strings (`json:"...,string"`). Existing pages (tickets 07-11) must type IDs as `string` to avoid `Number.MAX_SAFE_INTEGER` precision loss. Resolved in grilling review.
- 2026-07-09: **Add `client/utils.test.ts` with `bun test`.** Verified `bun test` runs. Tests cover all helpers including the two bug-fix edge cases (future dates, float precision).
- 2026-07-09: **File location: `client/utils.ts` (new), as proposed.**
