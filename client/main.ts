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

export const isAuthenticated = () => !!localStorage.getItem("token")

// Routes that don't require authentication
const publicRoutes = new Set(["/login"])

// Auth guard: returns true if navigation should proceed
const guardAuth = (path: string): boolean => {
  if (!isAuthenticated() && !publicRoutes.has(path)) {
    navigate("/login")
    return false
  }
  if (isAuthenticated() && publicRoutes.has(path)) {
    navigate("/")
    return false
  }
  return true
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
      localStorage.removeItem("token")
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
  postFormData: async (path: string, formData: FormData) => {
    const token = localStorage.getItem("token")
    const headers: Record<string, string> = {}
    if (token) {
      headers["Authorization"] = `Bearer ${token}`
    }
    const response = await fetch(`/api${path}`, {
      method: "POST",
      headers,
      body: formData,
    })
    if (response.status === 401) {
      localStorage.removeItem("token")
      navigate("/login")
      throw new Error("Unauthorized")
    }
    if (!response.ok) {
      const data = await response.json()
      throw new Error(data.message || data.error || "Request failed")
    }
    return response.json()
  },
}

// Layout
//
// The Sidebar is its own reactive component: the active link highlights
// based on `currentPath.val`. Using function-valued attributes for
// `aria-current` makes VanJS track the dependency and re-render the
// attribute on every route change. `e.preventDefault()` in the onclick
// handler stops the browser from updating the hash twice (once via the
// default anchor behavior, once via the explicit navigate() call).
const Sidebar = () => nav({ class: "sidebar" },
  a({
    href: "#/",
    "aria-current": () => currentPath.val === "/" ? "page" : null,
    onclick: (e: Event) => { e.preventDefault(); navigate("/") },
  }, "Home"),
  a({
    href: "#/receipts",
    "aria-current": () => {
      const p = currentPath.val
      return p === "/receipts" || p.startsWith("/receipts/") ? "page" : null
    },
    onclick: (e: Event) => { e.preventDefault(); navigate("/receipts") },
  }, "Receipts"),
  a({
    href: "#/items",
    "aria-current": () => {
      const p = currentPath.val
      return p === "/items" || p.startsWith("/items/") ? "page" : null
    },
    onclick: (e: Event) => { e.preventDefault(); navigate("/items") },
  }, "Items"),
  a({
    href: "#/merchants",
    "aria-current": () => currentPath.val === "/merchants" ? "page" : null,
    onclick: (e: Event) => { e.preventDefault(); navigate("/merchants") },
  }, "Merchants"),
  a({
    href: "#/categories",
    "aria-current": () => currentPath.val === "/categories" ? "page" : null,
    onclick: (e: Event) => { e.preventDefault(); navigate("/categories") },
  }, "Categories"),
  a({
    href: "#/analysis",
    "aria-current": () => currentPath.val === "/analysis" ? "page" : null,
    onclick: (e: Event) => { e.preventDefault(); navigate("/analysis") },
  }, "Analysis"),
)

const Layout = (content: any) => div({ class: "layout" },
  Sidebar(),
  div({ class: "content" }, content),
)

// Import pages
import Login from "./pages/login"
import HomePage from "./pages/home"
import ReceiptsPage from "./pages/receipts"
import ReceiptDetailPage from "./pages/receipt"
import UploadPage from "./pages/upload"
import ProposalDetailPage from "./pages/proposal"
import ItemsPage from "./pages/items"
import ItemDetailPage from "./pages/item-detail"
import MerchantsPage from "./pages/merchants"
import CategoriesPage from "./pages/categories"
import AnalysisPage from "./pages/analysis"

// App
const App = () => {
  return div({ id: "app" },
    () => {
      const path = currentPath.val

      // Auth guards — redirect before rendering
      if (!guardAuth(path)) return div()

      if (path === "/login") {
        return Login()
      }

      if (path === "/") {
        return Layout(HomePage())
      }

      return Layout(
        () => PageContent(currentPath.val)
      )
    }
  )
}

const PageContent = (path: string) => {
  if (path === "/receipts") return ReceiptsPage()
  if (path === "/receipts/upload") return UploadPage()
  if (path.startsWith("/receipts/")) return ReceiptDetailPage()
  if (path.startsWith("/proposals/")) return ProposalDetailPage()
  if (path === "/items") return ItemsPage()
  if (path.startsWith("/items/")) return ItemDetailPage()
  if (path === "/merchants") return MerchantsPage()
  if (path === "/categories") return CategoriesPage()
  if (path === "/analysis") return AnalysisPage()
  return div("404 - Page not found")
}

// Mount
van.add(document.getElementById("app")!, App())
