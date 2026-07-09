# UX Overhaul — Ticket Index

**Companion to:** `docs/superpowers/specs/2026-07-09-ux-overhaul.md`

## Status

| # | Ticket | Type | Status | Depends on |
|---|--------|------|--------|------------|
| 01 | [Backend: `GET /api/users`](./01-backend-users-endpoint.md) | backend | ✅ | — |
| 02 | [Backend: enriched receipt DTOs](./02-backend-enriched-dtos.md) | backend | ✅ | — |
| 03 | [Backend: enriched receipt endpoints](./03-backend-enriched-receipts-endpoint.md) | backend | ✅ | 02 |
| 04 | [Frontend: shared utility helpers](./04-frontend-utils.md) | frontend | ✅ | — |
| 05 | [Frontend: new CSS](./05-frontend-css.md) | frontend | ✅ | — |
| 06 | [Frontend: sidebar active state](./06-frontend-sidebar-active-state.md) | frontend | ✅ | 05 |
| 07 | [Frontend: receipts list page](./07-frontend-receipts-list.md) | frontend | ✅ | 01, 02, 03, 04, 05 |
| 08 | [Frontend: receipt detail page](./08-frontend-receipt-detail.md) | frontend | ✅ | 01, 02, 03, 04, 05 |
| 09 | [Frontend: items list page](./09-frontend-items-list.md) | frontend | ✅ | 04, 05 |
| 10 | [Frontend: item detail page](./10-frontend-item-detail.md) | frontend | ✅ | 04, 05 |
| 11 | [Frontend: home page](./11-frontend-home.md) | frontend | ✅ | 04, 05 |
| 12 | [Frontend: mobile responsive tables](./12-frontend-mobile-responsive.md) | frontend | ✅ | 05, 09, 10 |

## Recommended order

Backend first (so frontend has data to consume), then frontend in dependency order.

1. 01 (users endpoint)
2. 02 (DTOs)
3. 03 (enriched endpoints — depends on 02)
4. 04 (utility helpers)
5. 05 (CSS)
6. 06 (sidebar active state)
7. 07 (receipts list)
8. 08 (receipt detail)
9. 09 (items list)
10. 10 (item detail)
11. 11 (home)
12. 12 (mobile polish)

## Per-ticket workflow

Each session should:

1. Read the ticket file in full.
2. **Brainstorm** to fill the "Open questions" section. Add decisions to the ticket's "Decisions log".
3. Implement the change.
4. Verify against the acceptance criteria.
5. Run the build command(s) listed in the ticket.
6. Mark the ticket status in this index (e.g. `⬜` → `🟡` → `✅`).
7. Move on to the next ticket in a fresh session (per the user's workflow).

## Cross-cutting concerns

These don't have their own ticket but are noted in the relevant ones:

- **Photo memory leaks:** `URL.createObjectURL` is called but not `revokeObjectURL`'d. Track as a follow-up.
- **Search persistence in URL:** Currently no URL params for filters. Track as a follow-up.
- **Pagination:** Not implemented. Add when receipt count > 200.
- **Photo thumbnails:** List cards don't show photos. Track as a follow-up.
- **Owner display:** Data loaded but not shown per the product decision. Ticket to be created when product is ready.
