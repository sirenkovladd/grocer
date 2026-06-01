import van from "vanjs-core"
import { api, navigate } from "../main"

const { div, h1, table, tr, td, th, button } = van.tags

interface Item {
  itemId: number
  name: string
  categoryId: number
  merchantId: number
  normalized: string
  aliases: string[]
}

const ItemsPage = () => {
  const items = van.state<Item[]>([])
  const loading = van.state(true)

  const loadItems = async () => {
    loading.val = true
    try {
      const data = await api.get("/items")
      items.val = data || []
    } catch (err) {
      console.error("Failed to load items:", err)
    }
    loading.val = false
  }

  loadItems()

  return div({ class: "items-page" },
    div({ class: "page-header" },
      h1("Items"),
    ),
    table(
      tr(
        th("Name"),
        th("Category"),
        th("Aliases"),
        th("Actions"),
      ),
      ...items.val.map(item =>
        tr(
          td(item.name),
          td(item.categoryId.toString()),
          td(item.aliases.join(", ")),
          td(
            button({ onclick: () => navigate(`/items/${item.itemId}`) }, "View"),
          ),
        )
      ),
    ),
  )
}

export default ItemsPage
