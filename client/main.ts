import van from "vanjs-core"

const { div, nav, a, button } = van.tags

// Simple hash-based router
const currentPath = van.state(window.location.hash.slice(1) || "/")

window.addEventListener("hashchange", () => {
  currentPath.val = window.location.hash.slice(1)
})

// navOpen controls the mobile hamburger drawer. It's read by the
// Sidebar component on narrow viewports to toggle the drawer; the
// inline (desktop) nav ignores it entirely. The state is local to
// this module — there's no need to share it with pages.
const navOpen = van.state(false)

export const navigate = (path: string) => {
  window.location.hash = path
}

// Close the mobile drawer on Esc.
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && navOpen.val) {
    navOpen.val = false
  }
})

// API helper
//
// Session auth lives in an HttpOnly cookie set by the server on login.
// The browser auto-attaches the cookie to same-origin requests, so the
// client never reads or stores the token. On 401 we just navigate to
// /login — there is no local state to clean up.
export const api = {
  async fetch(path: string, options: RequestInit = {}) {
    const headers: Record<string, string> = {
      ...options.headers as Record<string, string>,
    }
    const response = await fetch(`/api${path}`, {
      ...options,
      headers,
      credentials: "same-origin",
    })
    if (response.status === 401) {
      navigate("/login")
      throw new Error("Unauthorized")
    }
    // Treat any non-2xx as an error so pages' try/catch blocks actually
    // fire. Without this, 4xx/5xx responses (which have a JSON body of
    // `{"error": "..."}`) would be returned as if they were success, and
    // the page would silently render an empty state instead of an error.
    if (!response.ok) {
      const data = await response.json().catch(() => ({} as Record<string, string>))
      throw new Error(data.error || data.message || `HTTP ${response.status}`)
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
    const response = await fetch(`/api${path}`, {
      method: "POST",
      body: formData,
      credentials: "same-origin",
    })
    if (response.status === 401) {
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

// Logout handler. The server clears the session cookie via
// `POST /api/auth/logout`; the client just needs to call it and redirect
// to `/login`. We always navigate to `/login` even on failure — the
// worst case is the cookie is still valid for one more request, and the
// user explicitly clicked "Sign out".
const handleLogout = async () => {
  try {
    await api.post("/auth/logout", {})
  } catch (err) {
    console.error("Logout request failed:", err)
  } finally {
    navigate("/login")
  }
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
  // Hamburger button — visible only on narrow viewports via CSS.
  button({
    class: "sidebar-hamburger",
    type: "button",
    "aria-label": "Open navigation",
    "aria-expanded": () => navOpen.val ? "true" : "false",
    onclick: () => { navOpen.val = true },
  }, "☰"),

  // Drawer — positioned over the page on narrow viewports. On
  // desktop it's a normal flow element (no positioning).
  div({
    class: () => "sidebar-drawer" + (navOpen.val ? " sidebar-drawer-open" : ""),
  },
    button({
      class: "sidebar-close",
      type: "button",
      "aria-label": "Close navigation",
      onclick: () => { navOpen.val = false },
    }, "✕"),
    a({
      href: "#/",
      "aria-current": () => currentPath.val === "/" ? "page" : null,
      onclick: (e: Event) => { e.preventDefault(); navOpen.val = false; navigate("/") },
    }, "Home"),
    a({
      href: "#/receipts",
      "aria-current": () => {
        const p = currentPath.val
        return p === "/receipts" || p.startsWith("/receipts/") ? "page" : null
      },
      onclick: (e: Event) => { e.preventDefault(); navOpen.val = false; navigate("/receipts") },
    }, "Receipts"),
    a({
      href: "#/items",
      "aria-current": () => {
        const p = currentPath.val
        return p === "/items" || p.startsWith("/items/") ? "page" : null
      },
      onclick: (e: Event) => { e.preventDefault(); navOpen.val = false; navigate("/items") },
    }, "Items"),
    a({
      href: "#/merchants",
      "aria-current": () => currentPath.val === "/merchants" ? "page" : null,
      onclick: (e: Event) => { e.preventDefault(); navOpen.val = false; navigate("/merchants") },
    }, "Merchants"),
    a({
      href: "#/categories",
      "aria-current": () => currentPath.val === "/categories" ? "page" : null,
      onclick: (e: Event) => { e.preventDefault(); navOpen.val = false; navigate("/categories") },
    }, "Categories"),
    a({
      href: "#/analysis",
      "aria-current": () => currentPath.val === "/analysis" ? "page" : null,
      onclick: (e: Event) => { e.preventDefault(); navOpen.val = false; navigate("/analysis") },
    }, "Analysis"),
    // Footer pushed to the bottom of the flex column via `margin-top: auto`
    // (set in CSS). Holds the Sign-out button so it sits below the nav
    // links without disrupting the existing layout.
    div({ class: "sidebar-footer" },
      button({
        class: "sidebar-logout",
        type: "button",
        onclick: () => { navOpen.val = false; handleLogout() },
      }, "Sign out"),
    ),
  ),

  // Backdrop — visible only on narrow viewports when the drawer is open.
  // Clicking it closes the drawer.
  div({
    class: () => "sidebar-backdrop" + (navOpen.val ? " sidebar-backdrop-open" : ""),
    onclick: () => { navOpen.val = false },
  }),
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
import ManualReceiptPage from "./pages/manual-receipt"
import ProposalDetailPage from "./pages/proposal"
import ItemsPage from "./pages/items"
import ItemDetailPage from "./pages/item-detail"
import MergeItemsPage from "./pages/merge-items"
import MerchantsPage from "./pages/merchants"
import CategoriesPage from "./pages/categories"
import AnalysisPage from "./pages/analysis"

// App
//
// We no longer do a synchronous auth check on every navigation. The
// browser auto-attaches the session cookie to same-origin requests, so
// the first API call from any protected page will either succeed (the
// cookie is valid) or 401 (the cookie is missing/expired). The 401
// handler in `api.fetch` redirects to /login. A logged-in user who
// visits /login can simply re-submit the form; the worst case is they
// see the login page for a moment, which is fine.
//
// Route dispatch is a SINGLE function-child of #app. We deliberately
// do NOT nest a second function-child inside Layout that also reads
// `currentPath.val` — VanJS registers every function-child as a
// binding on the states it reads, and a nested pair that share a
// dependency can cause the inner to re-evaluate in subsequent
// `updateDoms` cycles (the receipt page's `loadData()` schedules
// another `updateDoms` microtask, which re-runs the inner, which
// re-mounts the page, which schedules another load — infinite loop).
// Reading `currentPath` once and dispatching directly keeps the
// binding graph a tree.

// Per-page error boundary. Wraps a page component so a synchronous
// render error (typo, missing field, null deref) doesn't take down
// the whole app. The user gets a clear error message and a Reload
// button; the rest of the sidebar/nav remains usable.
//
// This only catches errors during the initial render. Errors thrown
// later from async data fetches are already handled inside the page
// (each page has its own error state). State-update errors that
// happen on user interaction are not caught here — fixing those
// requires a per-binding error boundary, which VanJS doesn't ship.
const withErrorBoundary = (pageName: string, pageFn: () => any) => {
  return () => {
    try {
      return pageFn()
    } catch (err) {
      console.error(`Page ${pageName} crashed during render:`, err)
      return div({ class: "empty-state error-boundary" },
        h2("Something went wrong"),
        p(`The ${pageName} page failed to load.`),
        button({ onclick: () => location.reload() }, "Reload page"),
      )
    }
  }
}

const App = () => {
  return div({ id: "app" },
    () => {
      const path = currentPath.val
      if (path === "/login") {
        return Login()
      }
      if (path === "/") {
        return Layout(withErrorBoundary("Home", HomePage)())
      }
      return Layout(withErrorBoundary(routeName(path), () => PageContent(path))())
    }
  )
}

// Friendly page name for the error boundary message. Falls back to
// the raw path for unknown routes.
const routeName = (path: string): string => {
  if (path === "/") return "Home"
  if (path === "/receipts") return "Receipts"
  if (path === "/receipts/upload") return "Upload"
  if (path === "/receipts/manual") return "Manual entry"
  if (path.startsWith("/receipts/")) return "Receipt detail"
  if (path.startsWith("/proposals/")) return "Proposal detail"
  if (path === "/items") return "Items"
  if (path === "/items/merge") return "Merge items"
  if (path.startsWith("/items/")) return "Item detail"
  if (path === "/merchants") return "Merchants"
  if (path === "/categories") return "Categories"
  if (path === "/analysis") return "Analysis"
  return path
}

const PageContent = (path: string) => {
  if (path === "/receipts") return ReceiptsPage()
  if (path === "/receipts/upload") return UploadPage()
  if (path === "/receipts/manual") return ManualReceiptPage()
  if (path.startsWith("/receipts/")) return ReceiptDetailPage()
  if (path.startsWith("/proposals/")) return ProposalDetailPage()
  if (path === "/items") return ItemsPage()
  if (path === "/items/merge") return MergeItemsPage()
  if (path.startsWith("/items/")) return ItemDetailPage()
  if (path === "/merchants") return MerchantsPage()
  if (path === "/categories") return CategoriesPage()
  if (path === "/analysis") return AnalysisPage()
  return div("404 - Page not found")
}

// Mount
van.add(document.getElementById("app")!, App())
