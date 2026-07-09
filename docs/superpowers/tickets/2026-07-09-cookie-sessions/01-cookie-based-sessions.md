# Ticket 01 — Server-side cookie-based session management

**Type:** Backend + frontend (small refactor)
**Files:**
- `internal/api/auth.go` — add `Set-Cookie` to login response
- `internal/api/router.go` — update `withAuth` to read cookie OR header
- `internal/api/api_test.go` — add cookie-based test
- `client/main.ts` — remove all `localStorage` token handling
- `client/pages/login.ts` — stop reading `result.token` from response

**Depends on:** —
**Blocks:** —

## Goal

Replace the client-side `localStorage` session storage with server-managed
`HttpOnly` cookies. The browser stops reading or sending session tokens;
the server sets, validates, and clears cookies via standard `Set-Cookie`
headers. The `Authorization: Bearer` header path stays for non-browser
clients (tests, future CLI tools) — both work, cookies are preferred.

## Why (architectural motivation)

The current design inverts session responsibility:

- **Server:** authoritative state. Validates session on every request.
- **Client:** copies the token to `localStorage`, attaches the
  `Authorization: Bearer` header on every fetch, watches for 401s, and
  clears the local copy on logout.

The client has a *copy* of the server's state, and has to keep it in sync.
Three failure modes flow from that:

1. **XSS vulnerability.** `localStorage` is readable by any JS on the
   page. A compromised dependency, malicious browser extension, or stored
   XSS can read the token and impersonate the user — for the lifetime of
   the session. `HttpOnly` cookies are not readable by JS, removing this
   attack class entirely.

2. **Two sources of truth.** The token lives in the client's
   `localStorage` AND in the server's `Session` table. If the server
   revokes the session, the client only finds out on the next 401. If
   the client clears `localStorage` but the server session is still
   alive, the next login creates a *new* session but the old one lingers
   until expiry. Race conditions live in the seams.

3. **No server-side lifecycle control.** The server can't rotate the
   session ID on privilege escalation, can't expire idle sessions from
   a different tab, can't log out all devices atomically. The client
   owns the ID; the server has to wait for it to come back.

Cookies fix all three: `HttpOnly` blocks JS reads, the cookie is the
single source of truth, the server rotates/expires the cookie on
whichever event it wants.

## Current state (where the work is)

**Browser side:**

```ts
// client/main.ts:16
export const isAuthenticated = () => !!localStorage.getItem("token")

// client/main.ts:37-43 (api.fetch)
const token = localStorage.getItem("token")
if (token) {
  headers["Authorization"] = `Bearer ${token}`
}

// client/main.ts:45-49 (on 401)
if (response.status === 401) {
  localStorage.removeItem("token")
  navigate("/login")
  throw new Error("Unauthorized")
}

// client/pages/login.ts:22-23 (after successful login)
if (result.token) {
  localStorage.setItem("token", result.token)
  localStorage.setItem("user", JSON.stringify(result.user))
  navigate("/receipts")
}

// client/pages/proposal.ts:151-154 (photo fetch in proposal page)
const token = localStorage.getItem("token")
const response = await fetch(`/api/photos/${receiptId}`, {
  headers: { Authorization: `Bearer ${token}` },
})
```

**Server side (`internal/api/router.go:120-153`):**

```go
func (r *Router) withAuth(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, req *http.Request) {
        authHeader := req.Header.Get("Authorization")
        if authHeader == "" {
            writeError(w, http.StatusUnauthorized, "missing authorization header")
            return
        }
        token := strings.TrimPrefix(authHeader, "Bearer ")
        // ... validate token
    }
}
```

**Server side (`internal/api/auth.go:29-77`, `handleLogin`):**

```go
// On successful login, returns token in JSON body:
writeJSON(w, http.StatusOK, loginResponse{
    Token: idTokenString,
    User:  user,
})
```

**Tests (use the header):**
`internal/api/api_test.go` (lines 179, 194, 234) and
`internal/api/receipts_enriched_test.go` (many lines) all use
`req.Header.Set("Authorization", "Bearer "+token)`. The header path
MUST stay working so these tests don't all break.

**Bots (do NOT use the API auth):**
`internal/bot/telegram.go` and `internal/bot/discord.go` use the platform
bot tokens (Telegram Bot API, Discord Bot API) and call the store
directly. They never call the grocer HTTP API. The migration doesn't
touch them.

**CLI commands (do NOT use the API auth):**
`cmd/server/main.go` has `--create-user` and `--link-bot` flags that
operate directly on the store. No API auth involved.

So the **only** consumer that switches to cookies is the browser SPA.
Everything else keeps using the `Authorization` header unchanged.

## Implementation

### Step 1: Server — set the cookie on login

In `internal/api/auth.go`, modify `handleLogin` to also send a
`Set-Cookie` header alongside the existing JSON response.

**Keep the JSON response.** The bot/CLI auth path doesn't exist today
but might tomorrow; the JSON token is forward-compat. The browser
ignores it (won't store it anywhere) once it sees the cookie.

```go
// After writeJSON, ALSO set the cookie:
http.SetCookie(w, &http.Cookie{
    Name:     "session",
    Value:    idTokenString,
    Path:     "/",
    HttpOnly: true,
    Secure:   true,                    // drop to false in dev if running on http
    SameSite: http.SameSiteLaxMode,
    MaxAge:   60 * 60 * 24 * 7,        // 1 week
})
```

**Cookie name:** `session`. (Not `grocer_session` or similar — short and
generic, the Path: "/" scopes it.)

**Cookie value:** the same `idTokenString` that's currently in the
`Token` field of the JSON response. Format is `"<sessionID>:<tokenStr>"`,
parsed by `parseTokenString` in `auth.go`.

**Flags:**
- `HttpOnly: true` — JS cannot read. Blocks the XSS class entirely.
- `Secure: true` — sent only over HTTPS. **For local dev over plain
  HTTP, you'll need to set this to `false` or use a TLS terminator.**
  Consider making it env-configurable (e.g. `COOKIE_SECURE` env var,
  default true in prod, false in dev). See open questions.
- `SameSite: http.SameSiteLaxMode` — modern CSRF protection. Blocks
  cross-site `POST`/`PUT`/`DELETE` (i.e. state-changing requests) from
  including the cookie. `Strict` is safer but breaks "click a link from
  email" UX. `Lax` is the right default.
- `Path: "/"` — the cookie is sent to every URL on the origin.
- `MaxAge: 7 days` — matches the existing session duration (look at
  `store.DeleteSessionsByUserID` calls and any cleanup logic to confirm
  the existing expiry; if there's none, 7 days is a reasonable default
  to start with).

**Resolve first (open questions):**
- Is there an existing logout endpoint? If yes, the cookie must be
  cleared there (Set-Cookie with `MaxAge: -1`). If no, this ticket
  doesn't add one. (See open questions.)
- Does the server currently expire idle sessions server-side? If no,
  the cookie expiry is the only bound on session lifetime. Worth
  confirming before shipping.

### Step 2: Server — update `withAuth` to read cookie OR header

In `internal/api/router.go`, modify `withAuth` to try the cookie first,
then fall back to the Authorization header.

```go
func (r *Router) withAuth(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, req *http.Request) {
        var token string

        // Prefer the HttpOnly cookie (browser path)
        if c, err := req.Cookie("session"); err == nil {
            token = c.Value
        } else if authHeader := req.Header.Get("Authorization"); authHeader != "" {
            // Fall back to Authorization header (tests, CLI, future services)
            token = strings.TrimPrefix(authHeader, "Bearer ")
            if token == authHeader {
                writeError(w, http.StatusUnauthorized, "invalid authorization format")
                return
            }
        } else {
            writeError(w, http.StatusUnauthorized, "missing session")
            return
        }

        // ... rest of the validation is unchanged
        sessionID, tokenStr, err := parseTokenString(token)
        if err != nil {
            writeError(w, http.StatusUnauthorized, "invalid token")
            return
        }
        // ... continue
    }
}
```

**Note on error message:** The current code says "missing authorization
header" when the header is absent. With cookies, "missing session" is
more accurate. Tests that assert on the exact error string will need to
update — `grep -r "missing authorization" internal/api/` first.

### Step 3: Client — remove all localStorage token handling

In `client/main.ts`, the changes are concentrated:

**Delete:**
- The `Authorization: Bearer ${token}` line in `api.fetch` (cookies are
  auto-attached for same-origin requests)
- The `localStorage.removeItem("token")` line in the 401 handler (no
  local state to clear)
- The `isAuthenticated()` helper (replaced by an async session check
  or by trusting 401)

**Keep:**
- The 401 handler itself, but simplified to just `navigate("/login")`
- The `navigate` export

**New behavior:** The browser auto-attaches the `session` cookie to
every same-origin fetch. The server returns 401 if the cookie is
missing or invalid. The client just navigates to `/login` on 401.

**`isAuthenticated()` replacement:**

Two options. **Recommend option A** for simplicity:

**Option A (lazy, no extra request):** Don't check. Render the page,
let the first API call either succeed (we're logged in) or 401
(trigger redirect). The user sees a brief flash of the page before
redirect, which is usually invisible because the API call resolves
in <50ms. Code:

```ts
const App = () => {
  return div({ id: "app" },
    () => {
      const path = currentPath.val
      if (path === "/login") return Login()
      // No more sync auth check. The auth guard happens in api.fetch
      // via the 401 handler below.
      if (path === "/") return Layout(HomePage())
      return Layout(() => PageContent(currentPath.val))
    }
  )
}
```

**Option B (eager, one extra request):** Add a `GET /api/session` endpoint
that returns 200 with the user info if the cookie is valid, 401 if not.
Call it once on page load to set up the route. Use the response to
populate the user display. More work, but no flash of content.

For this ticket, **use Option A**. If/when owner display is enabled
(see Open Questions), Option B becomes the natural fit.

**Simplified 401 handler:**

```ts
if (response.status === 401) {
  navigate("/login")
  throw new Error("Unauthorized")
}
```

Note: `localStorage.removeItem("token")` is gone, but the *key insight*
is that there's nothing local to remove. The cookie is server-managed.

### Step 4: Client — update login page

In `client/pages/login.ts`, the login flow changes from "store token,
navigate" to "server sets cookie via Set-Cookie, navigate":

```ts
const handleSubmit = async (e: Event) => {
  e.preventDefault()
  error.val = ""

  try {
    // The response body still has token + user, but we no longer store
    // the token in localStorage. The Set-Cookie header is what
    // authenticates subsequent requests.
    await api.post("/auth/login", {
      username: username.val,
      password: password.val,
    })
    navigate("/receipts")
  } catch (err) {
    error.val = "Login failed"
  }
}
```

**The `localStorage.getItem("user")` and `JSON.parse` are no longer
needed.** If any page reads the user from localStorage, those reads
need to be removed too. `grep -n "localStorage" client/` to find all
usages.

### Step 5: Client — update photo fetch in `client/pages/proposal.ts`

The proposal page does its own raw `fetch` (not via `api.fetch`) to load
photos. The header is no longer needed:

```ts
// Before:
const response = await fetch(`/api/photos/${receiptId}`, {
  headers: { Authorization: `Bearer ${token}` },
})

// After:
const response = await fetch(`/api/photos/${receiptId}`, {
  credentials: "same-origin",  // explicit; the default for same-origin
})
```

`credentials: "same-origin"` is the default for same-origin requests,
so it can be omitted. The cookie is auto-attached. Including it
explicitly is documentation.

After this change, the photo helper in `client/photos.ts` (used by
`client/pages/receipt.ts`) can be simplified too — its manual token
reading can be removed. See `client/photos.ts:18-22`.

### Step 6: Tests

**Add a new test in `internal/api/api_test.go`:**

```go
func TestSessionCookie(t *testing.T) {
    router, s := setupTestRouter(t)
    user := &domain.User{
        UserID:       s.UserID.Gen(),
        Username:     "cookietest",
        Name:         "Cookie Test",
        PasswordHash: hashPasswordForTest("password123"),
    }
    if err := s.CreateUser(user); err != nil {
        t.Fatalf("Failed to create user: %v", err)
    }

    // Login and check the Set-Cookie header
    loginReq := map[string]string{
        "username": "cookietest",
        "password": "password123",
    }
    body, _ := json.Marshal(loginReq)
    req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    if w.Code != http.StatusOK {
        t.Fatalf("Expected 200, got %d", w.Code)
    }

    cookies := w.Result().Cookies()
    var session *http.Cookie
    for _, c := range cookies {
        if c.Name == "session" {
            session = c
            break
        }
    }
    if session == nil {
        t.Fatal("Expected Set-Cookie: session=...; got none")
    }
    if !session.HttpOnly {
        t.Error("Expected HttpOnly=true")
    }
    if session.SameSite != http.SameSiteLaxMode {
        t.Errorf("Expected SameSite=Lax, got %v", session.SameSite)
    }
    if session.Path != "/" {
        t.Errorf("Expected Path=/, got %q", session.Path)
    }
    if session.Value == "" {
        t.Error("Expected non-empty cookie value")
    }

    // Now use the cookie to make an authenticated request
    req2 := httptest.NewRequest("GET", "/api/receipts", nil)
    req2.AddCookie(session)
    w2 := httptest.NewRecorder()
    router.ServeHTTP(w2, req2)

    if w2.Code != http.StatusOK {
        t.Errorf("Cookie-authenticated request failed: %d %s", w2.Code, w2.Body.String())
    }
}
```

**Existing tests should still pass** — they use the `Authorization`
header, which the new `withAuth` still accepts. The only thing that
could break is if any test asserts on the exact text of the 401 error
message (e.g. "missing authorization header" vs "missing session").
`grep -r "missing authorization" internal/api/`.

### Step 7: CORS

The current `ServeHTTP` and `withCORS` set `Access-Control-Allow-Headers:
"Content-Type, Authorization"`. With cookies, the browser doesn't need
an Authorization header in CORS preflights — but the Allow-Headers
list can stay as-is (it's additive). For cross-origin deployments (not
the current setup), you would also need
`Access-Control-Allow-Credentials: true` and the client would need
`credentials: "include"`. Since this project is single-origin
(Go server serves both the API and the SPA), no CORS change is
needed for the immediate migration.

**Open question:** Should we also tighten `Access-Control-Allow-Origin`
from `*` to the actual origin, for defense-in-depth? Not part of this
ticket, but worth a follow-up.

## Acceptance criteria

- [ ] `POST /api/auth/login` sets a `Set-Cookie: session=...; HttpOnly;
  SameSite=Lax; Path=/; Max-Age=...` header on a successful login
- [ ] `withAuth` reads the session from the cookie if present, falling
  back to the `Authorization: Bearer` header for backward compat
- [ ] All existing tests pass (`go test ./...`) — none modified
- [ ] New test `TestSessionCookie` passes
- [ ] `client/main.ts` no longer references `localStorage` for auth
- [ ] `client/pages/login.ts` no longer writes to `localStorage` for
  auth
- [ ] `client/pages/proposal.ts` and `client/photos.ts` no longer
  attach the `Authorization` header to photo fetches
- [ ] Manual smoke test: in a browser, log in → cookie is set (visible
  in DevTools → Application → Cookies) → navigate to `/receipts` →
  request succeeds → clear cookie in DevTools → refresh → redirected
  to `/login`
- [ ] `go build ./...` and `mise run build_client` pass
- [ ] No regression in the bot (Telegram/Discord) integration — the
  bots don't use the HTTP API auth, so they should be unaffected, but
  verify

## Open questions (resolve in fresh session)

- **Local development over HTTP:** `Secure: true` means the cookie is
  rejected by the browser on plain HTTP (e.g. `http://localhost:8080`).
  For dev you'll need either TLS termination or to make `Secure`
  env-configurable. **Recommend:** make it configurable via
  `COOKIE_SECURE` env var, default `true`. Set `COOKIE_SECURE=false`
  for local dev.

- **Logout endpoint:** There is none today. The login page doesn't
  have a "Logout" button visible. Should this ticket add a
  `POST /api/auth/logout` endpoint? It would clear the cookie
  (`Set-Cookie: session=; Max-Age=0`) and delete the server session.
  **Recommend yes** — without it, a user can't log out of a shared
  device. Trivial to add; a few LOC. (Look for any existing
  `/logout` link in the UI first — if there is one pointing nowhere,
  wire it up; if not, no UI work in this ticket.)

- **Session expiry server-side:** Does the server currently expire
  sessions after a period of inactivity? `grep -r "expire" internal/`
  and `grep -r "cleanup" internal/`. If not, the cookie `MaxAge` is
  the only expiry. Worth adding a server-side sweep as a follow-up.

- **Owner display deferral:** The data is loaded but the spec says
  owner names shouldn't be displayed yet. With cookies, the
  `isAuthenticated()` helper is gone, but a fresh `GET /api/session`
  could be added to populate user info when owner display is
  enabled. Out of scope for this ticket; just noting the hook.

- **CSRF beyond SameSite=Lax:** For a self-hosted family app, Lax is
  enough. If you ever deploy this publicly, add an `Origin` header
  check in `withAuth` for state-changing methods. Trivial follow-up.

- **Cookie domain scoping:** Currently `Path: "/"` with no `Domain`
  attribute, which defaults to the exact origin host. If you ever run
  the API on a subdomain (e.g. `api.grocer.example.com` and
  `app.grocer.example.com`), you'd need to set `Domain: ".example.com"`
  to share the cookie. Not needed today.

- **The `user` localStorage entry:** `client/pages/login.ts:23` stores
  `JSON.stringify(result.user)`. Some code might read it. Run
  `grep -n "localStorage.getItem(\"user\")" client/` to find any
  readers. They all need to be removed or replaced with a fresh
  fetch in a follow-up.

## Verification commands

```bash
# Build
go build ./...
mise run build_client

# Tests
go test ./internal/api/... -run TestSessionCookie -v
go test ./...                     # all 36 should still pass
bun test client/                  # 27 frontend tests should pass

# Manual smoke test
mise run build_client_prod && ./dist/server
# 1. Open http://localhost:8080 in a browser
# 2. Log in as any user
# 3. Open DevTools → Application → Cookies → http://localhost:8080
#    → confirm `session` cookie is HttpOnly, SameSite=Lax
# 4. Click around /receipts, /items, /analysis — all should work
# 5. In DevTools, delete the session cookie, refresh
#    → should redirect to /login
# 6. Close the browser, reopen, revisit the URL
#    → should redirect to /login (cookie was the only session state)
```

## Rollback plan

If something goes wrong, the migration is fully reversible:

1. **Server side:** Revert the cookie-writing change in `auth.go` and
   the cookie-reading change in `router.go`. The header path still
   works, so existing tests pass.
2. **Client side:** Revert the `localStorage` removals. The cookie
   is set but ignored by the client (browser just discards it). The
   header still works.

The two changes are independent: a half-rolled-out state is still
functional. You could also ship the server change first (header +
cookie both accepted), then ship the client change later. That
two-stage rollout is the recommended deployment order:

- **Deploy 1:** Server changes only. Cookie is set on login, but
  the client still uses localStorage. The cookie is harmless
  (browser stores it, client ignores it).
- **Deploy 2:** Client changes. localStorage is removed; the
  client relies on the cookie.

This avoids any "deploy both at once" risk.

## Decisions log

_(Append decisions made during implementation. Format:
`- YYYY-MM-DD: <decision> — <reason>`)_
