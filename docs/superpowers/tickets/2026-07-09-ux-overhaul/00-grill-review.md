# UX Overhaul — Grilling Review (2026-07-09)

**Scope:** Spec + tickets 01, 02 (with 03 read for context)
**Reviewer:** Agent session

## Summary

Spec is well-structured. Tickets 01 and 02 are implementable as written, but several
"open questions" have cheap, obvious answers that should be resolved now rather than
deferred to the implementation session. Three real concerns worth flagging are captured
below; none are blockers.

## Spec concerns (informational, not blocking)

1. **Tier 1 dependency overstated.** The spec says Tier 1 "depends on Tier 2" but tickets
   09, 10, 11 only need existing endpoints (`/api/items`, `/api/categories` already
   return names). Only tickets 07, 08 need enriched receipt DTOs. The ticket README's
   per-ticket dependency column is more accurate.

2. **Pagination threshold is arbitrary.** "~200 receipts" is not tied to a real signal.
   Better: "when a single response > 200KB on the wire" or "when client-side search
   p99 latency > 200ms." Defer the threshold, not the feature.

3. **No deprecation story for old endpoints.** Once the client is on enriched endpoints,
   `/api/receipts` and `/api/receipts/{id}` exist only for the bot. Worth a one-liner
   in the spec: "Old endpoints retained indefinitely for bot compatibility; no client
   code should consume them after ticket 12."

4. **Privacy note missing.** `/api/users` exposes every family member's `username` and
   `name` to any authenticated user. Acceptable under the family-shared model
   (per `AGENTS.md` project overview), but should be acknowledged, not assumed.

5. **Risk 9 mitigation is hand-wavy.** Spec says "document in the API spec" for the
   live-join behavior. The DTO struct comment is the right place — handled in ticket 02.

## Ticket 01 — resolved decisions

| Open question | Decision | Rationale |
|---------------|----------|-----------|
| Sort order | **Alphabetical by `Name` ascending** | Predictable for debugging, free at family scale (handful of users). |
| Field set | **Use `domain.User` directly** (`userId`, `name`, `username`); `passwordHash` already has `json:"-"` | No DTO needed; no field leakage risk. |
| Pagination | **None** | Family scale, not justified. |
| Caching headers | **None** | Client cache is short-lived (session); server doesn't need to advertise cacheability. |
| Empty list shape | **Return `[]`, not `null`** — guard `if users == nil { users = []*domain.User{} }` | Acceptance criterion; same Go pitfall applies to ticket 03. |
| Tests | **Add three:** happy path (sorted, no passwordHash), requires auth, empty returns `[]` | Pattern exists in `TestAuthorizedAccess` / `TestLoginEndpoint`. |

## Ticket 02 — resolved decisions

| Open question | Decision | Rationale |
|---------------|----------|-----------|
| File location | **`internal/api/types.go` (new)** | Keeps DTOs discoverable; ticket 03 handlers stay in `receipts.go`. |
| Fallback strings | **Constants in `types.go`:** `UnknownMerchant = "Unknown merchant"`, `UnknownOwner = "Unknown"`, `UnknownCategory = "Uncategorized"` | Single source of truth for ticket 03 handlers. |
| `TotalPriceCents` rounding | **`int64(math.Round(quantity * unitPriceCents))`** — documented in DTO comment, applied in ticket 03 handler | Float truncation would round 166.5¢ down to 166¢. |
| `PhotoURL` on list | **Include** | ~50 bytes per row, lets client render placeholder if it later adds thumbnails. |
| `currency` field | **Omit** | USD-only by spec decision (Risk 4). |
| "Live join" documentation | **Doc comment on each DTO** | Mitigates spec Risk 9 properly. |
| Tests | **None** | Pure data types; ticket 03 tests will exercise them via the handler. |

## Implementation notes for the implementer

- `withAuditLogging` extracts `req.PathValue("id")` for the resource ID; `/api/users`
  has no path value, so the audit log entry will have an empty `ResourceID` — fine.
- `writeJSON` is `json.NewEncoder(w).Encode(v)`. For empty slices, pass a non-nil
  `[]T{}`, not `nil`. This applies to `handleListUsers` (ticket 01) and will apply to
  `handleListEnrichedReceipts` (ticket 03).
- `domain.User` already has `json:"-"` on `PasswordHash` (line 4 of
  `internal/domain/types.go`). Confirmed; no DTO wrapper needed.
- All IDs in existing API use `json:"XxxId,string"` to render as JSON strings
  (avoiding JS `Number` precision loss for uint64). The DTOs follow the same
  pattern — consistent with the rest of the API.
