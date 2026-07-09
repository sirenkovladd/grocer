# Ticket 01 — Backend: Add `GET /api/users` endpoint

**Type:** Backend
**Files:** `internal/api/users.go` (new), `internal/api/router.go`
**Depends on:** —
**Blocks:** Tickets 07, 08 (frontend cache)

## Goal

Expose a list of all users via a new authenticated endpoint. The data is needed by the frontend to resolve owner IDs to names (even though owner names are not displayed yet — see plan doc).

## Context

The store already has `ListUsers()` in `internal/store/memdb.go:271` and `GetUserByUserID()` at line 254. The domain `User` struct (`internal/domain/types.go`) already has `json:"-"` on `PasswordHash`, so JSON serialization won't leak the hash.

No route currently exposes users. The only existing user-related endpoint is `POST /api/auth/login` in `internal/api/auth.go`.

## Existing patterns to follow

Look at `internal/api/merchants.go` for the simplest analog:
- `handleListMerchants` calls `r.store.ListMerchants()` and writes JSON.
- `handleCreateMerchant` does validation via `validateMerchantName` (see `internal/api/validation.go`).
- Both routes are registered in `internal/api/router.go` via `r.mux.HandleFunc(...)`.

## Acceptance criteria

- [ ] `GET /api/users` returns `200 OK` with JSON array of all users.
- [ ] Each user object has `userId`, `name`, `username` fields. **No `passwordHash` field** in the response.
- [ ] Endpoint requires auth (use `r.withAuth` middleware, consistent with `/api/merchants`).
- [ ] Endpoint is wrapped in `r.withCORS` and `r.withAuditLogging("list", "users", ...)`.
- [ ] No 500 on empty user list (return `[]`, not `null`).
- [ ] `go build ./...` passes.
- [ ] `go test ./internal/api/...` passes (add a test if there's a pattern for the merchants test).

## Open questions (brainstorm in fresh session)

- **Sort order:** Should the list be sorted alphabetically by `name`, by `username`, or in insertion order? The store's `ListUsers()` returns in memdb index order (effectively insertion order). The frontend will build a lookup map so order doesn't strictly matter, but predictable order helps debugging.
- **Field filtering:** Should we expose a slimmer DTO (e.g. `{userId, name}` only, no `username`)? `username` is internal-ish but harmless. Defer to taste.
- **Pagination:** Not needed at family scale (handful of users). Don't add.
- **Caching headers:** Should the endpoint return `Cache-Control`? The frontend will cache in-memory for the session. Defer.

## Verification commands

```bash
go build ./...
go test ./internal/api/...

# Manual: with server running
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/users | jq
# Expected: array of {userId, name, username} objects, no passwordHash field
```

## Decisions log

- 2026-07-09: **Sort by `Name` ascending** — predictable for debugging, free at family scale. Resolved in grilling review (see `00-grill-review.md`).
- 2026-07-09: **Return `domain.User` directly, no DTO wrapper** — `PasswordHash` is already tagged `json:"-"` on `internal/domain/types.go:4`. No leakage risk.
- 2026-07-09: **Empty list must be `[]`, not `null`** — guard with `if users == nil { users = []*domain.User{} }` before `writeJSON`. Same Go pitfall will apply to ticket 03.
- 2026-07-09: **Add three tests** — happy path (sorted, no `passwordHash` field), requires auth (401), empty returns `[]`. Pattern exists in `internal/api/api_test.go`.
- 2026-07-09: **No pagination, no caching headers** — family scale, short-lived client cache.

