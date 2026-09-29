import { createSignal, For, Show } from "solid-js"
import { A } from "@solidjs/router"
import QueryBoundary from "../components/QueryBoundary.tsx"
import { createQuery } from "../lib/query.ts"
import { api, idStr, navigate } from "../lib/api.ts"
import { indexBy, formatRelativeDate } from "../utils.ts"

// ID fields are `string` (uint64 precision safety).
interface Item {
  itemId: string
  name: string
  categoryId: string
  merchantId: string
  normalized: string
  aliases: string[]
  // purchaseCount and lastPurchasedAt come from /api/items/insights.
  // Default 0 / 0 so the type also works for the bare /api/items
  // endpoint (the merge tool only needs the bare list).
  purchaseCount: number
  lastPurchasedAt: number
}

interface Category {
  categoryId: string
  name: string
}

// The insights endpoint may be missing on older servers; or the
// bare endpoint may already return equivalent fields. Coerce the
// shape so the rest of the page can treat the data uniformly.
const normalizeItem = (raw: any): Item => ({
  itemId: String(raw.itemId),
  name: raw.name,
  categoryId: String(raw.categoryId),
  merchantId: String(raw.merchantId),
  normalized: raw.normalized,
  aliases: raw.aliases ?? [],
  purchaseCount: raw.purchaseCount ?? 0,
  lastPurchasedAt: raw.lastPurchasedAt ?? 0,
})

const sortedCategories = (cats: Record<string, Category>): Category[] =>
  Object.values(cats)
    .slice()
    .sort((a, b) => a.name.localeCompare(b.name))

function SkeletonRows() {
  return (
    <div class="items-skeleton">
      <For each={[0, 1, 2, 3, 4]}>
        {() => (
          <div class="skeleton-row">
            <div class="skeleton-cell skeleton-cell-lg" />
            <div class="skeleton-cell skeleton-cell-md" />
            <div class="skeleton-cell skeleton-cell-md" />
          </div>
        )}
      </For>
    </div>
  )
}

function Aliases(props: { aliases: string[] }) {
  if (props.aliases.length === 0) return <span class="muted">—</span>
  const MAX = 3
  const shown = props.aliases.slice(0, MAX).join(", ")
  const extra = props.aliases.length - MAX
  return <span title={props.aliases.join(", ")}>{extra > 0 ? `${shown} +${extra} more` : shown}</span>
}

// Inline edit form rendered as a single spanning row. Submits on Save
// (PATCH /api/items/{id}), bails on Cancel. Client validation: name
// must be non-empty.
function EditForm(props: {
  item: Item
  categories: Record<string, Category>
  name: () => string
  setName: (v: string) => void
  category: () => string
  setCategory: (v: string) => void
  aliases: () => string
  setAliases: (v: string) => void
  editError: () => string
  saving: () => boolean
  onSave: () => void
  onCancel: () => void
}) {
  return (
    <div class="inline-edit-form">
      <div class="edit-field">
        <label for={`edit-name-${props.item.itemId}`}>Name</label>
        <input
          id={`edit-name-${props.item.itemId}`}
          type="text"
          value={props.name()}
          onInput={(e) => props.setName(e.currentTarget.value)}
          disabled={props.saving()}
          class="edit-input"
        />
      </div>
      <div class="edit-field">
        <label for={`edit-cat-${props.item.itemId}`}>Category</label>
        <select
          id={`edit-cat-${props.item.itemId}`}
          onChange={(e) => props.setCategory(e.currentTarget.value)}
          disabled={props.saving()}
          class="edit-input"
        >
          {/* `0` / empty value is the "Uncategorized" sentinel — the
              backend falls back to UnknownCategory for any
              categoryID not in the map, so a missing category is safe. */}
          <option value="" selected={props.category() === ""}>
            Uncategorized
          </option>
          <For each={sortedCategories(props.categories)}>
            {(c) => (
              <option value={c.categoryId} selected={props.category() === c.categoryId}>
                {c.name}
              </option>
            )}
          </For>
        </select>
      </div>
      <div class="edit-field edit-field-wide">
        <label for={`edit-aliases-${props.item.itemId}`}>Aliases (comma-separated)</label>
        <input
          id={`edit-aliases-${props.item.itemId}`}
          type="text"
          value={props.aliases()}
          onInput={(e) => props.setAliases(e.currentTarget.value)}
          disabled={props.saving()}
          class="edit-input"
          placeholder="alias1, alias2, …"
        />
      </div>
      <Show when={props.editError()}>
        <div class="edit-error">{props.editError()}</div>
      </Show>
      <div class="edit-actions">
        <button
          type="button"
          onClick={props.onSave}
          disabled={props.saving() || !props.name().trim()}
          class="btn-sm btn-primary"
        >
          {props.saving() ? "Saving…" : "Save"}
        </button>
        <button
          type="button"
          onClick={props.onCancel}
          disabled={props.saving()}
          class="btn-sm btn-secondary"
        >
          Cancel
        </button>
      </div>
    </div>
  )
}

export default function ItemsPage() {
  // Possible categoryFilter values:
  //   ""                   -> no filter (show all)
  //   "__uncategorized__"  -> items whose categoryId is not in the
  //                           categories map
  //   <categoryId>         -> that specific category
  const UNCATEGORIZED = "__uncategorized__"
  const [search, setSearch] = createSignal("")
  const [categoryFilter, setCategoryFilter] = createSignal("")
  const [sort, setSort] = createSignal<"alpha" | "frequent">("alpha")

  // Edit state — only one row is in edit mode at a time.
  const [editingId, setEditingId] = createSignal("")
  const [editName, setEditName] = createSignal("")
  const [editCategory, setEditCategory] = createSignal("")
  const [editAliases, setEditAliases] = createSignal("")
  const [editError, setEditError] = createSignal("")
  const [saving, setSaving] = createSignal(false)
  const [deletingId, setDeletingId] = createSignal("")

  // Insights endpoint for purchase stats without per-item N+1.
  const { data, refetch } = createQuery(async () => {
    const [i, c] = await Promise.all([api.get("/items/insights"), api.get("/categories")])
    return {
      items: (Array.isArray(i) ? i.map(normalizeItem) : []) as Item[],
      categories: indexBy(Array.isArray(c) ? c : [], (x: Category) => x.categoryId),
    }
  })

  const sorted = (): Item[] => {
    const all = [...data()!.items]
    if (sort() === "frequent") {
      all.sort((a, b) => {
        if (b.purchaseCount !== a.purchaseCount) return b.purchaseCount - a.purchaseCount
        return a.name.toLowerCase().localeCompare(b.name.toLowerCase())
      })
    } else {
      all.sort((a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase()))
    }
    return all
  }

  const filtered = (): Item[] => {
    const s = search().trim().toLowerCase()
    const cat = categoryFilter()
    const cats = data()!.categories
    return sorted().filter((i) => {
      if (cat === UNCATEGORIZED) {
        if (cats[i.categoryId]) return false
      } else if (cat) {
        if (i.categoryId !== cat) return false
      }
      if (!s) return true
      return i.name.toLowerCase().includes(s) || i.aliases.some((a) => a.toLowerCase().includes(s))
    })
  }

  // Populate the edit-* states from the row and enter edit mode.
  const startEdit = (item: Item) => {
    setEditingId(item.itemId)
    setEditName(item.name)
    setEditCategory(item.categoryId)
    // Server stores aliases as []string; round-trip through CSV for
    // the typical small alias list.
    setEditAliases(item.aliases.join(", "))
    setEditError("")
  }

  const cancelEdit = () => {
    setEditingId("")
    setEditError("")
  }

  const saveEdit = async () => {
    const id = editingId()
    if (!id) return
    setSaving(true)
    setEditError("")
    try {
      // Parse aliases — split on commas, trim, drop empties.
      const aliases = editAliases()
        .split(",")
        .map((a) => a.trim())
        .filter((a) => a.length > 0)
      await api.patch(`/items/${id}`, {
        name: editName().trim(),
        categoryId: idStr(editCategory()),
        aliases,
      })
      refetch()
      cancelEdit()
    } catch (err) {
      setEditError((err as Error).message || "Failed to save")
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async (item: Item) => {
    if (!confirm(`Delete "${item.name}"?\n\nThis will fail if any receipt still references this item.`)) {
      return
    }
    setDeletingId(item.itemId)
    try {
      await api.delete(`/items/${item.itemId}`)
      refetch()
    } catch (err) {
      alert(`Delete failed: ${(err as Error).message}`)
    } finally {
      setDeletingId("")
    }
  }

  // Body reads the resource (suspending until ready) and renders
  // filters + table. A closure component so filter-bar signal reads
  // stay inside the boundary instead of suspending the page shell.
  const Body = () => {
    const cats = () => data()!.categories
    const list = () => filtered()
    const hasFilters = () => !!(search() || categoryFilter())
    return (
      <>
        <div class="filter-bar">
          <input
            type="search"
            class="search-input"
            placeholder="Search items or aliases…"
            value={search()}
            onInput={(e) => setSearch(e.currentTarget.value)}
          />
          <select
            onChange={(e) => setSort(e.currentTarget.value as "alpha" | "frequent")}
            aria-label="Sort items"
            class="sort-picker"
          >
            <option value="alpha" selected={sort() === "alpha"}>
              Alphabetical
            </option>
            <option value="frequent" selected={sort() === "frequent"}>
              Most frequently bought
            </option>
          </select>
          <select
            onChange={(e) => setCategoryFilter(e.currentTarget.value)}
            aria-label="Filter by category"
            class="category-filter"
          >
            <option value="" selected={categoryFilter() === ""}>
              All categories
            </option>
            {/* Same position as the EditForm picker so users see the
                same "Uncategorized" entry in both places. */}
            <option value={UNCATEGORIZED} selected={categoryFilter() === UNCATEGORIZED}>
              Uncategorized
            </option>
            <For each={sortedCategories(cats())}>
              {(c) => (
                <option value={c.categoryId} selected={categoryFilter() === c.categoryId}>
                  {c.name}
                </option>
              )}
            </For>
          </select>
        </div>

        <Show
          when={data()!.items.length > 0}
          fallback={
            <div class="empty-state">
              <h3>No items yet</h3>
              <p>Upload a receipt to get started.</p>
              <button onClick={() => navigate("/receipts/upload")}>Upload your first receipt</button>
            </div>
          }
        >
          <Show
            when={list().length > 0}
            fallback={
              <div class="empty-state">
                <h3>{hasFilters() ? "No items match your filters" : "No items match your search"}</h3>
                <p>
                  {hasFilters()
                    ? "Try adjusting or clearing your filters."
                    : "Try a different term or clear the search."}
                </p>
                <button
                  onClick={() => {
                    setSearch("")
                    setCategoryFilter("")
                  }}
                >
                  {hasFilters() ? "Clear filters" : "Clear search"}
                </button>
              </div>
            }
          >
            <div class="items-table-wrapper">
              <table class="responsive-table">
                <tr>
                  <th>Name</th>
                  <th>Category</th>
                  <th>Aliases</th>
                  <th>Last Bought</th>
                  <th>Purchases</th>
                  <th>Actions</th>
                </tr>
                <For each={list()}>
                  {(item) =>
                    editingId() === item.itemId ? (
                      <tr class="editing-row">
                        <td colspan={6} data-label="Edit">
                          <EditForm
                            item={item}
                            categories={cats()}
                            name={editName}
                            setName={setEditName}
                            category={editCategory}
                            setCategory={setEditCategory}
                            aliases={editAliases}
                            setAliases={setEditAliases}
                            editError={editError}
                            saving={saving}
                            onSave={saveEdit}
                            onCancel={cancelEdit}
                          />
                        </td>
                      </tr>
                    ) : (
                      <tr>
                        <td data-label="Name">
                          <A href={`/items/${item.itemId}`} class="item-name-link">
                            {item.name}
                          </A>
                        </td>
                        <td data-label="Category">
                          <span class="category-badge">
                            {cats()[item.categoryId]?.name || "Uncategorized"}
                          </span>
                        </td>
                        <td data-label="Aliases">
                          <Aliases aliases={item.aliases} />
                        </td>
                        <td data-label="Last Bought" class="muted">
                          {item.lastPurchasedAt > 0 ? formatRelativeDate(item.lastPurchasedAt) : "Never"}
                        </td>
                        <td data-label="Purchases" class="money">
                          {String(item.purchaseCount)}
                        </td>
                        <td data-label="Actions" class="row-actions">
                          <button
                            class="btn-sm btn-secondary"
                            onClick={() => startEdit(item)}
                            disabled={deletingId() === item.itemId}
                          >
                            Edit
                          </button>
                          <button
                            class="btn-sm btn-danger"
                            onClick={() => handleDelete(item)}
                            disabled={deletingId() === item.itemId}
                          >
                            {deletingId() === item.itemId ? "Deleting…" : "Delete"}
                          </button>
                        </td>
                      </tr>
                    )
                  }
                </For>
              </table>
            </div>
          </Show>
        </Show>
      </>
    )
  }

  return (
    <div class="items-page">
      <div class="page-header">
        <h1>Items</h1>
        <button onClick={() => navigate("/items/merge")} class="btn-secondary">
          Merge Duplicates
        </button>
      </div>
      <QueryBoundary pending={<SkeletonRows />}>
        <Body />
      </QueryBoundary>
    </div>
  )
}
