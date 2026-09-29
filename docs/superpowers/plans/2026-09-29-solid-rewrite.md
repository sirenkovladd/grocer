# VanJS → SolidJS Client Rewrite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rewrite the `client/` SPA from VanJS to SolidJS, keeping every route, API contract, and pixel of existing CSS working.

**Architecture:** Vite builds `client/` to `dist/` (same contract the Go server embeds via `webcontent` + SPA fallback in `internal/api/router.go`). Framework-agnostic modules (`utils.ts`, `photos.ts`, imperative `cropper.ts`/`zoomable-image.ts` DOM logic) are kept; only the reactive/UI layer is ported. All async data access goes through one `createQuery` helper so the single biggest Solid 1.x → 2.0 churn point (`createResource` removal) is a one-file change later.

**Tech Stack:** SolidJS **1.9.15** (stable — see version decision below), `@solidjs/router` with hash integration, Vite + `vite-plugin-solid`, TypeScript with `jsxImportSource: solid-js`, Chart.js (kept, wrapped), existing hand-rolled `client/styles/main.css` (kept verbatim, no new UI library).

**Spec:** This plan's spec is the existing client itself (`client/**/*.ts`, ~8.7k lines, 13 pages + 4 components) plus the Solid 2.0 migration guide and cheatsheet (read during planning — key deltas captured in "Solid 1.x → 2.0 upgrade notes" so code is written 2.0-ready). No behavior changes except where VanJS bugs are documented as fixed (re-render loop workarounds in `main.ts` comments are deleted, not ported).

## Global Constraints

- NEVER run `mise run start_server` from an agent session (long-running, blocks tool runtime, leaves :8080 bound). Verify with `vite build` / `tsc` only; browser smoke checks are done by the user.
- API calls go via the shared `api` helper (auto-attaches `X-Timezone`, handles 401 → `/login`) — never raw `fetch()` except the existing `postFormData` photo path.
- IDs are strings on the wire (backend serializes uint64 as JSON string); keep using `idStr()` for transport.
- Session auth is an HttpOnly cookie; the client never reads/stores tokens.
- Every route except `/login` is protected; unknown paths render the 404 branch.
- Keep the hash URL scheme (`#/receipts/123`, `#/items/456`, `?item=` query on receipts) — Telegram/Discord bot links and `target="_blank"` item links depend on it.
- `bun test client/utils.test.ts` must keep passing; `utils.ts` and `photos.ts` stay framework-free.
- Solid version floor: `solid-js@1.9.15` + `@solidjs/router@^0.15` + `vite-plugin-solid@^2.11`. Do NOT install `solid-js@next` / `@solidjs/vite-plugin@3.x-next`.

## Version decision: Solid 1.9.15 now, 2.0-ready patterns (locks the user's "2.0 if possible, else 1.9.15" choice)

Solid 2.0 is at **rc.10 (Sept 2026, `next` dist-tag)** with open install breakage (solidjs/solid#3680: no published runtime+compiler+vite-plugin combination installs cleanly; fixed only in unpublished `next.45/46`), requires **Node ≥ 22.12**, is **ESM-only**, and **no published UI library supports it** (Kobalte, Corvu, Solid-UI all target 1.x; Hope UI v1 explicitly waits for 2.0 stable before publishing). Building grocer on the RC means pinning broken prereleases with no component ecosystem.

Therefore: implement on **stable 1.9.15**, but write code that isolates every API known to change in 2.0 (table below, sourced from `MIGRATION.md` + `CHEATSHEET.md`). The 2.0 upgrade later is then ~2 files + mechanical effect rewrites instead of 17 files.

| We write (1.9.x) | Changes in 2.0 | Isolation |
|---|---|---|
| `createResource` for fetches | REMOVED → async `createMemo` + `<Loading>` | Hidden inside `client/lib/query.ts` `createQuery()`; pages never import `createResource` directly |
| `<Suspense>` / `<ErrorBoundary>` | Renamed `<Loading>` / `<Errored>` | Hidden inside `client/components/QueryBoundary.tsx`; pages never import them directly |
| `onMount` | Replaced by `onSettled` (returns cleanup) | Only used in 3 wrapper spots (Chart, cropper, zoomable); each is a one-line rename later |
| `Index` | REMOVED → `<For keyed={false}>` | Banned; always use `<For>` |
| `batch()` | Default microtask batching + `flush()` | Banned; rely on default batching |
| `createEffect(fn)` single-arg | Split `(compute, apply)` two-arg | Write effects as `createEffect(() => { const v = src(); ... })` with the read-first shape so the compute/apply split point is obvious; documented per task |
| `mergeProps`/`splitProps`, `unwrap`, `classList`, ` solid-js/web` imports | `merge`/`omit`, `snapshot`, `class={...}`, `@solidjs/web` | Banned from new code; use the 2.0 names where they already exist in 1.9 (`merge`/`omit` don't exist in 1.9 — so just avoid both: spread props manually) |
| `use:` directives, `attr:`/`bool:`/`on:` namespaces | `ref={factory()}` directive factories, plain attrs | Banned; use `ref` callbacks + `onClick` camelCase handlers only |

## UI library decision: none — reuse `main.css` verbatim

All published Solid UI kits (Kobalte, Corvu, Solid-UI/shadcn port, SUID) target Solid 1.x render internals and would need re-validation against 2.0 later; Tailwind-based kits would force rewriting the 2180-line hand-tuned stylesheet (receipt cards, cropper overlay, skeleton rows, mobile tab bar) for zero functional gain in a family-scale tool. Keep `client/styles/main.css` byte-identical, keep class names identical in ported JSX, and revisit Kobalte primitives only if a future dialog/menu need arises (tracked as out-of-scope follow-up, not this plan).

## Review Focus

- Hash deep-links (`#/receipts/123` from bots, `#/items/456` in new tabs, `/receipts?item=123` from item detail) must resolve to the same page as today — reasonable person expects old links to keep working.
- 401 on any API call must land on `/login` with no stuck spinner — the auth-cookie expiry path is the most common failure a real user hits.
- Proposal page (850 lines, TOML copy/paste, per-row edit state, photo zoom) must not lose edits on re-render — the page where state-loss would cost real user work.
- Upload+crop flow must produce the identical `multipart/form-data` payload (`onCrop` blob + `originalHash`, `onSkip` path) — a silent payload change breaks receipt parsing server-side.
- Analysis page must render identical Chart.js output and identical CSV bytes (`toCsv` CRLF/RFC-4180) — the export is consumed by Excel/Numbers.

---

## File structure (what gets created / modified / kept)

```
client/
  index.html                  MODIFY (Vite entry: /src/main.tsx script tag)
  main.ts                     DELETE → replaced by:
  main.tsx                    CREATE — render() mount + <Router> + global CSS import
  App.tsx                     CREATE — Layout + Sidebar + route table (from main.ts)
  lib/
    api.ts                    CREATE — api.fetch/get/post/patch/delete/postFormData + idStr + navigate re-export (moved verbatim from main.ts)
    query.ts                  CREATE — createQuery() wrapping createResource (the 2.0 seam)
  components/
    QueryBoundary.tsx         CREATE — <Suspense>+<ErrorBoundary> wrapper (the 2.0 seam)
    receipt-card.tsx          PORT from receipt-card.ts (JSX, thumb via createQuery/onMount)
    date-range.tsx            PORT from date-range.ts (trivial signals port)
    zoomable-image.tsx        PORT — keep zoomable-image.ts math/DOM file, add Solid wrapper (ref + onMount/onCleanup)
    cropper.tsx               PORT — keep cropper.ts imperative file as-is, add Solid wrapper (ref + onMount/onCleanup)
  pages/*.tsx                 PORT all 13 pages, .ts → .tsx (login, home, receipts, receipt, upload, manual-receipt, proposal, items, item-detail, merge-items, merchants, categories, analysis)
  utils.ts / photos.ts / utils.test.ts   KEEP UNTOUCHED (framework-free)
  styles/main.css             KEEP byte-identical
vite.config.ts                CREATE — solid plugin, outDir dist, /api proxy for dev
tsconfig.json                 MODIFY — jsx: preserve, jsxImportSource: solid-js
package.json                  MODIFY — drop vanjs-core, add solid-js@1.9.15 @solidjs/router vite vite-plugin-solid
mise.toml                     MODIFY — build_client/build_client_prod call vite instead of bun build
```

### VanJS → Solid pattern map (normative for every port task)

- `van.state(x)` → `createSignal(x)`; `.val` reads inside JSX stay bare calls (`count()`), event handlers write via setters.
- VanJS function-child `() => ...` for conditionals → `<Show when={} fallback={}>`; for lists → `<For each={}>`; plain derivations → function calls in JSX (no `createMemo` unless measured).
- `value: someState` two-way binding + `oninput` → `value={s()} onInput={e => setS(e.currentTarget.value)}` (Solid is one-way; handler writes the signal).
- `select(...options-from-state)` dynamic-children loop hazard (see AGENTS.md VanJS Gotchas) → disappears: `<select>` with `<For>` children is the normal Solid shape; do NOT port the function-child-wraps-select workaround.
- Component functions returning `div(...)` trees → components returning JSX; props are values (`<Row item={item()}>`), never accessors; never destructure reactive props in the signature (use `props.x`).
- Fire-and-forget loads in component body (`loadProposals(); loadReceipts()` in home.ts) → `createQuery()` (Suspense-tracked, no manual `loading`/`error` states, no double-fetch on re-render).
- `setPageTitle(...)` calls stay as-is (plain DOM write), moved into `createEffect` where they were inside VanJS function-children.
- `confirm()` / `clipboard` / `downloadFile` imperative helpers stay as-is.
- Chart.js: canvas `ref={el => canvasEl = el}`, chart created in `onMount`, `createEffect` updates data + `chart.update()`, `onCleanup(() => chart.destroy())`. Exactly the 3 allowed `onMount` sites (chart, cropper, zoomable).

---

### Task 0: Toolchain — Vite + Solid 1.9.15 + typecheck (no UI yet)

**Files:**
- Create: `vite.config.ts`, `client/main.tsx` (temporary "Hello" mount only), `client/lib/api.ts` (stub re-export, fleshed in Task 1)
- Modify: `package.json`, `tsconfig.json`, `client/index.html`, `mise.toml`
- Test: `vite build` output + `bun test client/utils.test.ts`

**Interfaces:**
- Consumes: Go SPA fallback contract (`dist/index.html` + assets, any path → `index.html`).
- Produces: `client/lib/api.ts` → `api.{fetch,get,post,patch,delete,postFormData}`, `idStr()`, `navigate()` signatures reused by every later task.

- [ ] **Step 1: Add dependencies, remove VanJS**

Run: `bun add solid-js@1.9.15 @solidjs/router && bun add -d vite vite-plugin-solid && bun remove vanjs-core`
Expected: `package.json` has no `vanjs-core`; `solid-js` is `1.9.15` (not `next`).

- [ ] **Step 2: Write `vite.config.ts`**

`import { defineConfig } from "vite"; import solid from "vite-plugin-solid"` with `plugins: [solid()]`, `build.outDir: "dist"`, `server.proxy: { "/api": "http://localhost:8080" }`, `appType: "spa"`. (OutDir `dist` preserves the Go `webcontent` embed contract; proxy preserves the same-origin cookie flow in dev.)

- [ ] **Step 3: Update `tsconfig.json`, `client/index.html`, `mise.toml`**

`tsconfig` gains `"jsx": "preserve"`, `"jsxImportSource": "solid-js"`, `"types": ["vite/client"]`. `index.html` script becomes `<script type="module" src="/main.tsx">` + keep CSS link. `mise.toml` `build_client` → `bunx vite build --sourcemap --outdir=dist ./client`, `build_client_prod` → `bunx vite build --outdir=dist ./client` (keep `rm -rf dist` first line in each). Grep `deploy/Dockerfile` for `bun build`/`build_client` and mirror the change there if present.

- [ ] **Step 4: Write temporary `client/main.tsx` + `client/lib/api.ts` stub, then verify**

`main.tsx` renders `<div>Solid mount OK</div>` via `render()` from `solid-js/web` into `#app`. `api.ts` exports the exact `api`/`idStr`/`navigate` signatures (copy bodies verbatim from `client/main.ts:33-105`).
Run: `bunx tsc --noEmit && bunx vite build --outdir=dist ./client && bun test client/utils.test.ts`
Expected: typecheck clean, `dist/index.html` + assets exist, utils tests pass.

- [ ] **Step 5: Commit**

```bash
git add vite.config.ts package.json tsconfig.json client/index.html mise.toml client/main.tsx client/lib/api.ts
git commit -m "chore: vite + solid 1.9.15 toolchain, dist contract preserved"
```

---

### Task 1: App shell + router + login (first real route)

**Files:**
- Create: `client/App.tsx`, `client/components/QueryBoundary.tsx`, `client/lib/query.ts`, `client/pages/login.tsx`
- Modify: `client/main.tsx` (mount `<App/>` under hash router)
- Delete: nothing yet (old `main.ts` deleted in final task)

**Interfaces:**
- Consumes: `api`, `idStr`, `navigate` from `client/lib/api.ts`.
- Produces: `QueryBoundary` → `<QueryBoundary pending={<Skeleton/>} error={fn}>{children}</QueryBoundary>` used by every page task; `createQuery<T>(key: string, fetcher: () => Promise<T>)` returning a 1.x `Resource<T>` used by every page task (2.0 seam: reimplement on async-memo + `<Loading>` without changing call sites).

- [ ] **Step 1: Implement `createQuery` in `client/lib/query.ts`**

```ts
export function createQuery<T>(fetcher: () => Promise<T>): Resource<T> {
  const [data] = createResource(fetcher)
  return data
}
```

Fetcher identity: fetchers close over signals (e.g. `() => api.get(...)`) so later tasks can add `createEffect(on(...))` refetch without changing the helper. No `batch`, no manual loading flags.

- [ ] **Step 2: Implement `QueryBoundary` + `App.tsx` (Sidebar/Layout/routes)**

`QueryBoundary` = `<ErrorBoundary fallback={(err, reset) => (...Try Again → reset()...)}><Suspense fallback={props.pending}>{props.children}</Suspense></ErrorBoundary>`. `App.tsx` ports `Sidebar`/`Layout`/`routeName`/`defaultTitleFor`/`PageContent` from `client/main.ts:112-260` to JSX + `@solidjs/router` `<Router source={hashIntegration()}>` with routes: `/login`, `/`, `/receipts`, `/receipts/upload`, `/receipts/manual`, `/receipts/:id`, `/proposals/:id`, `/items`, `/items/merge`, `/items/:id`, `/merchants`, `/categories`, `/analysis`, `*404`. Active-link highlight via `useLocation().pathname` (replaces `aria-current: () => currentPath.val === ...`). Delete the nested-function-child comment/workaround — it has no Solid equivalent. `withErrorBoundary` is replaced by `QueryBoundary`; do not port it.

- [ ] **Step 3: Port `pages/login.ts` → `login.tsx`**

Signals for `username/password/error`; `onSubmit` handler identical (`api.post("/auth/login", ...)` → `navigate("/receipts")`; catch → `"Login failed"`). Inputs become `value={username()} onInput={...}` one-way bindings.

- [ ] **Step 4: Verify**

Run: `bunx tsc --noEmit && bunx vite build --outdir=dist ./client`
Expected: clean build. User smoke (they run the server per AGENTS.md): `/login` renders, wrong creds → "Login failed", correct creds → `#/receipts` (stub 404 page until Task 3), sidebar highlights, sign-out → `/login`.

- [ ] **Step 5: Commit**

```bash
git add client/App.tsx client/main.tsx client/lib/query.ts client/components/QueryBoundary.tsx client/pages/login.tsx
git commit -m "feat: solid app shell, hash router, login page"
```

---

### Task 2: Shared components (receipt-card, date-range, zoomable, cropper wrappers)

**Files:**
- Create: `client/components/receipt-card.tsx`, `date-range.tsx`, `zoomable-image.tsx`, `cropper.tsx`
- Keep: `client/components/cropper.ts`, `zoomable-image.ts` logic files UNTOUCHED (imported by the wrappers)

**Interfaces:**
- Consumes: `QueryBoundary`, `createQuery`, `api` helpers, `EnrichedReceiptSummary` type (moved into `receipt-card.tsx` and imported by pages).
- Produces: `ReceiptCard(r: EnrichedReceiptSummary)`, `DateRange(props: { from: Accessor<string>, setFrom, to, setTo, onChange? })`, `ZoomableImage(props: { src: string })`, `ImageCropper(props: CropperOptions)` — all as Solid components taking **values/props, owned signals created by the caller** (caller passes getter+setter, not a VanJS state object).

- [ ] **Step 1: Port `receipt-card` + `date-range` (pure UI, no lifecycle)**

`ReceiptCard`: thumbnail via `createEffect`-fired `fetchPhotoUrl(r.receiptId, "thumb")` into a signal (replaces VanJS body-side promise + `thumbUrl` state); `onClick` → `navigate`. `DateRange`: presets + date inputs with one-way bindings; prop drilling replaces VanJS `any`-typed state objects. Both keep class names identical.

- [ ] **Step 2: Write `zoomable-image.tsx` + `cropper.tsx` wrappers (the only `onMount`/`onCleanup` sites besides the chart)**

Wrapper creates container `ref`, instantiates the existing imperative class/function from the kept `.ts` file in `onMount`, tears down in `onCleanup`. No logic is moved or rewritten — if the `.ts` file touches `van` anywhere, extract that line to a callback prop instead of importing VanJS.

- [ ] **Step 3: Verify**

Run: `bunx tsc --noEmit && bunx vite build --outdir=dist ./client`
Expected: clean build (visual check deferred to page tasks that mount them).

- [ ] **Step 4: Commit**

```bash
git add client/components/
git commit -m "feat: shared solid components, imperative wrappers for cropper/zoom"
```

---

### Task 3: Simple pages — receipts, items, merchants, categories, item-detail, merge-items

**Files:**
- Create: `client/pages/receipts.tsx` (257-line port), `items.tsx`, `merchants.tsx`, `categories.tsx`, `item-detail.tsx`, `merge-items.tsx`
- Consumes: `createQuery`, `QueryBoundary`, `ReceiptCard`, `DateRange` from Task 2.

- [ ] **Step 1: Port list pages (receipts, items, merchants, categories)**

Each page: replace `van.state + loadData() + loading/error` triple with `const data = createQuery(() => api.get("/..."))` wrapped in `<QueryBoundary pending={<Skeleton rows/>}>`. Filters (`ownerFilter`, search text, `?item=` query via `parseHashQuery(useLocation().hash)`) stay as local signals; `<For each={filtered()}>` replaces VanJS function-child list rendering — including the `select`-options case, which needs NO workaround in Solid. Keep `formatMoney/formatDate/csvField/toCsv/downloadFile` usage identical (Review Focus: CSV bytes must not change).

- [ ] **Step 2: Port item-detail + merge-items**

Item-detail: `useParams().id` replaces hash parsing; `setPageTitle` moves into `createEffect`; "View receipts" link keeps `#/receipts?item=${id}` shape. Merge-items: two-select + confirm + `api.post("/items/merge")` flow, one-way select bindings.

- [ ] **Step 3: Verify**

Run: `bunx tsc --noEmit && bunx vite build --outdir=dist ./client`
Expected: clean build. User smoke per page: list renders, skeletons show then resolve, filters narrow the list, error path shows Try Again, CSV download byte-identical to VanJS build (compare one export), deep-links work.

- [ ] **Step 4: Commit**

```bash
git add client/pages/receipts.tsx client/pages/items.tsx client/pages/merchants.tsx client/pages/categories.tsx client/pages/item-detail.tsx client/pages/merge-items.tsx
git commit -m "feat: port simple list/detail pages to solid"
```

---

### Task 4: Complex pages — home, receipt detail, proposal, manual-receipt, upload

**Files:**
- Create: `client/pages/home.tsx`, `receipt.tsx`, `proposal.tsx`, `manual-receipt.tsx`, `upload.tsx`
- Consumes: everything from Tasks 1–2.

**Interfaces:**
- Produces: no new shared APIs; proposal's `renderItemRow` stays a local (non-exported) function.

- [ ] **Step 1: Port home + receipt detail**

Home: two `createQuery` calls (`/proposals`, `/receipts/enriched`); `monthStats`/`deltaPct`/`recentReceipts` become plain functions of `query()` values called in JSX (no memo needed at family scale); `ProposalCard`/`statusBadge`/skeletons ported as local components; retry/delete handlers identical (`reparse`, `DELETE`, `confirm()`). Receipt detail: owner/merchant/item enrichment via `indexBy` (unchanged), per-line edit state via a `createStore<Record<index, draft>>` (replaces the VanJS `editItemQty` object-state + `rawVal` workaround — Solid stores track per-key, so inputs bind `value={draft[index] ?? ""}` with no focus loss), approve flow `POST /receipts/:id/approve` unchanged.

- [ ] **Step 2: Port proposal (largest page, 850 lines — state-loss risk is the Review Focus)**

Keep TOML_SCHEMA/LLM_PROMPT/copyToClipboard byte-identical. Per-row edit drafts in a `createStore`; live-total preview cell reads the store in JSX (reactive by design); `<input>`s bind via local-variable reads (never `store.val`-style tracked reads on the input itself — same focus rule as VanJS `rawVal`). `renderItemName` matched-item `target="_blank"` link kept. Photo pane uses `ZoomableImage` wrapper from Task 2. External-TOML apply + approve/reparse/delete handlers call identical endpoints.

- [ ] **Step 3: Port manual-receipt + upload (payload-shape Review Focus)**

Manual-receipt: dynamic item-rows add/remove, submit builds the identical `userInputReceipt` JSON body (field names `merchant/date/total/items[]` as the server's `llm_user_input.go` expects). Upload: `ImageCropper` wrapper from Task 2; `onCrop(blob, originalHash)` and `onSkip(originalHash)` forward to the identical `postFormData` call (same field names, same hash header/query as today).

- [ ] **Step 4: Verify**

Run: `bunx tsc --noEmit && bunx vite build --outdir=dist ./client`
Expected: clean build. User smoke: home stats match VanJS numbers; receipt approve works; proposal edit→approve round-trip preserves typed values (type in a row, blur, confirm no reset); manual receipt creates; upload crop→submit parses; photo zoom works on desktop + touch.

- [ ] **Step 5: Commit**

```bash
git add client/pages/home.tsx client/pages/receipt.tsx client/pages/proposal.tsx client/pages/manual-receipt.tsx client/pages/upload.tsx
git commit -m "feat: port complex pages (home, receipt, proposal, manual, upload)"
```

---

### Task 5: Analysis page (Chart.js) + global cleanup + VanJS removal

**Files:**
- Create: `client/pages/analysis.tsx`
- Modify: delete `client/main.ts`, `client/pages/*.ts`, `client/components/*.ts` (old VanJS files, after all `.tsx` exist); `package.json` (confirm no `vanjs-core`)
- Consumes: `DateRange`, `createQuery`, `groupTrips`/`tripKey` logic (moved verbatim).

**Interfaces:**
- Produces: nothing new; this task closes the port.

- [ ] **Step 1: Port analysis (third and last `onMount` site)**

`groupTrips`/`tripKey` copied verbatim (pure functions). Canvas via `ref`; `new Chart(...)` in `onMount` (third allowed site); data updates in `createEffect(() => { const t = trips(); ...chart.data = ...; chart.update() })` — read-first shape so the 2.0 compute/apply split is mechanical later; `onCleanup(() => chart.destroy())`. `Chart.register(...registerables)` stays at module top. Date-range presets + CSV export (`toCsv`/`downloadFile`) unchanged.

- [ ] **Step 2: Delete VanJS sources, verify zero references**

Delete `client/main.ts` and all old `pages/*.ts`, `components/*.ts` (utils/photos/tests stay). Run: `grep -rn "vanjs-core\|van\.tags\|van\.state\|\.val" client --include="*.tsx" --include="*.ts" | grep -v "utils.test"` — expected: no output except legitimate `.val`-free code (any remaining `.val` is a port bug). Then `bunx tsc --noEmit && bunx vite build --outdir=dist ./client && bunx vite build --production --outdir=dist ./client && bun test client/utils.test.ts`.

- [ ] **Step 3: Verify Go embed + prod bundle**

Run: `ls -la dist/ && grep -c "vanjs" dist/*.js || true` (expected: no vanjs in bundle). Confirm `internal/api/router.go` needs no change (it serves `dist/` + SPA fallback regardless of bundler). User final smoke: full walkthrough (login → home → upload/manual → proposal approve → receipt → analysis CSV) against a user-run server.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat: complete vanjs→solid rewrite, remove vanjs-core"
```

---

### Task 6: Solid 2.0 upgrade notes (docs-only, locks in the 2.0 reading)

**Files:**
- Create: `docs/superpowers/specs/solid-2.0-upgrade.md` (or append to the design doc dir)

- [ ] **Step 1: Write the upgrade doc from the migration guide + cheatsheet**

Contents: (a) trigger condition — 2.0 stable released AND `@solidjs/vite-plugin` stable AND Kobalte/Corvu (if adopted) green; (b) the two seam files to rewrite (`lib/query.ts` → async-memo + `<Loading>`; `components/QueryBoundary.tsx` → `<Loading>`/`<Errored>`); (c) mechanical rewrites — `onMount`→`onSettled` (3 sites listed by file/line), single-arg `createEffect`→`(compute, apply)` split (list each site), `solid-js/web`→`@solidjs/web` imports + `jsxImportSource: @solidjs/web`, `render` from `@solidjs/web`; (d) non-issues inventory — no `Index`, no `batch`, no `mergeProps`/`unwrap`/`classList`/`use:`/namespaced attrs used anywhere (banned by this plan), props already values-not-accessors; (e) Node ≥22.12 + ESM-only packaging consequences for the Dockerfile.

- [ ] **Step 2: Commit**

```bash
git add docs/
git commit -m "docs: solid 2.0 upgrade path from 1.9.15 baseline"
```

---

## Out of scope (explicitly not in this plan)

- Adopting Kobalte/Corvu/Solid-UI or Tailwind; dark mode; design refresh.
- SolidStart / SSR (`renderToString`); the Go server remains the only backend and serves the SPA bundle.
- Changing API contracts, bot links, CSV format, or URL scheme.
- Adding frontend unit tests beyond keeping `utils.test.ts` green (no test runner exists for UI; verification is typecheck + build + user browser smoke).
