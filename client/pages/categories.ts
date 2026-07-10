import van from "vanjs-core"
import { api } from "../main"
import { indexBy } from "../utils"

const { div, h1, h3, p, input, select, option, form, span, button, ul, li, label } = van.tags

// Migrated to `string` IDs (uint64 precision safety). The previous
// version used `number`, which is fine for navigation but loses
// precision at ID > 2^53. Matches the rest of the client.
interface Category {
  categoryId: string
  name: string
  parentId: string | null
  sortOrder: number
}

const CategoryTree = (
  categories: Category[],
  editingId: { val: string },
  onEdit: (id: string) => void,
  onDelete: (cat: Category) => void,
) => {
  const buildTree = (parentId: string | null): Category[] => {
    return categories
      .filter(c => (c.parentId ?? null) === parentId)
      .sort((a, b) => a.sortOrder - b.sortOrder || a.name.localeCompare(b.name))
  }

  const renderNode = (category: Category): HTMLElement => {
    const children = buildTree(category.categoryId)
    const wrapper = document.createElement("li")
    const row = document.createElement("div")
    row.className = "category-node"

    const nameSpan = document.createElement("span")
    nameSpan.textContent = category.name
    row.appendChild(nameSpan)

    const actions = document.createElement("div")
    actions.className = "category-actions"
    const editBtn = document.createElement("button")
    editBtn.textContent = "Edit"
    editBtn.onclick = () => onEdit(category.categoryId)
    actions.appendChild(editBtn)
    const delBtn = document.createElement("button")
    delBtn.className = "btn-danger"
    delBtn.textContent = "Delete"
    delBtn.onclick = () => onDelete(category)
    actions.appendChild(delBtn)
    row.appendChild(actions)
    wrapper.appendChild(row)

    if (children.length > 0) {
      const sub = document.createElement("ul")
      for (const child of children) sub.appendChild(renderNode(child))
      wrapper.appendChild(sub)
    }
    return wrapper
  }

  const rootCategories = buildTree(null)

  const container = document.createElement("ul")
  container.className = "category-tree"
  for (const root of rootCategories) container.appendChild(renderNode(root))
  return container
}

const CategoriesPage = () => {
  const categories = van.state<Category[]>([])
  const loading = van.state(true)
  const error = van.state<string | null>(null)

  // Create form state
  const newName = van.state("")
  const newParent = van.state("")
  const creating = van.state(false)
  const createError = van.state<string | null>(null)

  // Edit form state — only one category in edit mode at a time
  const editingId = van.state<string>("")
  const editName = van.state("")
  const editParent = van.state("")
  const savingEdit = van.state(false)
  const editError = van.state<string | null>(null)

  // Per-category delete-in-flight state, so the button can show
  // "Deleting…" and be disabled while the request is pending.
  const deletingId = van.state<string>("")
  // General error from a failed delete (e.g. "category in use").
  const deleteError = van.state<string | null>(null)

  const loadCategories = async () => {
    loading.val = true
    error.val = null
    try {
      const data = await api.get("/categories")
      categories.val = Array.isArray(data) ? data : []
    } catch (err) {
      error.val = (err as Error).message || "Failed to load categories"
    } finally {
      loading.val = false
    }
  }

  loadCategories()

  const handleCreate = async (e: Event) => {
    e.preventDefault()
    if (!newName.val.trim()) return

    creating.val = true
    createError.val = null
    try {
      const body: { name: string; parentId?: string } = {
        name: newName.val.trim(),
      }
      if (newParent.val) body.parentId = newParent.val
      await api.post("/categories", body)
      newName.val = ""
      newParent.val = ""
      await loadCategories()
    } catch (err) {
      createError.val = (err as Error).message || "Failed to create category"
    } finally {
      creating.val = false
    }
  }

  const startEdit = (id: string) => {
    const cat = categories.val.find(c => c.categoryId === id)
    if (!cat) return
    editingId.val = id
    editName.val = cat.name
    editParent.val = cat.parentId ?? ""
    editError.val = null
  }

  const cancelEdit = () => {
    editingId.val = ""
    editError.val = null
  }

  const handleUpdate = async (e: Event) => {
    e.preventDefault()
    const id = editingId.val
    if (!id || !editName.val.trim()) return

    savingEdit.val = true
    editError.val = null
    try {
      const body: { name: string; parentId: string | null } = {
        name: editName.val.trim(),
        // Send the new parent as-is; backend only updates if non-nil.
        // Leaving the value unchanged by selecting the same parent is
        // a no-op at the server. To unlink, we don't support that yet
        // (no clear UI affordance for "make this a root category").
        parentId: editParent.val || null,
      }
      await api.patch(`/categories/${id}`, body)
      cancelEdit()
      await loadCategories()
    } catch (err) {
      editError.val = (err as Error).message || "Failed to save"
    } finally {
      savingEdit.val = false
    }
  }

  const handleDelete = async (cat: Category) => {
    if (!confirm(`Delete "${cat.name}"?\n\nThis will fail if any items are using this category.`)) {
      return
    }
    deletingId.val = cat.categoryId
    deleteError.val = null
    try {
      await api.delete(`/categories/${cat.categoryId}`)
      await loadCategories()
    } catch (err) {
      deleteError.val = (err as Error).message || "Delete failed"
    } finally {
      deletingId.val = ""
    }
  }

  // Dropdown options for parent selection: every category except the
  // one currently being edited (would create a 1-level cycle). We do
  // not check for deep cycles — the backend doesn't either, but the
  // risk at family scale is negligible.
  const parentOptions = (excludeId: string): { value: string; name: string }[] => {
    return categories.val
      .filter(c => c.categoryId !== excludeId)
      .sort((a, b) => a.name.localeCompare(b.name))
      .map(c => ({ value: c.categoryId, name: c.name }))
  }

  return div({ class: "categories-page" },
    div({ class: "page-header" },
      h1("Categories"),
    ),

    // Create form — name + optional parent.
    form({ onsubmit: handleCreate, class: "create-form" },
      input({
        type: "text",
        placeholder: "New category name",
        value: newName,
        oninput: (e: Event) => newName.val = (e.target as HTMLInputElement).value,
        disabled: creating,
      }),
      // Parent picker — empty value means "root level". Reads from
      // the categories state; function-child ensures the options
      // re-render when categories change. Note: we cannot use
      // `value: newParent` as a VanJS prop binding, because VanJS
      // applies props to the <select> BEFORE appending the <option>
      // children, and the browser silently discards a select.value
      // that doesn't match any existing option. We set the value
      // manually after the options are appended.
      () => {
        const sel = select({
          onchange: (e: Event) => { newParent.val = (e.target as HTMLSelectElement).value },
          disabled: creating,
          "aria-label": "Parent category",
          class: "parent-picker",
        },
          option({ value: "" }, "(no parent)"),
          ...parentOptions("").map(o => option({ value: o.value }, o.name)),
        )
        sel.value = newParent.val
        return sel
      },
      button({ type: "submit", disabled: creating || !newName.val.trim() },
        () => creating.val ? "Adding…" : "Add"),
    ),
    () => createError.val
      ? p({ class: "error" }, createError.val)
      : "",

    // Edit form — only shown when a row is being edited.
    () => editingId.val
      ? form({ onsubmit: handleUpdate, class: "edit-form" },
          input({
            type: "text",
            value: editName,
            oninput: (e: Event) => editName.val = (e.target as HTMLInputElement).value,
            disabled: savingEdit,
          }),
          // See comment on the create-form select: we set the
          // value manually after the options are appended, because
          // VanJS applies props before children and the browser
          // discards a select.value with no matching option. This
          // is the cause of the "edit shows (no parent)" bug.
          () => {
            const sel = select({
              onchange: (e: Event) => { editParent.val = (e.target as HTMLSelectElement).value },
              disabled: savingEdit,
              "aria-label": "Parent category",
              class: "parent-picker",
            },
              option({ value: "" }, "(no parent)"),
              ...parentOptions(editingId.val).map(o => option({ value: o.value }, o.name)),
            )
            sel.value = editParent.val
            return sel
          },
          button({ type: "submit", disabled: savingEdit || !editName.val.trim() },
            () => savingEdit.val ? "Saving…" : "Save"),
          button({ type: "button", onclick: cancelEdit, disabled: savingEdit }, "Cancel"),
        )
      : "",
    () => editingId.val && editError.val
      ? p({ class: "error" }, editError.val)
      : "",

    // Error from the most recent failed delete (e.g. "in use").
    () => deleteError.val
      ? p({ class: "error" }, deleteError.val)
      : "",

    // Body — tree view.
    () => {
      if (error.val) {
        return div({ class: "empty-state" },
          h3("Couldn't load categories"),
          p(error.val),
          button({ onclick: loadCategories }, "Try Again"),
        )
      }
      if (loading.val) return div({ class: "muted" }, "Loading…")
      if (categories.val.length === 0) {
        return div({ class: "empty-state" },
          h3("No categories yet"),
          p("Create one above to get started."),
        )
      }
      const tree = CategoryTree(categories.val, editingId, startEdit, handleDelete)
      return tree
    },
  )
}

export default CategoriesPage
