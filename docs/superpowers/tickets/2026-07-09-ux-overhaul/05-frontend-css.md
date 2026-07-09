# Ticket 05 — Frontend: New CSS for skeleton, sidebar, breadcrumb, money, weighted items

**Type:** Frontend (styles)
**Files:** `client/styles/main.css` (append to existing)
**Depends on:** —
**Blocks:** Tickets 06, 07, 08, 09, 10, 11, 12

## Goal

Add the CSS classes that the page-level changes will use. Keep all new styles in `client/styles/main.css` (no separate files) for now — the file is one big stylesheet and consistency with the existing pattern matters more than splitting.

## Current state

`client/styles/main.css` is ~17 KB, has all styles. It's the single source of truth. The existing `skeleton-*` classes are already used by the proposal page (`skeleton-merchant`, `skeleton-date`, `skeleton-total` with shimmer animation).

## New CSS to add

Append to `client/styles/main.css`. Group by concern, with section header comments.

### 1. Sidebar active state
```css
.sidebar a.active,
.sidebar a[aria-current="page"] {
  background: var(--bg-tertiary);
  color: var(--text-primary);
  border-left: 2px solid var(--accent);
  padding-left: calc(0.75rem - 2px);
}
```

### 2. Skeleton row (for list/table loading)
```css
.skeleton-row {
  display: flex;
  gap: 1rem;
  padding: 0.75rem 1rem;
  border-bottom: 1px solid var(--border);
}
.skeleton-cell {
  height: 16px;
  background: linear-gradient(90deg, var(--bg-tertiary) 25%, var(--bg-secondary) 50%, var(--bg-tertiary) 75%);
  background-size: 200% 100%;
  animation: shimmer 1.5s infinite;
  border-radius: 4px;
}
.skeleton-cell-sm { width: 60px; }
.skeleton-cell-md { width: 120px; }
.skeleton-cell-lg { flex: 1; }
```

### 3. Money column
```css
.money {
  font-variant-numeric: tabular-nums;
  text-align: right;
  white-space: nowrap;
}
.muted {
  color: var(--text-secondary);
}
```

### 4. Receipt card — enriched layout
```css
.receipt-card .receipt-merchant {
  font-weight: 600;
  font-size: 1rem;
  margin-bottom: 0.25rem;
}
.receipt-card .receipt-meta {
  display: flex;
  gap: 0.75rem;
  font-size: 0.875rem;
  color: var(--text-secondary);
  margin-top: 0.5rem;
}
```

### 5. Breadcrumb
```css
.breadcrumb {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.875rem;
  color: var(--text-secondary);
  margin-bottom: 0.5rem;
}
.breadcrumb a {
  color: var(--text-secondary);
  text-decoration: none;
}
.breadcrumb a:hover { color: var(--text-primary); }
.breadcrumb .separator { color: var(--border); }
.breadcrumb .current { color: var(--text-primary); }
```

### 6. Empty state
```css
.empty-state {
  text-align: center;
  padding: 3rem 2rem;
  color: var(--text-secondary);
  background: var(--bg-secondary);
  border: 1px dashed var(--border);
  border-radius: 8px;
}
.empty-state h3 { color: var(--text-primary); margin-bottom: 0.5rem; }
.empty-state p { margin-bottom: 1rem; }
```

### 7. Filter bar (for receipts list)
```css
.filter-bar {
  display: flex;
  gap: 0.75rem;
  margin-bottom: 1.5rem;
  flex-wrap: wrap;
  align-items: center;
}
.filter-bar input, .filter-bar select {
  padding: 0.4rem 0.6rem;
  font-size: 0.875rem;
}
.filter-bar .search-input { flex: 1; min-width: 200px; }
.filter-bar .filter-label {
  color: var(--text-secondary);
  font-size: 0.85rem;
}
```

### 8. Category badge (on items table)
```css
.category-badge {
  display: inline-block;
  padding: 2px 8px;
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  border-radius: 10px;
  font-size: 0.75rem;
  text-decoration: none;
  white-space: nowrap;
}
.category-badge:hover { color: var(--text-primary); }
```

### 9. Item table — clickable name + weighted quantity display
The proposal page already has `.item-unit-price` for the `@ $1.96/kg` subtitle. Reuse it.
```css
.item-name-link {
  color: var(--text-primary);
  text-decoration: none;
  cursor: pointer;
}
.item-name-link:hover { color: var(--accent); text-decoration: underline; }
```

### 10. Mobile responsive (card-list fallback for tables under 768px)
This is the big Tier 3 piece. Tables collapse into stacked cards on mobile:
```css
@media (max-width: 768px) {
  .responsive-table thead { display: none; }
  .responsive-table tr {
    display: block;
    padding: 0.75rem 0;
    border-bottom: 1px solid var(--border);
  }
  .responsive-table td {
    display: flex;
    justify-content: space-between;
    padding: 0.25rem 0.75rem;
    border: none;
  }
  .responsive-table td::before {
    content: attr(data-label);
    color: var(--text-secondary);
    font-size: 0.85rem;
  }
}
```
The page will need to add `data-label` to each `<td>` and wrap with `.responsive-table`.

## Open questions (brainstorm in fresh session)

- **CSS variables:** Reuse existing `--bg-primary`, `--text-secondary`, etc. for consistency. No new vars needed unless you want a "skeleton-bg" var (defer).
- **Dark vs light theme:** Single dark theme per plan. No theme switching.
- **BEM-style class names:** The existing CSS uses kebab-case + component-prefix (`.receipt-card`, `.cropper-canvas`). Follow the same convention. **Already reflected above.**
- **Specificity wars:** Avoid `!important`. The existing CSS rarely uses it.
- **Animations:** Respect `prefers-reduced-motion` for the shimmer animation? Nice-to-have. Defer unless trivial.
- **Mobile breakpoints:** Single breakpoint at 768px matches the existing `@media (max-width: 768px)` for `.proposal-layout`. Keep that.
- **Color of the active sidebar item:** The current scheme is "subtle background + accent border-left". Could also be "filled accent background". Subtle is more elegant — **stick with plan.**

## Acceptance criteria

- [ ] All new CSS classes above are appended to `client/styles/main.css`.
- [ ] Each section has a CSS comment header (`/* Section Name */`).
- [ ] No existing styles are modified or removed.
- [ ] `mise run build_client` passes.
- [ ] Manual check: open any page that uses the new classes (in subsequent tickets) and verify the styles render correctly.

## Verification commands

```bash
mise run build_client
```

You can also visually inspect in the browser. Since this is CSS-only with no markup changes, no page will break. The new classes simply have no effect until the page-level tickets use them.

## Decisions log

_(Append decisions made during implementation. Format: `- YYYY-MM-DD: <decision> — <reason>`)_
