import van from "vanjs-core"
import { api } from "../main"

const { div, h1, button, input, form, span, ul, li } = van.tags

interface Category {
  categoryId: number
  name: string
  parentId: number | null
  sortOrder: number
}

const CategoryTree = (categories: Category[], onEdit: (id: number) => void) => {
  const buildTree = (parentId: number | null): Category[] => {
    return categories
      .filter(c => c.parentId === parentId)
      .sort((a, b) => a.sortOrder - b.sortOrder)
  }

  const renderNode = (category: Category) => {
    const children = buildTree(category.categoryId)
    
    return li(
      div({ class: "category-node" },
        span(category.name),
        button({ onclick: () => onEdit(category.categoryId) }, "Edit"),
      ),
      children.length > 0 ? ul(...children.map(renderNode)) : "",
    )
  }

  const rootCategories = buildTree(null)

  return ul({ class: "category-tree" },
    ...rootCategories.map(renderNode),
  )
}

const CategoriesPage = () => {
  const categories = van.state<Category[]>([])
  const loading = van.state(true)
  const newName = van.state("")
  const editingId = van.state<number | null>(null)
  const editName = van.state("")

  const loadCategories = async () => {
    loading.val = true
    try {
      const data = await api.get("/categories")
      categories.val = data || []
    } catch (err) {
      console.error("Failed to load categories:", err)
    }
    loading.val = false
  }

  loadCategories()

  const handleCreate = async (e: Event) => {
    e.preventDefault()
    if (!newName.val) return

    try {
      await api.post("/categories", { name: newName.val })
      newName.val = ""
      loadCategories()
    } catch (err) {
      console.error("Failed to create category:", err)
    }
  }

  const handleEdit = (id: number) => {
    const cat = categories.val.find(c => c.categoryId === id)
    if (cat) {
      editingId.val = id
      editName.val = cat.name
    }
  }

  const handleUpdate = async (e: Event) => {
    e.preventDefault()
    if (!editingId.val || !editName.val) return

    try {
      await api.patch(`/categories/${editingId.val}`, { name: editName.val })
      editingId.val = null
      editName.val = ""
      loadCategories()
    } catch (err) {
      console.error("Failed to update category:", err)
    }
  }

  return div({ class: "categories-page" },
    div({ class: "page-header" },
      h1("Categories"),
    ),
    form({ onsubmit: handleCreate, class: "create-form" },
      input({
        type: "text",
        placeholder: "New category name",
        value: newName,
        oninput: (e: Event) => newName.val = (e.target as HTMLInputElement).value,
      }),
      button({ type: "submit" }, "Add"),
    ),
    () => editingId.val
      ? form({ onsubmit: handleUpdate, class: "edit-form" },
          input({
            type: "text",
            value: editName,
            oninput: (e: Event) => editName.val = (e.target as HTMLInputElement).value,
          }),
          button({ type: "submit" }, "Save"),
          button({ type: "button", onclick: () => editingId.val = null }, "Cancel"),
        )
      : "",
    () => loading.val
      ? div("Loading...")
      : CategoryTree(categories.val, handleEdit),
  )
}

export default CategoriesPage
