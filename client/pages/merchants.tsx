import { createSignal, For, Show } from "solid-js"
import QueryBoundary from "../components/QueryBoundary.tsx"
import { createQuery } from "../lib/query.ts"
import { api, idStr } from "../lib/api.ts"

// ID fields are `string` (uint64 precision safety).
interface Merchant {
  merchantId: string
  name: string
}

function SkeletonRows() {
  return (
    <div class="merchants-skeleton">
      <For each={[0, 1, 2]}>
        {() => (
          <div class="skeleton-row">
            <div class="skeleton-cell skeleton-cell-lg" />
            <div class="skeleton-cell skeleton-cell-md" />
          </div>
        )}
      </For>
    </div>
  )
}

// Merchants management page: list, rename, delete, merge.
export default function MerchantsPage() {
  // Create form (static — outside the data boundary).
  const [newName, setNewName] = createSignal("")
  const [creating, setCreating] = createSignal(false)
  const [createError, setCreateError] = createSignal<string | null>(null)

  // Per-merchant edit state — only one row in edit mode at a time.
  const [editingId, setEditingId] = createSignal("")
  const [editName, setEditName] = createSignal("")
  const [savingEdit, setSavingEdit] = createSignal(false)
  const [editError, setEditError] = createSignal<string | null>(null)

  const [deletingId, setDeletingId] = createSignal("")
  // Per-merchant merge-in-flight key: "sourceId->targetId".
  const [mergingKey, setMergingKey] = createSignal("")
  // Target picker for the merge dropdown — per-row state.
  const [mergeTarget, setMergeTarget] = createSignal<Record<string, string>>({})
  // General error from a failed delete is shown via alert (matches
  // the original); edit errors render inline.

  const { data, refetch } = createQuery(async () => {
    const m = await api.get("/merchants")
    return (Array.isArray(m) ? m : []) as Merchant[]
  })

  const handleCreate = async (e: Event) => {
    e.preventDefault()
    if (!newName().trim()) return
    setCreating(true)
    setCreateError(null)
    try {
      await api.post("/merchants", { name: newName().trim() })
      setNewName("")
      refetch()
    } catch (err) {
      setCreateError((err as Error).message || "Failed to create merchant")
    } finally {
      setCreating(false)
    }
  }

  const startEdit = (m: Merchant) => {
    setEditingId(m.merchantId)
    setEditName(m.name)
    setEditError(null)
  }

  const cancelEdit = () => {
    setEditingId("")
    setEditError(null)
  }

  const saveEdit = async () => {
    const id = editingId()
    if (!id || !editName().trim()) return
    setSavingEdit(true)
    setEditError(null)
    try {
      await api.patch(`/merchants/${id}`, { name: editName().trim() })
      cancelEdit()
      refetch()
    } catch (err) {
      setEditError((err as Error).message || "Failed to save")
    } finally {
      setSavingEdit(false)
    }
  }

  const handleDelete = async (m: Merchant) => {
    if (!confirm(`Delete merchant "${m.name}"?\n\nThis will fail if any receipt still references it.`)) {
      return
    }
    setDeletingId(m.merchantId)
    try {
      await api.delete(`/merchants/${m.merchantId}`)
      refetch()
    } catch (err) {
      alert(`Delete failed: ${(err as Error).message}`)
    } finally {
      setDeletingId("")
    }
  }

  const handleMerge = async (source: Merchant, targetId: string) => {
    if (!targetId) {
      alert("Pick a target merchant to merge into.")
      return
    }
    const target = data()!.find((m) => m.merchantId === targetId)
    if (!target) return
    if (
      !confirm(
        `Merge "${source.name}" into "${target.name}"?\n\n` +
          `Every receipt currently tagged with "${source.name}" will be retagged to "${target.name}", ` +
          `and the "${source.name}" merchant will be deleted. This cannot be undone.`,
      )
    ) {
      return
    }
    const key = `${source.merchantId}->${target.merchantId}`
    setMergingKey(key)
    try {
      const result = await api.post(`/merchants/${source.merchantId}/merge`, {
        targetId: idStr(target.merchantId),
      })
      // Clear the per-row target picker.
      setMergeTarget((prev) => {
        const next = { ...prev }
        delete next[source.merchantId]
        return next
      })
      refetch()
      const n = (result as any).retargeted ?? 0
      alert(`Merged. ${n} receipt${n === 1 ? "" : "s"} retargeted.`)
    } catch (err) {
      alert(`Merge failed: ${(err as Error).message}`)
    } finally {
      setMergingKey("")
    }
  }

  // Body reads the resource (suspending until ready). Closure
  // component so data reads stay inside the boundary.
  const Body = () => {
    const list = () => [...data()!].sort((a, b) => a.name.localeCompare(b.name))
    return (
      <Show
        when={data()!.length > 0}
        fallback={
          <div class="empty-state">
            <h3>No merchants yet</h3>
            <p>Add one above to get started.</p>
          </div>
        }
      >
        <div class="items-table-wrapper">
          <table class="responsive-table">
            <tr>
              <th>Name</th>
              <th>Actions</th>
            </tr>
            <For each={list()}>
              {(m) =>
                editingId() === m.merchantId ? (
                  // Edit mode: span the row with the edit form.
                  <tr class="editing-row">
                    <td colspan={2} data-label="Edit">
                      <div class="inline-edit-form">
                        <div class="edit-field">
                          <span class="edit-label">Name</span>
                          <input
                            type="text"
                            value={editName()}
                            onInput={(e) => setEditName(e.currentTarget.value)}
                            disabled={savingEdit()}
                            class="edit-input"
                          />
                        </div>
                        <Show when={editError()}>
                          <div class="edit-error">{editError()}</div>
                        </Show>
                        <div class="edit-actions">
                          <button
                            type="button"
                            onClick={saveEdit}
                            disabled={savingEdit() || !editName().trim()}
                            class="btn-sm btn-primary"
                          >
                            {savingEdit() ? "Saving…" : "Save"}
                          </button>
                          <button
                            type="button"
                            onClick={cancelEdit}
                            disabled={savingEdit()}
                            class="btn-sm btn-secondary"
                          >
                            Cancel
                          </button>
                        </div>
                      </div>
                    </td>
                  </tr>
                ) : (
                  <tr>
                    <td data-label="Name">{m.name}</td>
                    <td data-label="Actions">
                      <div class="merchant-actions">
                        <button
                          class="btn-sm btn-secondary"
                          onClick={() => startEdit(m)}
                          disabled={deletingId() === m.merchantId || mergingKey() !== ""}
                        >
                          Rename
                        </button>
                        <button
                          class="btn-sm btn-danger"
                          onClick={() => handleDelete(m)}
                          disabled={deletingId() === m.merchantId || mergingKey() !== ""}
                        >
                          {deletingId() === m.merchantId ? "Deleting…" : "Delete"}
                        </button>
                        <div class="merchant-merge">
                          {/* Per-row target picker. Per-option
                              `selected` (not select `value=`): the
                              browser records select.value before
                              options are appended and drops it; the
                              selected attribute is respected
                              regardless of append order. */}
                          <select
                            onChange={(e) =>
                              setMergeTarget((prev) => ({
                                ...prev,
                                [m.merchantId]: e.currentTarget.value,
                              }))
                            }
                            disabled={deletingId() === m.merchantId || mergingKey() !== ""}
                            class="merge-target-picker"
                            aria-label={`Merge target for ${m.name}`}
                          >
                            <option value="" selected={(mergeTarget()[m.merchantId] ?? "") === ""}>
                              Merge into…
                            </option>
                            <For each={data()!.filter((x) => x.merchantId !== m.merchantId)}>
                              {(other) => (
                                <option
                                  value={other.merchantId}
                                  selected={mergeTarget()[m.merchantId] === other.merchantId}
                                >
                                  {other.name}
                                </option>
                              )}
                            </For>
                          </select>
                          <button
                            class="btn-sm btn-secondary"
                            onClick={() => handleMerge(m, mergeTarget()[m.merchantId] ?? "")}
                            disabled={
                              !mergeTarget()[m.merchantId] ||
                              deletingId() === m.merchantId ||
                              mergingKey() !== ""
                            }
                          >
                            {mergingKey() === `${m.merchantId}->${mergeTarget()[m.merchantId] ?? ""}`
                              ? "Merging…"
                              : "Merge"}
                          </button>
                        </div>
                      </div>
                    </td>
                  </tr>
                )
              }
            </For>
          </table>
        </div>
      </Show>
    )
  }

  return (
    <div class="merchants-page">
      <div class="page-header">
        <h1>Merchants</h1>
      </div>

      <form onSubmit={handleCreate} class="create-form">
        <input
          type="text"
          placeholder="New merchant name"
          value={newName()}
          onInput={(e) => setNewName(e.currentTarget.value)}
          disabled={creating()}
        />
        <button type="submit" disabled={creating() || !newName().trim()}>
          {creating() ? "Adding…" : "Add"}
        </button>
      </form>
      <Show when={createError()}>
        <p class="error">{createError()}</p>
      </Show>

      <QueryBoundary pending={<SkeletonRows />}>
        <Body />
      </QueryBoundary>
    </div>
  )
}
