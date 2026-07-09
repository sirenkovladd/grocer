# Cookie-based sessions — Ticket Index

**Origin:** Out-of-band follow-up to the UX overhaul (post-review
discussion, 2026-07-09).

## Status

| # | Ticket | Type | Status | Depends on |
|---|--------|------|--------|------------|
| 01 | [Server-side cookie-based session management](./01-cookie-based-sessions.md) | backend + frontend | ⬜ | — |

## Why a separate folder

This is a security/auth refactor, not a UX change. It doesn't fit
under the UX overhaul ticket set, and the audit trail (why we moved
from localStorage to cookies) belongs in its own folder for future
reference.

## Implementation order

Single ticket — implement straight through. The recommended rollout
is two-stage (server first, client second) — see the ticket's "Rollback
plan" section for details.

## Per-ticket workflow

Same as the UX overhaul:

1. Read the ticket file in full (it has goal, context, acceptance
   criteria, open questions, decisions log).
2. **Brainstorm** to fill the "Open questions" section. Add decisions
   to the ticket's "Decisions log".
3. Implement the change.
4. Verify against the acceptance criteria.
5. Run `go build ./...`, `go test ./...`, `mise run build_client`,
   `bun test client/`.
6. Do the manual smoke test (see "Verification commands" in the
   ticket).
7. Mark the ticket ✅ in this index.
