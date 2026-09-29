import { createEffect, type JSX } from "solid-js"
import { A, HashRouter, Route, useLocation } from "@solidjs/router"
import { api, navigate } from "./lib/api"
import { setPageTitle } from "./utils"
import Login from "./pages/login.tsx"
import ReceiptsPage from "./pages/receipts.tsx"
import ItemsPage from "./pages/items.tsx"
import ItemDetailPage from "./pages/item-detail.tsx"
import MergeItemsPage from "./pages/merge-items.tsx"
import MerchantsPage from "./pages/merchants.tsx"
import CategoriesPage from "./pages/categories.tsx"

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

// Sidebar nav. The router's <A> applies activeClass="active" on
// prefix match (exact for Home via `end`), which main.css already
// styles alongside [aria-current]. No manual currentPath tracking —
// the VanJS function-child binding workaround is gone with VanJS.
function Sidebar() {
  return (
    <nav class="sidebar">
      <A href="/" activeClass="active" end>Home</A>
      <A href="/receipts" activeClass="active">Receipts</A>
      <A href="/items" activeClass="active">Items</A>
      <A href="/merchants" activeClass="active">Merchants</A>
      <A href="/categories" activeClass="active">Categories</A>
      <A href="/analysis" activeClass="active">Analysis</A>
      <div class="sidebar-footer">
        <button class="sidebar-logout" type="button" onClick={handleLogout}>
          Sign out
        </button>
      </div>
    </nav>
  )
}

// Shell: sidebar layout for every route except /login, plus a
// route-derived default tab title on every navigation. Pages with
// dynamic content (item / receipt / proposal detail) override the
// title in their own effects, which run after this one.
function Shell(props: { children?: JSX.Element }) {
  const location = useLocation()
  createEffect(() => {
    setPageTitle(defaultTitleFor(location.pathname))
  })
  return location.pathname === "/login" ? (
    <>{props.children}</>
  ) : (
    <div class="layout">
      <Sidebar />
      <div class="content">{props.children}</div>
    </div>
  )
}

// Friendly page name for the error boundary message. Falls back to
// the raw path for unknown routes.
export const routeName = (path: string): string => {
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

// defaultTitleFor returns the browser-tab title for a given route.
// Pages with dynamic content (item detail, receipt detail, proposal
// detail) override this in their own reactive render so the tab
// reflects the data on screen, not just the route shape. The empty
// string for "/" lets the home page title be just "Grocer".
export const defaultTitleFor = (path: string): string => {
  const routePath = path.split("?")[0] ?? path
  if (routePath === "/") return ""
  if (routePath === "/login") return "Login"
  if (routePath === "/receipts") return "Receipts"
  if (routePath === "/receipts/upload") return "Upload Receipt"
  if (routePath === "/receipts/manual") return "Manual Receipt"
  if (routePath === "/items") return "Items"
  if (routePath === "/items/merge") return "Merge Items"
  if (routePath === "/merchants") return "Merchants"
  if (routePath === "/categories") return "Categories"
  if (routePath === "/analysis") return "Analysis"
  if (routePath.startsWith("/receipts/")) return "Receipt"
  if (routePath.startsWith("/proposals/")) return "Proposal"
  if (routePath.startsWith("/items/")) return "Item"
  return ""
}

// Placeholder for routes whose pages land in later tasks. Keeps the
// router table complete from Task 1 so each page task only adds its
// component.
function TodoPage() {
  const location = useLocation()
  return <div>404 - Page not found ({routeName(location.pathname)})</div>
}

export default function App() {
  return (
    <HashRouter root={Shell}>
      <Route path="/login" component={Login} />
      <Route path="/" component={TodoPage} />
      <Route path="/receipts" component={ReceiptsPage} />
      <Route path="/receipts/upload" component={TodoPage} />
      <Route path="/receipts/manual" component={TodoPage} />
      <Route path="/receipts/:id" component={TodoPage} />
      <Route path="/proposals/:id" component={TodoPage} />
      <Route path="/items" component={ItemsPage} />
      <Route path="/items/merge" component={MergeItemsPage} />
      <Route path="/items/:id" component={ItemDetailPage} />
      <Route path="/merchants" component={MerchantsPage} />
      <Route path="/categories" component={CategoriesPage} />
      <Route path="/analysis" component={TodoPage} />
      <Route path="*404" component={TodoPage} />
    </HashRouter>
  )
}
