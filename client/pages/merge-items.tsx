import { createSignal, For, Show } from "solid-js"
import QueryBoundary from "../components/QueryBoundary.tsx"
import { createQuery } from "../lib/query.ts"
import { api, idStr, navigate } from "../lib/api.ts"

// ID fields are `string` (uint64 precision safety).
interface Item {
  itemId: string
  name: string
  categoryId: string
  merchantId: string
  normalized: string
  aliases: string[]
}

// Pairs flagged for review: "substring or shared first word"
// heuristic — low false-positive rate while catching the common
// LLM cases ("Whole Milk 2%" vs "2% Milk", "Banana" vs "Bananas").
// Each unordered pair returned once.
const findSimilarPairs = (items: Item[]): { a: Item; b: Item }[] => {
  const pairs: { a: Item; b: Item }[] = []
  for (let i = 0; i < items.length; i++) {
    for (let j = i + 1; j < items.length; j++) {
      if (isSimilar(items[i]!, items[j]!)) {
        pairs.push({ a: items[i]!, b: items[j]! })
      }
    }
  }
  return pairs
}

// Three cheap heuristics, in order of confidence:
//
//   1. Case-insensitive equality of names
//   2. One name contains the other (min 3 chars, prevents "Egg"
//      matching "Eggnog")
//   3. Same first word (handles "Whole Milk" vs "Milk")
//
// Intentionally conservative — a false negative means a manual
// merge; a false positive collapses two distinct items, much
// harder to undo.
const isSimilar = (a: Item, b: Item): boolean => {
  const an = a.name.toLowerCase().trim()
  const bn = b.name.toLowerCase().trim()
  if (an === bn) return true
  if (an.length >= 3 && bn.length >= 3) {
    if (an.includes(bn) || bn.includes(an)) return true
  }
  const aFirst = an.split(/\s+/)[0]
  const bFirst = bn.split(/\s+/)[0]
  if (aFirst && bFirst && aFirst === bFirst && aFirst.length >= 3) {
    return true
  }
  return false
}

export default function MergeItemsPage() {
  const [search, setSearch] = createSignal("")
  // IDs of items currently being merged ("sourceId->targetId"), so
  // the button shows "Merging…" and disables.
  const [mergingKey, setMergingKey] = createSignal("")

  const { data, refetch } = createQuery(async () => {
    const items = await api.get("/items")
    return (Array.isArray(items) ? items : []) as Item[]
  })

  // Filter auto-detected pairs by the search box: only pairs where
  // either name matches. Finds one candidate in a long list.
  const filteredPairs = (): { a: Item; b: Item }[] => {
    const allPairs = findSimilarPairs(data()!)
    const s = search().trim().toLowerCase()
    if (!s) return allPairs
    return allPairs.filter(
      (p) => p.a.name.toLowerCase().includes(s) || p.b.name.toLowerCase().includes(s),
    )
  }

  // Merge source into target: source deleted, referencing receipts
  // rewritten. Backend returns retargeted line-item count.
  const doMerge = async (source: Item, target: Item) => {
    const key = `${source.itemId}->${target.itemId}`
    if (
      !confirm(
        `Merge "${source.name}" into "${target.name}"?\n\n` +
          `This will:\n` +
          `  • Retarget every receipt currently using "${source.name}" to use "${target.name}"\n` +
          `  • Delete the "${source.name}" item\n\n` +
          `This cannot be undone.`,
      )
    ) {
      return
    }
    setMergingKey(key)
    try {
      const result = await api.post(`/items/${source.itemId}/merge`, {
        targetId: idStr(target.itemId),
      })
      refetch()
      const n = (result as any).retargeted ?? 0
      alert(`Merged. ${n} receipt line item${n === 1 ? "" : "s"} retargeted.`)
    } catch (err) {
      alert(`Merge failed: ${(err as Error).message}`)
    } finally {
      setMergingKey("")
    }
  }

  // Body reads the resource (suspending until ready). Closure
  // component so data reads stay inside the boundary.
  const Body = () => {
    const pairs = () => filteredPairs()
    const busy = () => mergingKey() !== ""
    return (
      <Show
        when={pairs().length > 0}
        fallback={
          <div class="empty-state">
            <h3>No similar items found</h3>
            <p>Either the catalog is clean, or your filter is too narrow.</p>
            <Show when={search()}>
              <button onClick={() => setSearch("")}>Clear search</button>
            </Show>
          </div>
        }
      >
        <div class="merge-pairs">
          <For each={pairs()}>
            {(pair) => {
              const k1 = `${pair.a.itemId}->${pair.b.itemId}`
              const k2 = `${pair.b.itemId}->${pair.a.itemId}`
              return (
                <div class="merge-pair card">
                  <div class="merge-side">
                    {/* Name links to item detail (new tab, merge page
                        stays in context). Same pattern as proposal
                        review. */}
                    <a
                      href={`#/items/${pair.a.itemId}`}
                      target="_blank"
                      rel="noopener"
                      class="merge-name item-name-link"
                      title={`Opens "${pair.a.name}" in a new tab`}
                    >
                      {pair.a.name}
                    </a>
                    <button
                      class="btn-sm btn-secondary"
                      disabled={busy()}
                      onClick={() => doMerge(pair.a, pair.b)}
                    >
                      {mergingKey() === k1 ? "Merging…" : `Merge into "${pair.b.name}" ↓`}
                    </button>
                  </div>
                  <div class="merge-arrow">⇄</div>
                  <div class="merge-side">
                    <a
                      href={`#/items/${pair.b.itemId}`}
                      target="_blank"
                      rel="noopener"
                      class="merge-name item-name-link"
                      title={`Opens "${pair.b.name}" in a new tab`}
                    >
                      {pair.b.name}
                    </a>
                    <button
                      class="btn-sm btn-secondary"
                      disabled={busy()}
                      onClick={() => doMerge(pair.b, pair.a)}
                    >
                      {mergingKey() === k2 ? "Merging…" : `Merge into "${pair.a.name}" ↓`}
                    </button>
                  </div>
                </div>
              )
            }}
          </For>
        </div>
      </Show>
    )
  }

  return (
    <div class="merge-items-page">
      <div class="page-header">
        <h1>Merge Items</h1>
        <button onClick={() => navigate("/items")} class="btn-secondary">
          Back to Items
        </button>
      </div>

      <p class="muted">
        Suggested pairs of items that look like duplicates. Click a direction to merge one into the
        other; receipts will be retargeted automatically.
      </p>

      <div class="filter-bar">
        <input
          type="search"
          class="search-input"
          placeholder="Filter pairs…"
          value={search()}
          onInput={(e) => setSearch(e.currentTarget.value)}
        />
      </div>

      <QueryBoundary pending={<div class="muted">Loading…</div>}>
        <Body />
      </QueryBoundary>
    </div>
  )
}
