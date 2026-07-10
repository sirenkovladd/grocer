import van from "vanjs-core"
import { api } from "../main"

const { div, h1, h3, p, input, button, form, span, table, tr, td, th, select, option } = van.tags

// ID fields are `string` (uint64 precision safety). See ticket 04.
interface Merchant {
  merchantId: string
  name: string
}

const SkeletonRow = () =>
  div({ class: "skeleton-row" },
    div({ class: "skeleton-cell skeleton-cell-lg" }),
    div({ class: "skeleton-cell skeleton-cell-md" }),
  )

// The Merchants management page: list, rename, delete, merge. The
// earlier 'pages/merchants.ts' (deleted in #3) was mislabeled and
// just called /api/analysis/items/{id}. This is a real management
// page for the merchant catalog.

const MerchantsPage = () => {
  const merchants = van.state<Merchant[]>([])
  const loading = van.state(true)
  const error = van.state<string | null>(null)

  // Create form
  const newName = van.state("")
  const creating = van.state(false)
  const createError = van.state<string | null>(null)

  // Per-merchant edit state. We use a record keyed by id; the
  // edit form is rendered inline in the row when `editingId`
  // matches. Single editingId so only one row is in edit mode
  // at a time.
  const editingId = van.state<string>("")
  const editName = van.state("")
  const savingEdit = van.state(false)
  const editError = van.state<string | null>(null)

  // Per-merchant delete-in-flight state.
  const deletingId = van.state<string>("")
  // Per-merchant merge-in-flight state, keyed as "sourceId->targetId".
  const mergingKey = van.state<string>("")
  // Target picker for the merge dropdown — per-row state.
  const mergeTarget = van.state<Record<string, string>>({})

  const load = async () => {
    loading.val = true
    error.val = null
    try {
      const data = await api.get("/merchants")
      merchants.val = Array.isArray(data) ? data : []
    } catch (err) {
      error.val = (err as Error).message || "Failed to load merchants"
    } finally {
      loading.val = false
    }
  }

  load()

  const handleCreate = async (e: Event) => {
    e.preventDefault()
    if (!newName.val.trim()) return
    creating.val = true
    createError.val = null
    try {
      await api.post("/merchants", { name: newName.val.trim() })
      newName.val = ""
      await load()
    } catch (err) {
      createError.val = (err as Error).message || "Failed to create merchant"
    } finally {
      creating.val = false
    }
  }

  const startEdit = (m: Merchant) => {
    editingId.val = m.merchantId
    editName.val = m.name
    editError.val = null
  }

  const cancelEdit = () => {
    editingId.val = ""
    editError.val = null
  }

  const saveEdit = async () => {
    const id = editingId.val
    if (!id || !editName.val.trim()) return
    savingEdit.val = true
    editError.val = null
    try {
      await api.patch(`/merchants/${id}`, { name: editName.val.trim() })
      cancelEdit()
      await load()
    } catch (err) {
      editError.val = (err as Error).message || "Failed to save"
    } finally {
      savingEdit.val = false
    }
  }

  const handleDelete = async (m: Merchant) => {
    if (!confirm(`Delete merchant "${m.name}"?\n\nThis will fail if any receipt still references it.`)) {
      return
    }
    deletingId.val = m.merchantId
    try {
      await api.delete(`/merchants/${m.merchantId}`)
      await load()
    } catch (err) {
      alert(`Delete failed: ${(err as Error).message}`)
    } finally {
      deletingId.val = ""
    }
  }

  const handleMerge = async (source: Merchant, targetId: string) => {
    if (!targetId) {
      alert("Pick a target merchant to merge into.")
      return
    }
    const target = merchants.val.find(m => m.merchantId === targetId)
    if (!target) return
    if (!confirm(
      `Merge "${source.name}" into "${target.name}"?\n\n` +
      `Every receipt currently tagged with "${source.name}" will be retagged to "${target.name}", ` +
      `and the "${source.name}" merchant will be deleted. This cannot be undone.`
    )) {
      return
    }
    const key = `${source.merchantId}->${target.merchantId}`
    mergingKey.val = key
    try {
      const result = await api.post(`/merchants/${source.merchantId}/merge`, {
        targetId: target.merchantId,
      })
      // Clear the per-row target picker.
      const next = { ...mergeTarget.val }
      delete next[source.merchantId]
      mergeTarget.val = next
      await load()
      const n = (result as any).retargeted ?? 0
      alert(`Merged. ${n} receipt${n === 1 ? "" : "s"} retargeted.`)
    } catch (err) {
      alert(`Merge failed: ${(err as Error).message}`)
    } finally {
      mergingKey.val = ""
    }
  }

  return div({ class: "merchants-page" },
    div({ class: "page-header" },
      h1("Merchants"),
    ),

    form({ onsubmit: handleCreate, class: "create-form" },
      input({
        type: "text",
        placeholder: "New merchant name",
        value: newName,
        oninput: (e: Event) => newName.val = (e.target as HTMLInputElement).value,
        disabled: creating,
      }),
      button({ type: "submit", disabled: creating || !newName.val.trim() },
        () => creating.val ? "Adding…" : "Add"),
    ),
    () => createError.val
      ? p({ class: "error" }, createError.val)
      : "",

    () => {
      if (error.val) {
        return div({ class: "empty-state" },
          h3("Couldn't load merchants"),
          p(error.val),
          button({ onclick: load }, "Try Again"),
        )
      }
      if (loading.val) {
        return div({ class: "merchants-skeleton" },
          SkeletonRow(), SkeletonRow(), SkeletonRow(),
        )
      }
      if (merchants.val.length === 0) {
        return div({ class: "empty-state" },
          h3("No merchants yet"),
          p("Add one above to get started."),
        )
      }
      // Sort alphabetically for predictable scanning.
      const sorted = [...merchants.val].sort((a, b) => a.name.localeCompare(b.name))
      return div({ class: "items-table-wrapper" },
        table({ class: "responsive-table" },
          tr(
            th("Name"),
            th("Actions"),
          ),
          ...sorted.map(m => {
            if (editingId.val === m.merchantId) {
              // Edit mode: span the row with the edit form.
              return tr({ class: "editing-row" },
                td({ colspan: 2, "data-label": "Edit" },
                  div({ class: "inline-edit-form" },
                    div({ class: "edit-field" },
                      span({ class: "edit-label" }, "Name"),
                      input({
                        type: "text",
                        value: editName,
                        oninput: (e: Event) => { editName.val = (e.target as HTMLInputElement).value },
                        disabled: savingEdit,
                        class: "edit-input",
                      }),
                    ),
                    () => editError.val
                      ? div({ class: "edit-error" }, editError.val)
                      : "",
                    div({ class: "edit-actions" },
                      button({
                        type: "button",
                        onclick: saveEdit,
                        disabled: savingEdit || !editName.val.trim(),
                        class: "btn-sm btn-primary",
                      }, () => savingEdit.val ? "Saving…" : "Save"),
                      button({
                        type: "button",
                        onclick: cancelEdit,
                        disabled: savingEdit,
                        class: "btn-sm btn-secondary",
                      }, "Cancel"),
                    ),
                  ),
                ),
              )
            }
            // Read-mode row. The merge control sits in the actions
            // column so it's visible on both desktop and the
            // stacked mobile layout.
            const mergeKey = `${m.merchantId}->${mergeTarget.val[m.merchantId] ?? ""}`
            const isMerging = mergingKey.val === mergeKey
            const otherMerchants = merchants.val.filter(x => x.merchantId !== m.merchantId)
            return tr(
              td({ "data-label": "Name" }, m.name),
              td({ "data-label": "Actions" },
                div({ class: "merchant-actions" },
                  button({
                    class: "btn-sm btn-secondary",
                    onclick: () => startEdit(m),
                    disabled: () => deletingId.val === m.merchantId || mergingKey.val !== "",
                  }, "Rename"),
                  button({
                    class: "btn-sm btn-danger",
                    onclick: () => handleDelete(m),
                    disabled: () => deletingId.val === m.merchantId || mergingKey.val !== "",
                  }, () => deletingId.val === m.merchantId ? "Deleting…" : "Delete"),
                  div({ class: "merchant-merge" },
                    // Per-row target picker. Wrapped in a function-
                    // child so reading merchants.val is scoped
                    // here, not the surrounding App function-child
                    // (VanJS dep-tracking gotcha).
                    //
                    // The matching <option> is marked selected at
                    // render time: setting select.value before the
                    // <option> children are appended doesn't work,
                    // because the browser records the value at
                    // element-creation time and doesn't re-check it
                    // when options are added. Marking the matching
                    // option as selected makes the browser respect
                    // the choice regardless of child-append order.
                    // Same fix as the receipt edit-mode select.
                    () => {
                      const currentValue = mergeTarget.val[m.merchantId] ?? ""
                      return select({
                        value: currentValue,
                        onchange: (e: Event) => {
                          const v = (e.target as HTMLSelectElement).value
                          mergeTarget.val = { ...mergeTarget.val, [m.merchantId]: v }
                        },
                        disabled: () => deletingId.val === m.merchantId || mergingKey.val !== "",
                        class: "merge-target-picker",
                        "aria-label": `Merge target for ${m.name}`,
                      },
                        option({ value: "", selected: currentValue === "" }, "Merge into…"),
                        ...otherMerchants.map(other =>
                          option({
                            value: other.merchantId,
                            selected: other.merchantId === currentValue,
                          }, other.name),
                        ),
                      )
                    },
                    button({
                      class: "btn-sm btn-secondary",
                      onclick: () => handleMerge(m, mergeTarget.val[m.merchantId] ?? ""),
                      disabled: () => !mergeTarget.val[m.merchantId] || deletingId.val === m.merchantId || mergingKey.val !== "",
                    }, () => isMerging ? "Merging…" : "Merge"),
                  ),
                ),
              ),
            )
          }),
        ),
      )
    },
  )
}

export default MerchantsPage
