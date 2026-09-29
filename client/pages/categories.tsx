import { createSignal, For, Show } from "solid-js"
import QueryBoundary from "../components/QueryBoundary.tsx"
import { createQuery } from "../lib/query.ts"
import { api, idStr } from "../lib/api.ts"

// String IDs (uint64 precision safety).
interface Category {
  categoryId: string
  name: string
  parentId: string | null
  sortOrder: number
}

// Recursive tree node. Pure function of values (no per-node
// reactivity needed — the tree re-renders wholesale on refetch).
// The VanJS version built this with document.createElement; JSX is
// equivalent and keeps class names identical.
function CategoryNode(props: {
  category: Category
  children: Category[]
  all: Category[]
  onEdit: (id: string) => void
  onDelete: (cat: Category) => void
}) {
  return (
    <li>
      <div class="category-node">
        <span>{props.category.name}</span>
        <div class="category-actions">
          <button onClick={() => props.onEdit(props.category.categoryId)}>Edit</button>
          <button class="btn-danger" onClick={() => props.onDelete(props.category)}>
            Delete
          </button>
        </div>
      </div>
      <Show when={props.children.length > 0}>
        <ul>
          <For each={props.children}>
            {(child) => (
              <CategoryNode
                category={child}
                children={props.all
                  .filter((c) => (c.parentId ?? null) === child.categoryId)
                  .sort((a, b) => a.sortOrder - b.sortOrder || a.name.localeCompare(b.name))}
                all={props.all}
                onEdit={props.onEdit}
                onDelete={props.onDelete}
              />
            )}
          </For>
        </ul>
      </Show>
    </li>
  )
}

export default function CategoriesPage() {
  // Create form state (the name input is static; the parent picker
  // needs data, so the whole form suspends with the body).
  const [newName, setNewName] = createSignal("")
  const [newParent, setNewParent] = createSignal("")
  const [creating, setCreating] = createSignal(false)
  const [createError, setCreateError] = createSignal<string | null>(null)

  // Edit form state — only one category in edit mode at a time.
  const [editingId, setEditingId] = createSignal("")
  const [editName, setEditName] = createSignal("")
  const [editParent, setEditParent] = createSignal("")
  const [savingEdit, setSavingEdit] = createSignal(false)
  const [editError, setEditError] = createSignal<string | null>(null)

  // General error from a failed delete (e.g. "category in use").
  const [deleteError, setDeleteError] = createSignal<string | null>(null)

  const { data, refetch } = createQuery(async () => {
    const c = await api.get("/categories")
    return (Array.isArray(c) ? c : []) as Category[]
  })

  const handleCreate = async (e: Event) => {
    e.preventDefault()
    if (!newName().trim()) return
    setCreating(true)
    setCreateError(null)
    try {
      const body: { name: string; parentId?: string } = { name: newName().trim() }
      const newParentId = idStr(newParent())
      if (newParentId) body.parentId = newParentId
      await api.post("/categories", body)
      setNewName("")
      setNewParent("")
      refetch()
    } catch (err) {
      setCreateError((err as Error).message || "Failed to create category")
    } finally {
      setCreating(false)
    }
  }

  const startEdit = (id: string) => {
    const cat = data()!.find((c) => c.categoryId === id)
    if (!cat) return
    setEditingId(id)
    setEditName(cat.name)
    setEditParent(cat.parentId ?? "")
    setEditError(null)
  }

  const cancelEdit = () => {
    setEditingId("")
    setEditError(null)
  }

  const handleUpdate = async (e: Event) => {
    e.preventDefault()
    const id = editingId()
    if (!id || !editName().trim()) return
    setSavingEdit(true)
    setEditError(null)
    try {
      const body: { name: string; parentId: string | null } = {
        name: editName().trim(),
        // Send the new parent as-is; backend only updates if non-nil.
        // To unlink (make root) is unsupported — no clear UI
        // affordance for it yet.
        parentId: idStr(editParent()),
      }
      await api.patch(`/categories/${id}`, body)
      cancelEdit()
      refetch()
    } catch (err) {
      setEditError((err as Error).message || "Failed to save")
    } finally {
      setSavingEdit(false)
    }
  }

  const handleDelete = async (cat: Category) => {
    if (!confirm(`Delete "${cat.name}"?\n\nThis will fail if any items are using this category.`)) {
      return
    }
    setDeleteError(null)
    try {
      await api.delete(`/categories/${cat.categoryId}`)
      refetch()
    } catch (err) {
      setDeleteError((err as Error).message || "Delete failed")
    }
  }

  // Parent-picker options: every category except the one being
  // edited (would create a 1-level cycle). Deep cycles unchecked —
  // the backend doesn't either; negligible at family scale.
  const parentOptions = (excludeId: string): Category[] =>
    data()!
      .filter((c) => c.categoryId !== excludeId)
      .sort((a, b) => a.name.localeCompare(b.name))

  const roots = (): Category[] =>
    data()!
      .filter((c) => (c.parentId ?? null) === null)
      .sort((a, b) => a.sortOrder - b.sortOrder || a.name.localeCompare(b.name))

  const childrenOf = (id: string): Category[] =>
    data()!
      .filter((c) => (c.parentId ?? null) === id)
      .sort((a, b) => a.sortOrder - b.sortOrder || a.name.localeCompare(b.name))

  // Body reads the resource (suspending until ready). Closure
  // component so data reads stay inside the boundary.
  const Body = () => (
    <>
      {/* Create form — name + optional parent (empty = root level).
          Per-option `selected`, not select `value=`: the browser
          drops a select.value set before options exist. */}
      <form onSubmit={handleCreate} class="create-form">
        <input
          type="text"
          placeholder="New category name"
          value={newName()}
          onInput={(e) => setNewName(e.currentTarget.value)}
          disabled={creating()}
        />
        <select
          onChange={(e) => setNewParent(e.currentTarget.value)}
          disabled={creating()}
          aria-label="Parent category"
          class="parent-picker"
        >
          <option value="" selected={newParent() === ""}>
            (no parent)
          </option>
          <For each={parentOptions("")}>
            {(o) => (
              <option value={o.categoryId} selected={newParent() === o.categoryId}>
                {o.name}
              </option>
            )}
          </For>
        </select>
        <button type="submit" disabled={creating() || !newName().trim()}>
          {creating() ? "Adding…" : "Add"}
        </button>
      </form>
      <Show when={createError()}>
        <p class="error">{createError()}</p>
      </Show>

      {/* Edit form — only shown when a row is being edited. */}
      <Show when={editingId()}>
        <form onSubmit={handleUpdate} class="edit-form">
          <input
            type="text"
            value={editName()}
            onInput={(e) => setEditName(e.currentTarget.value)}
            disabled={savingEdit()}
          />
          <select
            onChange={(e) => setEditParent(e.currentTarget.value)}
            disabled={savingEdit()}
            aria-label="Parent category"
            class="parent-picker"
          >
            <option value="" selected={editParent() === ""}>
              (no parent)
            </option>
            <For each={parentOptions(editingId())}>
              {(o) => (
                <option value={o.categoryId} selected={editParent() === o.categoryId}>
                  {o.name}
                </option>
              )}
            </For>
          </select>
          <button type="submit" disabled={savingEdit() || !editName().trim()}>
            {savingEdit() ? "Saving…" : "Save"}
          </button>
          <button type="button" onClick={cancelEdit} disabled={savingEdit()}>
            Cancel
          </button>
        </form>
      </Show>
      <Show when={editingId() && editError()}>
        <p class="error">{editError()}</p>
      </Show>

      {/* Error from the most recent failed delete (e.g. "in use"). */}
      <Show when={deleteError()}>
        <p class="error">{deleteError()}</p>
      </Show>

      <Show
        when={data()!.length > 0}
        fallback={
          <div class="empty-state">
            <h3>No categories yet</h3>
            <p>Create one above to get started.</p>
          </div>
        }
      >
        <ul class="category-tree">
          <For each={roots()}>
            {(root) => (
              <CategoryNode
                category={root}
                children={childrenOf(root.categoryId)}
                all={data()!}
                onEdit={startEdit}
                onDelete={handleDelete}
              />
            )}
          </For>
        </ul>
      </Show>
    </>
  )

  return (
    <div class="categories-page">
      <div class="page-header">
        <h1>Categories</h1>
      </div>
      <QueryBoundary pending={<div class="muted">Loading…</div>}>
        <Body />
      </QueryBoundary>
    </div>
  )
}
