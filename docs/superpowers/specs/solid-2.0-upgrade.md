# Solid 2.0 Upgrade Notes (from the 1.9.15 baseline)

The client was rewritten from VanJS to **Solid 1.9.15** (stable) instead of
Solid 2.0 (rc.10 at the time, broken installs, no UI-kit support). This doc
locks in the 2.0 migration guide + cheatsheet reading so the upgrade is
mechanical when 2.0 stabilizes. Code was written 2.0-ready on purpose:
every API known to change in 2.0 is isolated or avoided (see inventory).

## Trigger condition (ALL must hold)

1. `solid-js@2.x` stable is published (not `next`/rc).
2. The 1.x-line Vite plugin has a stable Vite-8-compatible release, or the
   2.0 plugin line (`@solidjs/vite-plugin`) is stable. (During the rewrite,
   `vite-plugin-solid@2.11.14` broke `.tsx` builds on Vite 8 — upstream
   PR solidjs/solid-vite-plugin#363, unpublished — which is why we pin
   `vite@^7`. Re-test `vite@8` first; it may already be fixed.)
3. No adopted dependency still targets 1.x internals (currently: none —
   no UI kit was adopted).

## The two seam files (only real rewrites)

**`client/lib/query.ts`** — `createResource` is REMOVED in 2.0.
Reimplement on an async computation + `<Loading>`:

```ts
// 2.0 sketch (signatures consumed by pages stay identical)
import { createMemo } from "solid-js"
export function createQuery<T>(fetcher: () => Promise<T>) {
  const data = createMemo(fetcher)          // async memo suspends like a resource
  return { data, refetch: () => refresh(data) }
}
```

Call sites (`data()` reads, `refetch()` after mutations) do not change.
The one direct `createResource` use — the optional item-name fetch in
`client/pages/receipts.tsx` (keyed resource with falsy source = no
suspense) — becomes a memo returning `""` when the param is empty.

**`client/components/QueryBoundary.tsx`** — `<Suspense>` → `<Loading>`,
`<ErrorBoundary>` → `<Errored>` (fallback signatures: `Errored`
`fallback={(err, reset) => ...}` matches what we already pass).

## Mechanical rewrites (per-site list, verified 2026-09-29)

**Imports.** Every `from "solid-js/web"` (today: only `client/main.tsx`
`render`) → `from "@solidjs/web"`. `createStore` (today:
`client/pages/manual-receipt.tsx`, `client/pages/receipt.tsx`, from
`"solid-js/store"`) → from `"solid-js"`. `tsconfig.json`
`jsxImportSource: "solid-js"` → `"@solidjs/web"`.

**`onMount` → `onSettled` (2 sites).** `onMount` is replaced by
`onSettled` (which runs post-settle and may return cleanup):
- `client/components/zoomable-image.tsx:18` (append imperative element)
- `client/components/cropper.tsx:11` (same)

Charts deliberately do NOT use `onMount` — they build in
state-guarded `createEffect`s (below), so no change needed there.

**Single-arg `createEffect(fn)` → split `(compute, apply)` (13 sites).**
The compute half reads, the apply half writes. All our effects are
already read-first, so the split point is the first write:

| File:line | Compute (reads) | Apply (writes) |
|---|---|---|
| `App.tsx:62` | `location.pathname` | `setPageTitle(...)` |
| `receipt-card.tsx:40` | `r.photoUrl`, `r.receiptId` | `fetchPhotoUrl().then(setThumbUrl)` (async continuation — already unowned, likely needs NO change) |
| `receipt.tsx:89` | `data.state`, `data()` | `fetchPhotoUrl().then(setPhotoSrc)` (async — likely no change) |
| `receipt.tsx:116` | `data.state`, `data()` | `setPageTitle(...)` |
| `receipt.tsx:149` | `params.id` | `setIsEditing(false)`, `setEditError(null)` |
| `proposal.tsx:289` | `params.id` | `resetStream()` + `fetchSSE()` — reset moves to apply; stream writes are async (no change) |
| `proposal.tsx:298` | `proposal()`, `status()` | `setPageTitle(...)` |
| `proposal.tsx:316` | `proposal()`, `status()`, `photoSrc()` | `fetchPhotoUrl().then(...)` (async — likely no change) |
| `item-detail.tsx:91` | `data.state`, `data()` | chart destroy/rebuild (DOM writes — apply) |
| `item-detail.tsx:144` | `data.state`, `data()` | `setPageTitle(...)` |
| `analysis.tsx:128` | — (fire-and-forget fetch) | `setCatLookup(...)` (async — likely no change) |
| `analysis.tsx:139` | `from()`, `to()` | `setLoadingReceipts(true)` + fetch (fetch async; the sync `setLoadingReceipts(true)` moves to apply) |
| `analysis.tsx:185` | `data.state`, `data()`, `filteredCategories()` | chart destroy/rebuild (DOM writes — apply) |

Also audit at upgrade time: 2.0 throws (dev) for writes inside owned
scope and warns for top-level reactive reads in component bodies. The
rows above marked "likely no change" are async continuations, not
owned scope — confirm by running dev mode and reading diagnostics by
code (RFC 08).

**`onCleanup` (kept, check nuance).** `onCleanup` still exists in 2.0
but is reserved for reactive cleanup inside computations; the
component-level setup-and-teardown sites should move to `onSettled`
with a returned cleanup:
- `client/pages/upload.tsx:70` (paste listener) → `onSettled(() => {
  document.addEventListener(...); return () => document.removeEventListener(...) })`
- `client/pages/receipt.tsx:104,108` (photo cancel/revoke),
  `proposal.tsx:179` (timers), `:293` (stream abort),
  `:329` (photo cancel), `item-detail.tsx:89`,
  `analysis.tsx:183` (chart destroy) — these already live inside
  effects/computations, so they stay as reactive cleanup.

## Non-issues inventory (banned during the rewrite, verified by grep)

No `Index` (always `<For>`), no `batch()`, no `mergeProps`/`splitProps`
(manual prop passing), no `unwrap` (no snapshot need), no `classList`
(`class` string/object forms), no `use:` directives (only `ref`
callbacks), no `attr:`/`bool:`/`on:` namespaces, no
`on:`/`oncapture:` handlers. Props are values at call sites
(`<Row item={item()}>`), never destructured accessors. Selects use
per-option `selected=` (order-independent, compiler-wrapped bindings —
valid in both versions). `/*@once*/` never used.

## Packaging consequences

2.0 is ESM-only with `engines.node >= 22.12`. The Go build embeds
`dist/` output only, so the server is unaffected — but any Docker/CI
image building the client needs Node ≥ 22.12. (No Dockerfile exists
in-repo as of this writing; `mise` pins `bun`/`go` toolchains only —
add a node floor when upgrading.)
