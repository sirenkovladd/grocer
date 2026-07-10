import van from "vanjs-core"
import { api, idStr, navigate } from "../main"

const { div, h1, h3, p, button, input, a } = van.tags

// ID fields are `string` (uint64 precision safety). See ticket 04.
interface Item {
  itemId: string
  name: string
  categoryId: string
  merchantId: string
  normalized: string
  aliases: string[]
}

// Pairs flagged for the user's review. We use a "substring or
// shared first word" heuristic to keep the false-positive rate
// low while still catching the common cases the LLM produces
// ("Whole Milk 2%" vs "2% Milk", "Banana" vs "Bananas", etc.).
//
// Returns an array of pairs (a, b). Each pair represents a
// potential merge. We only return each unordered pair once.
const findSimilarPairs = (items: Item[]): { a: Item; b: Item }[] => {
  const pairs: { a: Item; b: Item }[] = []
  for (let i = 0; i < items.length; i++) {
    for (let j = i + 1; j < items.length; j++) {
      if (isSimilar(items[i], items[j])) {
        pairs.push({ a: items[i], b: items[j] })
      }
    }
  }
  return pairs
}

// isSimilar returns true when two items are likely the same thing
// and should be merged. Three cheap heuristics, in order of
// confidence:
//
//   1. Case-insensitive equality of names
//   2. One name contains the other (min 3 chars, prevents "Egg"
//      matching "Eggnog")
//   3. Same first word (handles "Whole Milk" vs "Milk")
//
// These are intentionally conservative — a false negative means
// the user just has to merge manually. A false positive would
// collapse two distinct items, which is much harder to undo.
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

const MergeItemsPage = () => {
  const items = van.state<Item[]>([])
  const loading = van.state(true)
  const error = van.state<string | null>(null)
  const search = van.state("")
  // IDs of items currently being merged, so the corresponding
  // button can show "Merging…" and be disabled.
  const mergingKey = van.state<string>("")

  const load = async () => {
    loading.val = true
    error.val = null
    try {
      const data = await api.get("/items")
      items.val = Array.isArray(data) ? data : []
    } catch (err) {
      error.val = (err as Error).message || "Failed to load items"
    } finally {
      loading.val = false
    }
  }

  load()

  // Filter the auto-detected pairs by the search box. If the user
  // types a word, only show pairs where either item name matches.
  // This makes it easy to find a specific merge candidate in a
  // long list.
  const filteredPairs = (): { a: Item; b: Item }[] => {
    const allPairs = findSimilarPairs(items.val)
    const s = search.val.trim().toLowerCase()
    if (!s) return allPairs
    return allPairs.filter(p =>
      p.a.name.toLowerCase().includes(s) ||
      p.b.name.toLowerCase().includes(s),
    )
  }

  // Merge source into target. The source item is deleted; every
  // receipt that referenced the source is rewritten to reference
  // the target. Backend returns the number of line items retargeted.
  const doMerge = async (source: Item, target: Item) => {
    const key = `${source.itemId}->${target.itemId}`
    if (!confirm(
      `Merge "${source.name}" into "${target.name}"?\n\n` +
      `This will:\n` +
      `  • Retarget every receipt currently using "${source.name}" to use "${target.name}"\n` +
      `  • Delete the "${source.name}" item\n\n` +
      `This cannot be undone.`
    )) {
      return
    }
    mergingKey.val = key
    try {
      const result = await api.post(`/items/${source.itemId}/merge`, {
        targetId: idStr(target.itemId),
      })
      await load()
      const n = (result as any).retargeted ?? 0
      alert(`Merged. ${n} receipt line item${n === 1 ? "" : "s"} retargeted.`)
    } catch (err) {
      alert(`Merge failed: ${(err as Error).message}`)
    } finally {
      mergingKey.val = ""
    }
  }

  return div({ class: "merge-items-page" },
    div({ class: "page-header" },
      h1("Merge Items"),
      button({ onclick: () => navigate("/items"), class: "btn-secondary" }, "Back to Items"),
    ),

    p({ class: "muted" },
      "Suggested pairs of items that look like duplicates. ",
      "Click a direction to merge one into the other; receipts will be retargeted automatically.",
    ),

    div({ class: "filter-bar" },
      input({
        type: "search",
        class: "search-input",
        placeholder: "Filter pairs…",
        value: search,
        oninput: (e: Event) => {
          search.val = (e.target as HTMLInputElement).value
        },
      }),
    ),

    () => {
      if (error.val) {
        return div({ class: "empty-state" },
          h3("Couldn't load items"),
          p(error.val),
          button({ onclick: load }, "Try Again"),
        )
      }
      if (loading.val) {
        return div({ class: "muted" }, "Loading…")
      }
      const pairs = filteredPairs()
      if (pairs.length === 0) {
        return div({ class: "empty-state" },
          h3("No similar items found"),
          p("Either the catalog is clean, or your filter is too narrow."),
          search.val
            ? button({ onclick: () => { search.val = "" } }, "Clear search")
            : "",
        )
      }
      return div({ class: "merge-pairs" },
        ...pairs.map(pair => {
          const k1 = `${pair.a.itemId}->${pair.b.itemId}`
          const k2 = `${pair.b.itemId}->${pair.a.itemId}`
          const isMerging = (k: string) => mergingKey.val === k
          return div({ class: "merge-pair card" },
            div({ class: "merge-side" },
              // Link the name to the item detail page so the user
              // can verify the catalog entry (category, price
              // history, aliases) before deciding which direction
              // to merge. Opens in a new tab so the merge page stays
              // in context — same pattern as the proposal review
              // page. Reuses .item-name-link for visual consistency.
              a({
                href: `#/items/${pair.a.itemId}`,
                target: "_blank",
                rel: "noopener",
                class: "merge-name item-name-link",
                title: `Opens "${pair.a.name}" in a new tab`,
              }, pair.a.name),
              button({
                class: "btn-sm btn-secondary",
                disabled: () => mergingKey.val !== "",
                onclick: () => doMerge(pair.a, pair.b),
              }, () => isMerging(k1) ? "Merging…" : `Merge into "${pair.b.name}" ↓`),
            ),
            div({ class: "merge-arrow" }, "⇄"),
            div({ class: "merge-side" },
              a({
                href: `#/items/${pair.b.itemId}`,
                target: "_blank",
                rel: "noopener",
                class: "merge-name item-name-link",
                title: `Opens "${pair.b.name}" in a new tab`,
              }, pair.b.name),
              button({
                class: "btn-sm btn-secondary",
                disabled: () => mergingKey.val !== "",
                onclick: () => doMerge(pair.b, pair.a),
              }, () => isMerging(k2) ? "Merging…" : `Merge into "${pair.a.name}" ↓`),
            ),
          )
        }),
      )
    },
  )
}

export default MergeItemsPage
