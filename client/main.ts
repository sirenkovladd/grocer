import van from "vanjs-core"

const { div, nav, a, button } = van.tags

// Simple hash-based router
const currentPath = van.state(window.location.hash.slice(1) || "/")

window.addEventListener("hashchange", () => {
  currentPath.val = window.location.hash.slice(1)
})

export const navigate = (path: string) => {
  window.location.hash = path
}

// API helper
export const api = {
  async fetch(path: string, options: RequestInit = {}) {
    const token = localStorage.getItem("token")
    const headers: Record<string, string> = {
      ...options.headers as Record<string, string>,
    }
    if (token) {
      headers["Authorization"] = `Bearer ${token}`
    }
    const response = await fetch(`/api${path}`, { ...options, headers })
    if (response.status === 401) {
      navigate("/login")
      throw new Error("Unauthorized")
    }
    return response.json()
  },

  get: (path: string) => api.fetch(path),
  post: (path: string, body: any) => api.fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  }),
  patch: (path: string, body: any) => api.fetch(path, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  }),
  delete: (path: string) => api.fetch(path, { method: "DELETE" }),
}

// Layout
const Layout = (content: any) => div({ class: "layout" },
  nav({ class: "sidebar" },
    a({ href: "#/receipts", onclick: () => navigate("/receipts") }, "Receipts"),
    a({ href: "#/proposals", onclick: () => navigate("/proposals") }, "Proposals"),
    a({ href: "#/items", onclick: () => navigate("/items") }, "Items"),
    a({ href: "#/categories", onclick: () => navigate("/categories") }, "Categories"),
    a({ href: "#/analysis", onclick: () => navigate("/analysis") }, "Analysis"),
  ),
  div({ class: "content" }, content),
)

// Import pages
import Login from "./pages/login"
import ReceiptsPage from "./pages/receipts"
import ReceiptDetailPage from "./pages/receipt"
import ProposalsPage from "./pages/proposals"
import ItemsPage from "./pages/items"
import CategoriesPage from "./pages/categories"
import AnalysisPage from "./pages/analysis"

// App
const App = () => {
  return div({ id: "app" },
    () => {
      const path = currentPath.val
      
      if (path === "/login") {
        return Login()
      }
      
      if (path === "/") {
        navigate("/receipts")
        return div()
      }
      
      return Layout(PageContent(path))
    }
  )
}

const PageContent = (path: string) => {
  if (path === "/receipts") return ReceiptsPage()
  if (path.startsWith("/receipts/")) return ReceiptDetailPage()
  if (path === "/proposals") return ProposalsPage()
  if (path === "/items") return ItemsPage()
  if (path === "/categories") return CategoriesPage()
  if (path === "/analysis") return AnalysisPage()
  return div("404 - Page not found")
}

// Mount
van.add(document.getElementById("app")!, App())
