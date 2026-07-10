# Mobile UI and Cropper Touch Fix — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Grocer app usable on a phone end-to-end by fixing the touch-only cropper bug, adding a hamburger menu, applying the existing `.responsive-table` CSS to the merchants page, and bumping primary action buttons to 44px on mobile.

**Architecture:** Pure frontend change. Four independent components (cropper touch, hamburger, responsive-table, button sizing), each a small DOM/CSS/TS edit. No backend changes, no new state shapes, no new endpoints.

**Tech Stack:** Vanilla TypeScript (VanJS for state), CSS, no new dependencies.

**Spec:** `docs/superpowers/specs/2026-07-10-mobile-ui-and-cropper-touch.md`

## Global Constraints

- This is a frontend-only change. No Go files are touched.
- The breakpoint throughout is `768px` (matches the existing single `@media` rule in `main.css`).
- The CSS class `.responsive-table` and its stacked-card rules already exist in `client/styles/main.css` from the 2026-07-09 UX overhaul (ticket 05). Reuse, do not re-define.
- The `.sidebar` element is actually a top nav rendered as `<nav class="sidebar">` — keep the class name even when adding the hamburger.
- Manual verification happens in Chrome DevTools responsive mode (375px and 390px). No automated browser tests in this project.
- The build chain must pass at the end of each task: `mise run build_client`, `go build ./...`, `go test ./...`.

---

## File Map

| File | What changes |
|---|---|
| `client/components/cropper.ts` | Port mouse handle-detection + resize branches to touch handlers. Bump touch hit area to 32px. |
| `client/main.ts` | Add `navOpen` state. Refactor `Sidebar` to render hamburger on mobile. Add drawer open/close. |
| `client/styles/main.css` | Add hamburger/drawer rules to the existing `@media (max-width: 768px)` block. Add 44px rule for `.approve-btn`/`.btn-primary`. |
| `client/pages/merchants.ts` | Add `class: "responsive-table"` to the price-history table and `data-label` to each `<td>`. |

---

### Task 1: Cropper touch resize

**Files:**
- Modify: `client/components/cropper.ts:308-353` (the `touchstart`, `touchmove`, `touchend` handlers in `setupEventListeners`)
- Modify: `client/components/cropper.ts:36-44` (the touch-handle-size constant near the top of the file)

**Interfaces:**
- Consumes: the existing `cropRect`, `dragHandle`, `dragStartX`, `dragStartY`, `dragStartCrop`, `isDragging`, `isMoving` module-level state in `cropper.ts`.
- Produces: a touch handler that, on `touchstart`, hits any of the four corner handles and arms the corresponding resize branch in `touchmove`. The DOM output and `onCrop`/`onSkip` callbacks are unchanged.

- [ ] **Step 1: Add a touch-specific handle size constant**

In `client/components/cropper.ts`, find the drag-state block (the `let` declarations around lines 36-44) and add a touch-handle-size constant right above the existing handlers. Read the file first to find the exact insertion point — the existing `handleSize = 12` lives inside the `mousedown` handler (line 227), not as a module-level constant, so put the new one in the drag-state block:

```ts
// Touch hit area is larger than the 12px desktop hit area so a finger
// can grab a corner reliably. Mouse hit area stays at 12px because
// pointer precision is much higher.
const touchHandleSize = 32
```

- [ ] **Step 2: Replace the `touchstart` handler**

Find the `cropBox.addEventListener("touchstart", (e) => {` block (line 308) and replace its body with the version below. The new version does the same corner-handle hit test the mouse handler does, then falls through to the "inside the box → move" branch.

```ts
    cropBox.addEventListener("touchstart", (e) => {
      e.preventDefault()
      const touch = e.touches[0]
      const rect = overlay.getBoundingClientRect()
      const x = touch.clientX - rect.left
      const y = touch.clientY - rect.top

      // Check if near a handle (corners). Same logic as mousedown
      // but with the larger touch hit area.
      const handles = [
        { name: "nw", x: cropRect.x, y: cropRect.y },
        { name: "ne", x: cropRect.x + cropRect.width, y: cropRect.y },
        { name: "sw", x: cropRect.x, y: cropRect.y + cropRect.height },
        { name: "se", x: cropRect.x + cropRect.width, y: cropRect.y + cropRect.height },
      ]

      for (const handle of handles) {
        if (Math.abs(x - handle.x) < touchHandleSize && Math.abs(y - handle.y) < touchHandleSize) {
          isDragging = true
          dragHandle = handle.name
          dragStartX = x
          dragStartY = y
          dragStartCrop = { ...cropRect }
          return
        }
      }

      // Check if inside crop box for moving
      if (x >= cropRect.x && x <= cropRect.x + cropRect.width &&
          y >= cropRect.y && y <= cropRect.y + cropRect.height) {
        isDragging = true
        isMoving = true
        dragHandle = null
        dragStartX = x
        dragStartY = y
        dragStartCrop = { ...cropRect }
      }
    })
```

- [ ] **Step 3: Replace the `touchmove` handler**

Find the `document.addEventListener("touchmove", (e) => {` block (line 327) and replace its body with the version that handles all four resize branches (not just the move branch). The structure mirrors the mouse `mousemove` handler's resize switch, but uses the touch coordinates from `e.touches[0]`.

```ts
    document.addEventListener("touchmove", (e) => {
      if (!isDragging) return
      e.preventDefault()

      const touch = e.touches[0]
      const rect = overlay.getBoundingClientRect()
      const x = touch.clientX - rect.left
      const y = touch.clientY - rect.top
      const dx = x - dragStartX
      const dy = y - dragStartY

      if (isMoving) {
        cropRect.x = Math.max(0, Math.min(canvas.width - dragStartCrop.width, dragStartCrop.x + dx))
        cropRect.y = Math.max(0, Math.min(canvas.height - dragStartCrop.height, dragStartCrop.y + dy))
      } else if (dragHandle) {
        const minSize = 20

        switch (dragHandle) {
          case "nw":
            cropRect.x = Math.max(0, dragStartCrop.x + dx)
            cropRect.y = Math.max(0, dragStartCrop.y + dy)
            cropRect.width = Math.max(minSize, dragStartCrop.width - (cropRect.x - dragStartCrop.x))
            cropRect.height = Math.max(minSize, dragStartCrop.height - (cropRect.y - dragStartCrop.y))
            break
          case "ne":
            cropRect.width = Math.max(minSize, Math.min(canvas.width - dragStartCrop.x, dragStartCrop.width + dx))
            cropRect.y = Math.max(0, dragStartCrop.y + dy)
            cropRect.height = Math.max(minSize, dragStartCrop.height - (cropRect.y - dragStartCrop.y))
            break
          case "sw":
            cropRect.x = Math.max(0, dragStartCrop.x + dx)
            cropRect.width = Math.max(minSize, dragStartCrop.width - (cropRect.x - dragStartCrop.x))
            cropRect.height = Math.max(minSize, Math.min(canvas.height - dragStartCrop.y, dragStartCrop.height + dy))
            break
          case "se":
            cropRect.width = Math.max(minSize, Math.min(canvas.width - dragStartCrop.x, dragStartCrop.width + dx))
            cropRect.height = Math.max(minSize, Math.min(canvas.height - dragStartCrop.y, dragStartCrop.height + dy))
            break
        }
      }

      updateCropBox()
    })
```

- [ ] **Step 4: Build and verify no regressions**

Run: `cd /Users/vlad/code/grocer && mise run build_client`
Expected: build succeeds with no TypeScript errors.

Run: `go build ./... && go test ./...`
Expected: Go side builds, all tests pass (no Go changes here, but verify nothing else broke).

- [ ] **Step 5: Manual test (mobile viewport)**

In Chrome DevTools, set viewport to 390px wide. Open the app, go to `/receipts/upload`, pick any image. The cropper should appear. Verify:
- Dragging the crop box itself moves it (this is the existing behavior — was already working).
- Dragging any of the four corner handles (NW/NE/SW/SE) resizes the box.
- The 32px hit area is generous enough that you don't have to be precise on the corner pixel.

Then set viewport to 1280px. Verify:
- The existing mouse behavior is unchanged (12px hit area, can resize by clicking near the corners).

- [ ] **Step 6: Commit**

```bash
cd /Users/vlad/code/grocer
git add client/components/cropper.ts
git commit -m "fix(cropper): support corner-handle resize on touch

The mousedown/mousemove handlers handled four corner
handles (NW/NE/SW/SE) with a 12px hit area, but the
touchstart/touchmove handlers were a stripped-down
copy that only moved the box — never checked for the
corners and never branched into the resize math.
Result: on a phone you could drag the box but not
resize it, so a receipt was only ever cropped to
whatever default rectangle the cropper started with.

Port the corner-handle hit test and the four
per-handle resize branches from the mouse handlers
to the touch handlers, and bump the touch hit area
to 32px (mouse stays at 12px since pointer
precision is higher). Touch event listeners
remain attached to cropBox; the hit test uses the
larger touchHandleSize."
```

---

### Task 2: Hamburger menu

**Files:**
- Modify: `client/main.ts:1-99` (the `Sidebar` component and surrounding `currentPath` / `navigate` setup)
- Modify: `client/styles/main.css` (the existing `@media (max-width: 768px)` block, plus a new block for the drawer)

**Interfaces:**
- Consumes: `currentPath` (existing VanJS state), `navigate` (existing function), `handleLogout` (existing function in this file).
- Produces: a `navOpen` VanJS state. On screens < 768px, the `Sidebar` renders a hamburger button + a slide-in drawer with the same nav items. On screens ≥ 769px, the existing inline nav is unchanged. Drawer closes on link click, click outside, or `Esc`.

- [ ] **Step 1: Add the `navOpen` state and refactor `Sidebar` to read it**

In `client/main.ts`, find the `currentPath` state declaration (somewhere above `navigate` in the file) and add `navOpen` next to it:

```ts
// navOpen controls the mobile hamburger drawer. It's read by the
// Sidebar component on narrow viewports to toggle the drawer; the
// inline (desktop) nav ignores it entirely. The state is local to
// this module — there's no need to share it with pages.
const navOpen = van.state(false)
```

Then refactor the `Sidebar` function to:
- On wide viewports (≥ 769px), behave exactly as today — render the existing nav with all links visible.
- On narrow viewports (< 768px), render a hamburger button on the left and a slide-in drawer with the nav items.
- The drawer is `position: fixed` and overlays the page. Closed by default; opens when the hamburger is clicked.

The cleanest way to do this with VanJS is to have `Sidebar` always render the same DOM and let CSS hide/show based on a `@media` query. The hamburger button and the drawer are both always in the DOM; CSS makes the drawer invisible by default on desktop, and the hamburger button invisible by default on desktop. JS only toggles a class (or the `navOpen` state) to open/close the drawer.

Replace the existing `Sidebar` function with:

```ts
const Sidebar = () => nav({ class: "sidebar" },
  // Hamburger button — visible only on narrow viewports via CSS.
  button({
    class: "sidebar-hamburger",
    type: "button",
    "aria-label": "Open navigation",
    "aria-expanded": () => navOpen.val ? "true" : "false",
    onclick: () => { navOpen.val = true },
  }, "☰"),

  // Drawer — positioned over the page on narrow viewports. On
  // desktop it's a normal flow element (no positioning).
  div({
    class: () => "sidebar-drawer" + (navOpen.val ? " sidebar-drawer-open" : ""),
  },
    button({
      class: "sidebar-close",
      type: "button",
      "aria-label": "Close navigation",
      onclick: () => { navOpen.val = false },
    }, "✕"),
    a({
      href: "#/",
      "aria-current": () => currentPath.val === "/" ? "page" : null,
      onclick: (e: Event) => { e.preventDefault(); navOpen.val = false; navigate("/") },
    }, "Home"),
    a({
      href: "#/receipts",
      "aria-current": () => {
        const p = currentPath.val
        return p === "/receipts" || p.startsWith("/receipts/") ? "page" : null
      },
      onclick: (e: Event) => { e.preventDefault(); navOpen.val = false; navigate("/receipts") },
    }, "Receipts"),
    a({
      href: "#/items",
      "aria-current": () => {
        const p = currentPath.val
        return p === "/items" || p.startsWith("/items/") ? "page" : null
      },
      onclick: (e: Event) => { e.preventDefault(); navOpen.val = false; navigate("/items") },
    }, "Items"),
    a({
      href: "#/merchants",
      "aria-current": () => currentPath.val === "/merchants" ? "page" : null,
      onclick: (e: Event) => { e.preventDefault(); navOpen.val = false; navigate("/merchants") },
    }, "Merchants"),
    a({
      href: "#/categories",
      "aria-current": () => currentPath.val === "/categories" ? "page" : null,
      onclick: (e: Event) => { e.preventDefault(); navOpen.val = false; navigate("/categories") },
    }, "Categories"),
    a({
      href: "#/analysis",
      "aria-current": () => currentPath.val === "/analysis" ? "page" : null,
      onclick: (e: Event) => { e.preventDefault(); navOpen.val = false; navigate("/analysis") },
    }, "Analysis"),
    // Footer pushed to the bottom of the flex column via `margin-top: auto`
    // (set in CSS). Holds the Sign-out button so it sits below the nav
    // links without disrupting the existing layout.
    div({ class: "sidebar-footer" },
      button({
        class: "sidebar-logout",
        type: "button",
        onclick: () => { navOpen.val = false; handleLogout() },
      }, "Sign out"),
    ),
  ),

  // Backdrop — visible only on narrow viewports when the drawer is open.
  // Clicking it closes the drawer.
  div({
    class: () => "sidebar-backdrop" + (navOpen.val ? " sidebar-backdrop-open" : ""),
    onclick: () => { navOpen.val = false },
  }),
)
```

Note: on wide viewports the drawer is the inline nav (existing layout). On narrow viewports CSS positions the drawer off-screen by default and slides it in when `sidebar-drawer-open` is added. The hamburger button is `display: none` on wide viewports.

- [ ] **Step 2: Add Esc-key handler to close the drawer**

In `client/main.ts`, add a single `keydown` listener (module-level) that closes the drawer on `Esc`. This needs to be added once at module load, not inside `Sidebar` (which is re-invoked on every render).

```ts
// Close the mobile drawer on Esc.
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && navOpen.val) {
    navOpen.val = false
  }
})
```

- [ ] **Step 3: Add the CSS for the hamburger, drawer, and backdrop**

In `client/styles/main.css`, find the existing `@media (max-width: 768px)` block (the one that sets `.proposal-layout` to single column). Inside that block, add the new rules:

```css
@media (max-width: 768px) {
  .proposal-layout {
    grid-template-columns: 1fr;
  }

  /* Hamburger button — visible only on narrow viewports. */
  .sidebar-hamburger {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 44px;
    height: 44px;
    font-size: 1.5rem;
    background: none;
    border: none;
    cursor: pointer;
    margin-right: 0.5rem;
  }

  /* On wide viewports the drawer is just a normal flex item inside
     the .sidebar nav. On narrow viewports it's positioned off-screen
     and slides in when .sidebar-drawer-open is set. */
  .sidebar-drawer {
    position: fixed;
    top: 0;
    left: 0;
    width: 260px;
    height: 100vh;
    background: var(--color-surface, #fff);
    box-shadow: 2px 0 8px rgba(0, 0, 0, 0.1);
    transform: translateX(-100%);
    transition: transform 0.2s ease-out;
    z-index: 1000;
    display: flex;
    flex-direction: column;
    padding: 1rem;
    overflow-y: auto;
  }

  .sidebar-drawer-open {
    transform: translateX(0);
  }

  .sidebar-close {
    align-self: flex-end;
    background: none;
    border: none;
    font-size: 1.5rem;
    cursor: pointer;
    padding: 0.5rem;
    margin-bottom: 0.5rem;
    min-width: 44px;
    min-height: 44px;
  }

  .sidebar-drawer a {
    display: block;
    padding: 0.75rem 0.5rem;
    min-height: 44px;
    border-radius: 4px;
  }

  .sidebar-drawer a[aria-current="page"] {
    background: var(--color-primary-bg, rgba(76, 154, 255, 0.1));
    font-weight: 600;
  }

  /* Backdrop — visible only when the drawer is open. */
  .sidebar-backdrop {
    display: none;
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.3);
    z-index: 999;
  }

  .sidebar-backdrop-open {
    display: block;
  }
}
```

Then **outside** any `@media` block (so it applies on desktop too), add the rule that hides the hamburger on wide viewports:

```css
/* Hamburger button is hidden on wide viewports. The drawer is the
   inline nav on desktop, so positioning rules inside @media don't
   apply and the drawer is just a normal flex item. */
.sidebar-hamburger {
  display: none;
}
```

- [ ] **Step 4: Build and verify**

Run: `cd /Users/vlad/code/grocer && mise run build_client`
Expected: build succeeds.

Run: `go build ./... && go test ./...`
Expected: passes.

- [ ] **Step 5: Manual test (mobile + desktop)**

In Chrome DevTools at 390px width:
- Verify the inline nav links are gone; only the `☰` button is visible.
- Tap the `☰` button — the drawer slides in from the left, with all 7 items.
- The current page is highlighted in the drawer.
- Tap a link — the drawer closes and the page navigates.
- Tap the `✕` button or the backdrop — the drawer closes.
- Press `Esc` — the drawer closes.

At 1280px width:
- Verify the inline nav is shown exactly as before (all 7 links visible horizontally, no `☰` button, no drawer behavior).

- [ ] **Step 6: Commit**

```bash
cd /Users/vlad/code/grocer
git add client/main.ts client/styles/main.css
git commit -m "feat(ui): hamburger menu on narrow viewports

The top nav has 7 inline links (Home, Receipts, Items,
Merchants, Categories, Analysis, Sign out) and overflows
horizontally on a phone. The existing CSS had exactly
one @media rule (collapsing the proposal page to a
single column) and nothing for the nav itself.

Add a hamburger button + slide-in drawer that's
visible only below 768px. On wide viewports the nav
is unchanged — the existing inline layout still
applies. The drawer is positioned (fixed, 100vh, 260px
wide) on narrow viewports and off-screen by default;
tapping the hamburger or any nav link toggles it open
or closed, and Esc + backdrop click also close it.

The current-page highlight uses the existing
aria-current pattern, so no new state plumbing."
```

---

### Task 3: Apply `.responsive-table` to merchants

**Status: already complete, no changes needed.**

The 2026-07-09 UX overhaul ticket 12 audit listed `merchants.ts` as the one outstanding page where `.responsive-table` had not been applied. Re-checking during implementation (2026-07-10):

- The audit assumed `merchants.ts` had a separate price-history / comparison table (`comparison.val.map(...)`) that needed the class. That table does not exist in the current `merchants.ts`. The purchase-history table that the audit was describing lives in `item-detail.ts` and already has `class: "responsive-table"` plus `data-label` attributes on every `<td>`.
- The merchant list table that does exist in `merchants.ts` (line 198) already has `class: "responsive-table"`, and its `<td>` cells already have `data-label` attributes.

A scan of all five table-rendering pages (`merchants.ts`, `items.ts`, `item-detail.ts`, `receipt.ts`, `manual-receipt.ts`) confirms every one already has `class: "responsive-table"` and `data-label` attributes on its `<td>` cells. The work ticket 12 called for was completed in a later commit; the audit just wasn't updated.

**No code change or commit for this task.** The spec's reference to ticket 12 is now satisfied; ticket 12 can be marked done.

---


---

### Task 4: Primary action button sizing on mobile

**Files:**
- Modify: `client/styles/main.css` (add to the existing `@media (max-width: 768px)` block)

**Interfaces:**
- Consumes: existing `.approve-btn` and `.btn-primary` rules.
- Produces: a new rule inside the existing `@media` block that bumps these buttons to ≥44px tall on mobile.

- [ ] **Step 1: Add the size rule**

In `client/styles/main.css`, find the existing `@media (max-width: 768px)` block. Inside it, add:

```css
  .approve-btn,
  .btn-primary {
    min-height: 44px;
    padding: 0.75rem 1.5rem;
    font-size: 1rem;
  }
```

- [ ] **Step 2: Build and verify**

Run: `cd /Users/vlad/code/grocer && mise run build_client`
Expected: build succeeds.

- [ ] **Step 3: Manual test**

In Chrome DevTools at 390px width, navigate to any proposal. Verify:
- The "Approve" button is at least 44px tall and easily tappable with a thumb.
- Other primary buttons (e.g., the "Add Item" button) on the proposal page are also ≥44px.

At 1280px width, verify the buttons are unchanged (no 44px override visible on desktop).

- [ ] **Step 4: Commit**

```bash
cd /Users/vlad/code/grocer
git add client/styles/main.css
git commit -m "style(ui): bump primary action buttons to 44px on mobile

The proposal page's Approve button (and other .btn-primary
elements) were 32-36px tall, which is below the 44px
accessibility minimum for touch targets. On narrow viewports,
bump them to 44px tall with larger padding. Desktop layout
is unchanged because the rule is inside the @media block."
```

---

## Self-Review

- [x] **Spec coverage:** all four components from the spec are covered (cropper, hamburger, responsive-table, button sizing). The out-of-scope follow-up is intentionally excluded.
- [x] **Placeholder scan:** no TBD/TODO/"implement later" in any step. Every step shows the actual code.
- [x] **Type consistency:** `navOpen` is referenced consistently in `Sidebar` and the `keydown` handler. `touchHandleSize` is referenced only in the new touch handler. Class names match between `main.ts` and the CSS additions.
- [x] **Manual test gates:** every task ends with a manual verification step (Chrome DevTools responsive mode) plus a build verification.

## Execution

The user has asked me to proceed with inline execution. Tasks 1-4 will be done in this session, with a commit per task and a manual test loop for the cropper and hamburger.
