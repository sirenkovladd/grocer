import van from "vanjs-core"
import { api, idStr, navigate } from "../main"
import { indexBy, formatRelativeDate } from "../utils"

const { div, h1, h2, h3, p, input, select, option, label, table, tr, td, th, button, span, a } = van.tags

// ID fields are `string` (uint64 precision safety). See ticket 04
// decisions log. Migrating other pages that still use `number` is a
// follow-up.
interface Item {
  itemId: string
  name: string
  categoryId: string
  merchantId: string
  normalized: string
  aliases: string[]
  // purchaseCount and lastPurchasedAt come from /api/items/insights.
  // We default to 0 / 0 here so the type also works for callers
  // that hit the bare /api/items endpoint (the merge tool, for
  // example, only needs the bare list).
  purchaseCount: number
  lastPurchasedAt: number
}

interface Category {
  categoryId: string
  name: string
}

const SkeletonRow = () =>
  div({ class: "skeleton-row" },
    div({ class: "skeleton-cell skeleton-cell-lg" }),
    div({ class: "skeleton-cell skeleton-cell-md" }),
    div({ class: "skeleton-cell skeleton-cell-md" }),
  )

const formatAliases = (aliases: string[]) => {
  if (aliases.length === 0) {
    return span({ class: "muted" }, "—")
  }
  const MAX = 3
  const shown = aliases.slice(0, MAX).join(", ")
  const extra = aliases.length - MAX
  const text = extra > 0 ? `${shown} +${extra} more` : shown
  return span({ title: aliases.join(", ") }, text)
}

// Inline edit form rendered as a single row that spans all columns.
// The form submits on Save (which calls PATCH /api/items/{id}) and
// bails out on Cancel. Validates client-side: name must be non-empty.
const EditForm = (
  item: Item,
  categories: Record<string, Category>,
  editName: { val: string },
  editCategory: { val: string },
  editAliases: { val: string },
  editError: { val: string },
  saving: { val: boolean },
  onSave: () => void,
  onCancel: () => void,
) => {
  return div({ class: "inline-edit-form" },
    div({ class: "edit-field" },
      label({ for: `edit-name-${item.itemId}` }, "Name"),
      input({
        id: `edit-name-${item.itemId}`,
        type: "text",
        value: editName,
        oninput: (e: Event) => { editName.val = (e.target as HTMLInputElement).value },
        disabled: saving,
        class: "edit-input",
      }),
    ),
    div({ class: "edit-field" },
      label({ for: `edit-cat-${item.itemId}` }, "Category"),
      select({
        id: `edit-cat-${item.itemId}`,
        value: editCategory,
        disabled: saving,
        onchange: (e: Event) => { editCategory.val = (e.target as HTMLSelectElement).value },
        class: "edit-input",
      },
        // `0` / empty value used as "Uncategorized" sentinel — the
        // backend's GetItem falls back to UnknownCategory for any
        // categoryID not in the map, so a missing category is safe.
        option({ value: "" }, "Uncategorized"),
        ...Object.values(categories)
          .slice()
          .sort((a, b) => a.name.localeCompare(b.name))
          .map((c: Category) =>
            option({ value: c.categoryId }, c.name),
          ),
      ),
    ),
    div({ class: "edit-field edit-field-wide" },
      label({ for: `edit-aliases-${item.itemId}` }, "Aliases (comma-separated)"),
      input({
        id: `edit-aliases-${item.itemId}`,
        type: "text",
        value: editAliases,
        oninput: (e: Event) => { editAliases.val = (e.target as HTMLInputElement).value },
        disabled: saving,
        class: "edit-input",
        placeholder: "alias1, alias2, …",
      }),
    ),
    () => editError.val
      ? div({ class: "edit-error" }, editError.val)
      : "",
    div({ class: "edit-actions" },
      button({
        type: "button",
        onclick: onSave,
        disabled: saving || !editName.val.trim(),
        class: "btn-sm btn-primary",
      }, () => saving.val ? "Saving…" : "Save"),
      button({
        type: "button",
        onclick: onCancel,
        disabled: saving,
        class: "btn-sm btn-secondary",
      }, "Cancel"),
    ),
  )
}

const ItemsPage = () => {
  const items = van.state<Item[]>([])
  const categories = van.state<Record<string, Category>>({})
  const loading = van.state(true)
  const error = van.state<string | null>(null)
  const search = van.state("")
  // Possible values:
  //   ""                -> no filter (show all)
  //   "__uncategorized__" -> items whose categoryId is not in the
  //                          categories map (matches the
  //                          "Uncategorized" fallback in the
  //                          category column)
  //   <categoryId>      -> that specific category
  // We can't reuse the empty string for "Uncategorized" because it
  // already means "no filter", and we can't use a real categoryId
  // because there's no canonical ID for the implicit "Unknown"
  // category on the client (the backend just falls back to it).
  const UNCATEGORIZED = "__uncategorized__"
  const categoryFilter = van.state<string>("")

  // Edit state — only one row is in edit mode at a time, so a single
  // set of "edit-*" states is sufficient. `editingId` is the empty
  // string when no row is being edited.
  const editingId = van.state<string>("")
  const editName = van.state<string>("")
  const editCategory = van.state<string>("")
  const editAliases = van.state<string>("")
  const editError = van.state<string>("")
  const saving = van.state<boolean>(false)

  // Per-item delete state. Tracks which item has a delete in flight
  // so we can disable its button and show "Deleting…" feedback.
  const deletingId = van.state<string>("")

  const load = async () => {
    loading.val = true
    error.val = null
    try {
      // Use the insights endpoint so we can render purchase stats
      // (count + last bought) without a per-item N+1 to the
      // analysis endpoint. Falls back to the bare list if the
      // server is older than the insights endpoint was added.
      const [i, c] = await Promise.all([
        api.get("/items/insights"),
        api.get("/categories"),
      ])
      items.val = Array.isArray(i) ? i.map(normalizeItem) : []
      categories.val = indexBy(Array.isArray(c) ? c : [], (x: Category) => x.categoryId)
    } catch (err) {
      console.error("Failed to load items:", err)
      error.val = (err as Error).message || "Failed to load items"
    } finally {
      loading.val = false
    }
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

  load()

  // Sort selector — "alpha" (default) sorts alphabetically;
  // "frequent" sorts by purchase count desc, with alphabetical
  // tie-breaking. The `sort` state is read in the reactive render
  // so changing it re-sorts without a refetch.
  const sort = van.state<"alpha" | "frequent">("alpha")

  // Sort: pure function called inside the reactive render.
  const sorted = (): Item[] => {
    const all = [...items.val]
    if (sort.val === "frequent") {
      all.sort((a, b) => {
        if (b.purchaseCount !== a.purchaseCount) {
          return b.purchaseCount - a.purchaseCount
        }
        return a.name.toLowerCase().localeCompare(b.name.toLowerCase())
      })
    } else {
      all.sort((a, b) =>
        a.name.toLowerCase().localeCompare(b.name.toLowerCase()),
      )
    }
    return all
  }

  const filtered = (): Item[] => {
    const s = search.val.trim().toLowerCase()
    const cat = categoryFilter.val
    const cats = categories.val
    const list = sorted()
    return list.filter(i => {
      if (cat === UNCATEGORIZED) {
        if (cats[i.categoryId]) return false
      } else if (cat) {
        if (i.categoryId !== cat) return false
      }
      if (!s) return true
      return i.name.toLowerCase().includes(s) ||
        i.aliases.some(a => a.toLowerCase().includes(s))
    })
  }

  // Populate the edit-* states from the row and enter edit mode.
  const startEdit = (item: Item) => {
    editingId.val = item.itemId
    editName.val = item.name
    editCategory.val = item.categoryId
    // Render aliases as a comma-separated string for editing. The
    // server stores them as []string; round-tripping through CSV
    // is fine for the typical small alias list.
    editAliases.val = item.aliases.join(", ")
    editError.val = ""
  }

  const cancelEdit = () => {
    editingId.val = ""
    editError.val = ""
  }

  const saveEdit = async () => {
    const id = editingId.val
    if (!id) return

    saving.val = true
    editError.val = ""
    try {
      // Parse aliases — split on commas, trim, drop empties.
      const aliases = editAliases.val
        .split(",")
        .map(a => a.trim())
        .filter(a => a.length > 0)
      await api.patch(`/items/${id}`, {
        name: editName.val.trim(),
        categoryId: idStr(editCategory.val),
        aliases,
      })
      await load()
      cancelEdit()
    } catch (err) {
      editError.val = (err as Error).message || "Failed to save"
    } finally {
      saving.val = false
    }
  }

  const handleDelete = async (item: Item) => {
    if (!confirm(`Delete "${item.name}"?\n\nThis will fail if any receipt still references this item.`)) {
      return
    }
    deletingId.val = item.itemId
    try {
      await api.delete(`/items/${item.itemId}`)
      await load()
    } catch (err) {
      alert(`Delete failed: ${(err as Error).message}`)
    } finally {
      deletingId.val = ""
    }
  }

  return div({ class: "items-page" },
    div({ class: "page-header" },
      h1("Items"),
      button({
        onclick: () => navigate("/items/merge"),
        class: "btn-secondary",
      }, "Merge Duplicates"),
    ),

    div({ class: "filter-bar" },
      input({
        type: "search",
        class: "search-input",
        placeholder: "Search items or aliases…",
        value: search,
        oninput: (e: Event) => {
          search.val = (e.target as HTMLInputElement).value
        },
      }),
      select({
        value: sort,
        onchange: (e: Event) => { sort.val = (e.target as HTMLSelectElement).value as "alpha" | "frequent" },
        "aria-label": "Sort items",
        class: "sort-picker",
      },
        option({ value: "alpha" }, "Alphabetical"),
        option({ value: "frequent" }, "Most frequently bought"),
      ),
      // Wrap the entire <select> in a function-child so reading
      // `categories.val` is scoped to this function-child, not the
      // surrounding App function-child. Without it, VanJS would
      // push the App binding into categories._bindings, and a later
      // `loadData()` set would re-evaluate App, call ItemsPage()
      // again, and loop forever. Same pattern as the owner/merchant
      // filters in receipts.ts.
      () => select({
        value: categoryFilter,
        onchange: (e: Event) => {
          categoryFilter.val = (e.target as HTMLSelectElement).value
        },
        "aria-label": "Filter by category",
        class: "category-filter",
      },
        option({ value: "" }, "All categories"),
        // Matches the position used by the EditForm's category
        // picker so users see the same "Uncategorized" entry in
        // both places. Placed right after "All categories" so the
        // implicit/no-category items are easy to find without
        // scrolling through the sorted real categories.
        option({ value: UNCATEGORIZED }, "Uncategorized"),
        ...Object.values(categories.val)
          .slice()
          .sort((a, b) => a.name.localeCompare(b.name))
          .map((c: Category) =>
            option({ value: c.categoryId }, c.name),
          ),
      ),
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
        return div({ class: "items-skeleton" },
          SkeletonRow(), SkeletonRow(), SkeletonRow(),
          SkeletonRow(), SkeletonRow(),
        )
      }
      if (items.val.length === 0) {
        return div({ class: "empty-state" },
          h3("No items yet"),
          p("Upload a receipt to get started."),
          button({ onclick: () => navigate("/receipts/upload") }, "Upload your first receipt"),
        )
      }
      const list = filtered()
      if (list.length === 0) {
        const hasFilters = !!(search.val || categoryFilter.val)
        return div({ class: "empty-state" },
          h3(hasFilters ? "No items match your filters" : "No items match your search"),
          p(hasFilters
            ? "Try adjusting or clearing your filters."
            : "Try a different term or clear the search."),
          button({
            onclick: () => {
              search.val = ""
              categoryFilter.val = ""
            },
          }, hasFilters ? "Clear filters" : "Clear search"),
        )
      }
      return div({ class: "items-table-wrapper" },
        table({ class: "responsive-table" },
          tr(
            th("Name"),
            th("Category"),
            th("Aliases"),
            th("Last Bought"),
            th("Purchases"),
            th("Actions"),
          ),
          ...list.map(item => {
            // Edit mode: render a single spanning row with the form
            // instead of the normal columns.
            if (editingId.val === item.itemId) {
              return tr({ class: "editing-row" },
                td({ colspan: 6, "data-label": "Edit" },
                  EditForm(
                    item, categories.val, editName, editCategory,
                    editAliases, editError, saving, saveEdit, cancelEdit,
                  ),
                ),
              )
            }
            const catName = categories.val[item.categoryId]?.name || "Uncategorized"
            return tr(
              td({ "data-label": "Name" },
                a({
                  href: `#/items/${item.itemId}`,
                  class: "item-name-link",
                  onclick: (e: Event) => {
                    e.preventDefault()
                    navigate(`/items/${item.itemId}`)
                  },
                }, item.name),
              ),
              td({ "data-label": "Category" },
                span({ class: "category-badge" }, catName),
              ),
              td({ "data-label": "Aliases" }, formatAliases(item.aliases)),
              td({ "data-label": "Last Bought", class: "muted" },
                () => item.lastPurchasedAt > 0
                  ? formatRelativeDate(item.lastPurchasedAt)
                  : "Never",
              ),
              td({ "data-label": "Purchases", class: "money" },
                () => String(item.purchaseCount),
              ),
              td({ "data-label": "Actions", class: "row-actions" },
                button({
                  class: "btn-sm btn-secondary",
                  onclick: () => startEdit(item),
                  disabled: () => deletingId.val === item.itemId,
                }, "Edit"),
                button({
                  class: "btn-sm btn-danger",
                  onclick: () => handleDelete(item),
                  disabled: () => deletingId.val === item.itemId,
                }, () => deletingId.val === item.itemId ? "Deleting…" : "Delete"),
              ),
            )
          }),
        ),
      )
    },
  )
}

export default ItemsPage
