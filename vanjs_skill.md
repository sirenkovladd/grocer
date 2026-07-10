# VanJS Development Skill

Best practices and patterns for developing web applications with [VanJS](https://vanjs.org/) (vanjs-core 1.6+).

> "The best solution is usually the one with the least unnecessary complexity" — VanJS tutorial

VanJS is a 1KB reactive UI framework with no virtual DOM, no transpiler, and no opinions about routing/state management. Tag functions return real DOM nodes; `van.state` creates reactive primitives; `van.derive` composes them.

---

## 1. State primitives

### `van.state(init)` — reactive value
```ts
const count = van.state(0)
count.val          // read (registers current binding as a dependency)
count.val = 5      // write (notifies all bindings)
count.rawVal       // read WITHOUT registering a dependency (VanJS ≥1.5.0)
count.oldVal       // previous value during the current render cycle
```

### `van.derive(fn)` — computed state
```ts
const doubled = van.derive(() => count.val * 2)
```
- Recomputes when dependencies change.
- Since VanJS 1.5.0, derivations run **asynchronously** (next microtask) to batch updates.
- A `derive` whose function sets the same state's `.val` is **not** treated as self-referential — VanJS 1.3.0+ ignores writes inside a binding when computing dependencies, so `Reset` buttons can't trigger infinite loops.

---

## 2. DOM construction

### Tag functions
```ts
const { div, button, span, input, table, tr, td } = van.tags
const Hello = () => div(p("👋Hello"), button({ onclick: () => alert("hi") }, "Click"))
```
- Tag functions return real `HTMLElement` objects — you can use them with native DOM APIs.
- Children can be: primitives, `State` objects, functions, arrays, or `null`/`undefined` (filtered out).
- `van.add(parent, ...children)` appends children to an existing node (imperative alternative).

### Don't reuse already-connected nodes
```ts
const existing = document.getElementById("foo")
div(existing)                    // ❌ throws in debug build
van.add(document.body, existing) // ❌ same
```

---

## 3. State binding — the four forms

VanJS supports four ways to bind a `State` to a DOM property/child. Getting these right is the difference between a snappy UI and one that re-creates nodes on every keystroke.

### Form A: `State`-typed prop (pass the State object)
```ts
const text = van.state("")
input({ value: text, oninput: e => text.val = e.target.value })
```
- Two-way binding. VanJS sets `dom.value = text.val` whenever the state changes.
- **The read of `text` (the State object) does NOT register a dependency** — only `.val` reads do. The dependency is registered internally by VanJS when it sets up the binding.
- The DOM element is created once. Only the property is updated.

### Form B: `State`-derived prop (function)
```ts
input({ value: () => text.val.toUpperCase(), oninput: e => text.val = e.target.value })
```
- Computed binding. The function re-runs when dependencies change.
- The element is created once. Only the property is updated.
- Use this when the value is derived from one or more states with logic.

### Form C: `State`-derived child (function as child)
```ts
div(() => text.val ? p(text.val) : p("Enter text"))
```
- The function re-runs when dependencies change, returning a new child.
- **The returned value is REPLACED in the DOM** — the old child is removed, the new one is inserted.
- Return a single element (not an array). Wrap in `span`/`div` if you need multiple.

### Form D: One-time read (`state.val` as a plain value)
```ts
input({ value: text.val, oninput: e => text.val = e.target.value })  // ⚠️ DANGEROUS
```
- Sets `dom.value` once at element creation. The property is NOT reactive.
- **But the `.val` read DOES register the current binding as a dependency of `text`.** When `text.val` changes, the binding re-runs, which can recreate the element.
- This is the bug behind "input loses focus on every keystroke" — see §4 below.

---

## 4. ⚠️ Controlled inputs: pass the State, not `.val`

**Rule:** for `<input>`, `<textarea>`, `<select>`, and similar, always pass the `State` object to `value`, never `state.val`.

```ts
// ✅ Correct — two-way binding, element not recreated
input({ value: text, oninput: e => text.val = e.target.value })

// ❌ Buggy — every keystroke recreates the input and steals focus
input({ value: text.val, oninput: e => text.val = e.target.value })
```

**Why the second form is buggy:**
1. `oninput` fires on each keystroke and sets `text.val`.
2. The read of `text.val` in the `value` prop registered the **enclosing function-child** as a dependency of `text` (VanJS tracks `.val` reads as dependencies, regardless of whether the value flows back into a property).
3. When `text.val` changes, the enclosing function-child re-runs, recreating the entire subtree, destroying the `<input>`, and stealing focus.

This bug only manifests when the read happens inside a function-child context (e.g., a `() => ...` that returns DOM, or inside `van.derive`). Top-level component functions are usually not in a binding context, so the bug is invisible there.

**Fix variants:**

| State type | Fix |
|---|---|
| Primitive state (`van.state("")`) | `value: state` (Form A) — clean two-way binding |
| Object state (`van.state({0: "a", 1: "b"})`) | `value: state.rawVal[index]` — peek without registering a dependency, or read into a local variable at the top of the function-child |
| Read-once initial value | `value: () => state.val` (Form B) — only re-reads when other deps change |

**Diagnostic:** if an input loses focus or cursor position on every keystroke, search for `value: <state>.val` in the file. Replace with `value: <state>` (primitive states) or `value: <state>.rawVal` (object states).

---

## 5. `rawVal` — peek without subscribing

VanJS 1.5.0+ adds `rawVal`, a read that returns the current value **without** registering a dependency:

```ts
const total = van.state(0)
const log = van.derive(() => {
  console.log("current total:", total.rawVal)  // logs on every render, but doesn't re-trigger on total changes
  return total.rawVal + 1                       // participates in this derive, doesn't trigger on its own changes
})
```

**Use `rawVal` when:**
- You need the current value at render time but don't want a re-render when it changes.
- You're inside a binding context and want to avoid leaking a dependency.
- Example: an `<input value={state.rawVal}>` where the browser maintains the value as the user types, and you only read `state.val` on submit.

---

## 6. Minimize the scope of DOM updates

Wrap fast-changing subtrees in their own function-child so unrelated parts of the page don't re-render:

```ts
// ❌ Whole paragraph rebuilds on every keystroke
const Name1 = () => div(() => name.val.trim().length === 0
  ? p("Please enter your name")
  : p("Hello ", b(name)))

// ✅ Only the <b> text node updates; the <p> wrapper is stable
const Name2 = () => {
  const isEmpty = van.derive(() => name.val.trim().length === 0)
  return div(() => isEmpty.val
    ? p("Please enter your name")
    : p("Hello ", b(name)))
}
```

**Rule of thumb:** if a `State` only affects a small part of the UI, derive a boolean/state scoped to that part, and let the function-child switch on the derived state.

---

## 7. State granularity

Prefer many small states over one large object:

```ts
// ❌ One state, every change re-renders every binding
const app = van.state({ a: 1, b: 2, c: 3 })

// ✅ Independent states, only affected bindings re-run
const a = van.state(1)
const b = van.state(2)
const c = van.state(3)
```

Smaller states → smaller dependency graphs → fewer re-renders.

---

## 8. Stateful binding — reuse the DOM node

For hot paths (autocomplete, large lists), a binding function can receive the **current** DOM node and return it unchanged to skip re-creation:

```ts
div((dom?: Element) => {
  if (dom && candidates.val === candidates.oldVal) {
    // Only the selected class changed; mutate the existing DOM
    dom.querySelector(`[data-index="${selectedIndex.oldVal}"]`)?.classList.remove("selected")
    dom.querySelector(`[data-index="${selectedIndex.val}"]`)?.classList.add("selected")
    return dom  // ← VanJS reuses this node, no re-creation
  }
  return SuggestionList({ candidates: candidates.val, selectedIndex: selectedIndex.val })
})
```

`dom` is `undefined` on the first call (initial mount) and the current node on subsequent calls. Return `dom` to skip re-creation; return a new node to replace it; return a primitive for a text node.

---

## 9. Lifecycle hooks

VanJS has no built-in `onMount`/`onUnmount`. Three patterns:

1. **`setTimeout(..., 0)`** — quick & dirty; runs after the next render cycle.
   ```ts
   setTimeout(() => focusEl.focus(), 0)
   ```
2. **`van.derive` trigger** — VanJS 1.5.0+ runs derivations after the current render cycle.
   ```ts
   const mount = (el: HTMLElement) => { /* ... */ }
   const trigger = van.state(false)
   van.derive(() => { if (trigger.val) mount(el) })
   trigger.val = true
   ```
3. **Web Components** — `connectedCallback`/`disconnectedCallback` for reliable mount/unmount. The `van_element` addon wraps this.

---

## 10. Async components

Fetch data and store in a `State`; render with a function-child that checks for `null`:

```ts
const AsyncData = () => {
  const data = van.state<Data | null>(null)
  fetch("/api/data").then(r => r.json()).then(d => data.val = d)
  return div(() => data.val ? JSON.stringify(data.val) : "Loading…")
}
```

**GC warning:** VanJS garbage-collects bindings on disconnected DOM nodes. If you build a tree, set a state, and the element hasn't been added to the document yet, the binding may be GC'd. Always:
1. Build the full DOM tree (synchronously, no `await` in the middle).
2. Connect it to the document.
3. Then trigger state changes.

---

## 11. Conditional rendering

```ts
// ✅ Preferred — empty string keeps a placeholder text node VanJS can update
() => isVisible.val ? p("Visible") : ""

// ⚠️ Risky — returning null permanently removes the node from the tree.
// VanJS won't bring it back even when the condition flips back to true.
() => isVisible.val ? p("Visible") : null
```

Use `""` for "toggle visibility" cases; use `null` only when the element should never reappear.

---

## 12. Function-children and dep-tracking scopes

A function-child creates its **own** dependency-tracking context. Reads of `.val` inside the function are scoped to that function-child:

```ts
// Reads inside the inner () => are scoped to this <select>; they don't
// leak to the surrounding App binding (which would cause infinite loops).
() => select({ value: filter, onchange: e => filter.val = e.target.value },
  option({ value: "" }, "All"),
  ...Object.values(items.val).map(it => option({ value: it.id }, it.name)),
)
```

**When a function-child must return a single element** (not an array), wrap with a pass-through container if needed:

```ts
// ❌ Returns an array of options; not a single element
select({ ... }, () => [
  option({ value: "" }, "All"),
  ...items.val.map(it => option({ value: it.id }, it.name)),
])

// ✅ Returns a single <select> element
() => select({ ... },
  option({ value: "" }, "All"),
  ...items.val.map(it => option({ value: it.id }, it.name)),
)
```

---

## 13. Project-specific patterns (this codebase)

- **Entry point:** `client/main.ts` — owns auth, API client, SPA router.
- **Pages:** `client/pages/<name>.ts` — default-export a function that returns DOM.
- **Components:** `client/components/<name>.ts` — named exports, no router.
- **Styling:** CSS classes in `client/styles/main.css`, toggled via state (not inline styles).
- **API client:** always use `api.get/post/patch/delete` from `main.ts` — never raw `fetch()`. `api.fetch` auto-attaches `X-Timezone`.
- **ID precision:** uint64 IDs use `,string` JSON tags. Use `idStr(value)` before sending, `parseInt(value)` for the URL path.
- **State scoping:** the proposal page defines `editName`/`editQty`/`editPrice` at the component level (not per-row) because only one row is in edit mode at a time. The receipt page uses per-index `Record<number, string>` states because all rows edit simultaneously.
- **No `vanjs-ui` / `vanjs-ext`** — `vanjs-core` only.

---

## 14. Quick reference: common bugs

| Symptom | Cause | Fix |
|---|---|---|
| Input loses focus on every keystroke | `value: state.val` reads `.val` in a binding context | `value: state` (primitive) or `value: state.rawVal[...]` (object) |
| `select` shows wrong selected option | `select.value` set before `<option>` children are appended | Mark the matching `option({ selected: true })`; wrap entire `<select>` in a function-child |
| Infinite re-render loop | Page component reads a state that's written during its own render | Make reads local to a function-child so the write doesn't re-trigger the page |
| `null` child doesn't reappear | Returned `null` from a function-child | Return `""` instead |
| Stale data after page mount | `setState` before DOM tree is connected | Build tree → connect → then setState |
| `van.derive` reads `a`, never updates | Used `a.rawVal` instead of `a.val` | Use `a.val` to register the dependency |
| `van.derive` re-runs infinitely | Write to a state whose `.val` was read in the same derive | VanJS 1.3.0+ guards against this; check your version |

---

## 15. Version-specific notes

- **1.4.0:** `van.tags(<namespaceURI>)` replaces `van.tagsNS`.
- **1.5.0:** `rawVal` peek; `van.derive` is asynchronous; `dom` parameter in stateful binding.
- **1.5.3:** `is` option in `createElement`.
- **1.6.x:** This codebase. All features above are available.
