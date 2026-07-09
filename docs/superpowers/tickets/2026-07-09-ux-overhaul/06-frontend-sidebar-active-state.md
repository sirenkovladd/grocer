# Ticket 06 — Frontend: Sidebar active state

**Type:** Frontend (small)
**Files:** `client/main.ts`
**Depends on:** Ticket 05 (CSS for `.sidebar a.active`)
**Blocks:** —

## Goal

Highlight the current page in the sidebar. Smallest possible change.

## Current state

`client/main.ts` lines 90–97 build the sidebar:
```ts
nav({ class: "sidebar" },
  a({ href: "#/", onclick: () => navigate("/") }, "Home"),
  a({ href: "#/receipts", onclick: () => navigate("/receipts") }, "Receipts"),
  a({ href: "#/items", onclick: () => navigate("/items") }, "Items"),
  a({ href: "#/merchants", onclick: () => navigate("/merchants") }, "Merchants"),
  a({ href: "#/categories", onclick: () => navigate("/categories") }, "Categories"),
  a({ href: "#/analysis", onclick: () => navigate("/analysis") }, "Analysis"),
),
```

The sidebar is currently built once outside the App component (or inside it but the `currentPath.val` doesn't trigger re-render of the nav). The current implementation has the entire Layout — including the nav — as a constant `Layout` function that doesn't subscribe to `currentPath.val`.

## Implementation

Convert the sidebar to be reactive to `currentPath`. The current page should be marked with `aria-current="page"` (and optionally `class="active"` for the visual treatment — CSS already targets both).

```ts
const Sidebar = () => nav({ class: "sidebar" },
  a({
    href: "#/",
    "aria-current": () => currentPath.val === "/" ? "page" : null,
    onclick: () => navigate("/"),
  }, "Home"),
  // ... same for each link
)
```

VanJS supports function-valued attributes for reactivity. Returning `null` removes the attribute. The CSS in ticket 05 already targets `[aria-current="page"]`.

Also need to make sure the `Layout` function is called inside the reactive context so the sidebar re-renders on path change. Check the current structure:

```ts
const App = () => {
  return div({ id: "app" },
    () => {
      const path = currentPath.val
      if (!guardAuth(path)) return div()
      if (path === "/login") return Login()
      if (path === "/") return Layout(HomePage())
      return Layout(() => PageContent(currentPath.val))
    }
  )
}
```

The `Layout` function takes `content` as a non-reactive argument. The Sidebar must be a separate reactive subtree.

## Refactor

Make the Layout take no args and render both the sidebar and the content slot. Or pass the sidebar as a separate component.

**Recommended approach:**

```ts
const Sidebar = () => nav({ class: "sidebar" },
  a({
    href: "#/",
    "aria-current": () => currentPath.val === "/" ? "page" : null,
    onclick: (e: Event) => { e.preventDefault(); navigate("/") },
  }, "Home"),
  a({
    href: "#/receipts",
    "aria-current": () => currentPath.val === "/receipts" || currentPath.val.startsWith("/receipts/") ? "page" : null,
    onclick: (e: Event) => { e.preventDefault(); navigate("/receipts") },
  }, "Receipts"),
  // ... etc
)

const Layout = (content: any) => div({ class: "layout" },
  Sidebar(),
  div({ class: "content" }, content),
)
```

Add `e.preventDefault()` to the existing onclick handlers so the hash change is handled by `navigate()` consistently (otherwise the URL updates twice on click).

## Active state rules

| Path | Active link |
|------|-------------|
| `/` | Home |
| `/receipts` or `/receipts/{anything}` | Receipts |
| `/receipts/upload` | Receipts (treat as a sub-page) |
| `/items` or `/items/{id}` | Items |
| `/merchants` | Merchants |
| `/categories` | Categories |
| `/analysis` | Analysis |
| `/proposals/{id}` | (none — proposal page is a sub-flow, not a main section) |
| `/login` | (none — login has no sidebar) |

## Acceptance criteria

- [ ] Visiting `/` highlights "Home" in the sidebar.
- [ ] Visiting `/receipts` or `/receipts/123` highlights "Receipts".
- [ ] Visiting `/items` or `/items/123` highlights "Items".
- [ ] Visiting `/proposals/123` highlights **no** main nav item (or just keeps the previous — your call; recommend none).
- [ ] The visual treatment (subtle background + accent border-left) renders per the CSS from ticket 05.
- [ ] Clicking sidebar links still navigates correctly (no double-navigation).
- [ ] `mise run build_client` passes.

## Open questions (brainstorm in fresh session)

- **Proposal pages:** Should the active state stay on the previous main nav? Or clear? Most SPA frameworks keep the previous active state. **Recommend keep previous** (less visual churn).
- **Mobile:** Sidebar is 200px wide — fine for desktop, but on mobile (375px) it eats half the screen. Existing CSS has no mobile sidebar. **Defer mobile sidebar to a future ticket.** This ticket is desktop-only.
- **Hash vs pathname:** Links use `#/` for hash routing. `aria-current` doesn't care about the format.

## Verification commands

```bash
mise run build_client
```

Manual: open each route in a browser, confirm the correct sidebar item is highlighted.

## Decisions log

- 2026-07-09: **`aria-current="page"` only, no `class="active"`.** CSS targets both, so the `class` attribute is redundant. `aria-current` is the accessible signal.
- 2026-07-09: **`e.preventDefault()` added to all sidebar link handlers.** Fixes a latent double-`hashchange` bug (browser default + explicit `navigate()`).
- 2026-07-09: **`Sidebar` is a separate reactive component**; `Layout` calls it. The function-valued `aria-current` attribute re-evaluates on `currentPath.val` changes; VanJS diffs and updates the DOM in place.
- 2026-07-09: **Receipts/Items highlights also match nested paths** (`/receipts/123`, `/items/45`) via `startsWith`. Proposal pages highlight nothing (per recommendation).
