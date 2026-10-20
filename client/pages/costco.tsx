import { createSignal, For, Show } from "solid-js"
import { api, navigate } from "../lib/api.ts"
import { formatMoney, formatDate } from "../utils.ts"

interface CostcoSummary {
  barcode: string
  date: string
  warehouseName: string
  total: number
  totalItemCount: number
  alreadyImported: boolean
}

interface CostcoLinePreview {
  name: string
  sku: string
  quantity: number
  unitPriceCents: number
  totalPriceCents: number
  matchedItemId?: string
  matchedName?: string
  willCreate: boolean
}

interface CostcoDetailPreview {
  barcode: string
  warehouseName: string
  date: number
  totalCents: number
  alreadyImported: boolean
  lines: CostcoLinePreview[]
}

interface User {
  userId: string
  name: string
}

export default function CostcoPage() {
  const [listPaste, setListPaste] = createSignal("")
  const [listRows, setListRows] = createSignal<CostcoSummary[]>([])
  const [listError, setListError] = createSignal<string | null>(null)
  const [listLoading, setListLoading] = createSignal(false)

  const [detailPaste, setDetailPaste] = createSignal("")
  const [details, setDetails] = createSignal<CostcoDetailPreview[]>([])
  const [detailError, setDetailError] = createSignal<string | null>(null)
  const [detailLoading, setDetailLoading] = createSignal(false)

  const [users, setUsers] = createSignal<User[]>([])
  const [ownerId, setOwnerId] = createSignal("")
  const [mode, setMode] = createSignal<"proposal" | "receipt">("proposal")
  const [importing, setImporting] = createSignal(false)
  const [importResult, setImportResult] = createSignal<any>(null)
  const [importError, setImportError] = createSignal<string | null>(null)

  // Direct-fetch (experimental)
  const [bearer, setBearer] = createSignal("")
  const [startDate, setStartDate] = createSignal("8/01/2026")
  const [endDate, setEndDate] = createSignal("10/31/2026")
  const [syncLoading, setSyncLoading] = createSignal(false)
  const [syncError, setSyncError] = createSignal<string | null>(null)

  // Load users for the owner picker.
  const loadUsers = async () => {
    try {
      const u = (await api.get("/users")) as User[]
      setUsers(Array.isArray(u) ? u : [])
      if (u.length > 0 && !ownerId()) setOwnerId(u[0]!.userId)
    } catch (e) {
      console.warn("Failed to load users:", e)
    }
  }
  loadUsers()

  const previewList = async () => {
    setListLoading(true)
    setListError(null)
    try {
      const res = (await api.post("/costco/preview-list", {
        content: listPaste(),
      })) as CostcoSummary[]
      setListRows(res)
      if (res.length === 0) setListError("No receipts found in that paste.")
    } catch (e: any) {
      setListError(e.message || "Preview failed")
    } finally {
      setListLoading(false)
    }
  }

  const previewDetails = async () => {
    setDetailLoading(true)
    setDetailError(null)
    try {
      const res = (await api.post("/costco/preview-detail", {
        content: detailPaste(),
      })) as CostcoDetailPreview[]
      setDetails(res)
      if (res.length === 0) setDetailError("No receipt details found.")
    } catch (e: any) {
      setDetailError(e.message || "Preview failed")
    } finally {
      setDetailLoading(false)
    }
  }

  const doImport = async () => {
    setImporting(true)
    setImportError(null)
    setImportResult(null)
    try {
      const res = await api.post("/costco/import", {
        ownerId: ownerId() || undefined,
        details: detailPaste(),
        mode: mode(),
      })
      setImportResult(res)
      // Refresh previews so imported rows flip to "already imported".
      if (listRows().length > 0) await previewList()
      if (detailPaste()) await previewDetails()
    } catch (e: any) {
      setImportError(e.message || "Import failed")
    } finally {
      setImporting(false)
    }
  }

  const doSync = async () => {
    setSyncLoading(true)
    setSyncError(null)
    try {
      const res = (await api.post("/costco/sync", {
        bearer: bearer(),
        startDate: startDate(),
        endDate: endDate(),
      })) as CostcoSummary[]
      setListRows(res)
    } catch (e: any) {
      setSyncError(
        (e.message || "Sync failed") +
          " — token likely expired or bot-challenged. Use paste import below."
      )
    } finally {
      setSyncLoading(false)
    }
  }

  const newCount = () => details().filter((d) => !d.alreadyImported).length

  return (
    <div class="costco-page">
      <div class="breadcrumb">
        <a href="#/" onClick={(e) => { e.preventDefault(); navigate("/"); }}>Home</a>
        <span class="separator">›</span>
        <span class="current">Costco import</span>
      </div>

      <div class="page-header">
        <div>
          <h1>Costco import</h1>
          <p class="muted">
            Import warehouse receipts from costco.ca order history. Paste-first: no
            passwords or tokens are stored — you copy the GraphQL responses from
            DevTools and Grocer parses them locally.
          </p>
        </div>
      </div>

      <details class="costco-help">
        <summary>How to copy the JSON (2 minutes)</summary>
        <ol>
          <li>Open <b>costco.ca</b> logged in → <b>Orders &amp; Purchases</b> → order history.</li>
          <li>Open DevTools (<b>F12</b>) → <b>Network</b> tab → filter <b>graphql</b>.</li>
          <li>Reload the page. Click the <b>receiptsWithCounts</b> request with{" "}
            <code>startDate</code> in the payload (that's the <b>list</b> query).</li>
          <li><b>Response</b> tab → copy the whole body → paste into <b>Step 1</b> below.</li>
          <li>Click into any receipt (or open each barcode). Each click fires a{" "}
            <b>receiptsWithCounts</b> request with <code>barcode</code> (the{" "}
            <b>detail</b> query). Copy each response → paste all of them into{" "}
            <b>Step 2</b> (one after another, or as a JSON array).</li>
          <li>Pick an owner, choose <b>Proposal</b> (review first, recommended) or{" "}
            <b>Receipt</b> (commit directly), then <b>Import</b>.</li>
        </ol>
        <p class="muted">
          Tokens in DevTools expire after ~10 minutes and can't be reused by the
          server reliably (Kasada bot check). Paste import always works; the
          experimental direct-fetch in Step 3 is a convenience when the token
          is seconds old.
        </p>
      </details>

      <section class="card">
        <h2>Step 1 — List (which receipts?)</h2>
        <textarea
          class="mono paste-box"
          rows={6}
          placeholder='Paste the list query response here ({"data":{"receiptsWithCounts":{...}}})'
          value={listPaste()}
          onInput={(e) => setListPaste(e.currentTarget.value)}
        />
        <div class="row-actions">
          <button class="btn-primary" onClick={previewList} disabled={listLoading() || !listPaste()}>
            {listLoading() ? "Parsing…" : "Preview list"}
          </button>
          <Show when={listRows().length > 0}>
            <span class="muted">{listRows().length} receipts found</span>
          </Show>
        </div>
        <Show when={listError()}><p class="error">{listError()}</p></Show>
        <Show when={listRows().length > 0}>
          <div class="items-table-wrapper">
            <table class="responsive-table">
              <tr><th>Date</th><th>Warehouse</th><th class="money">Total</th><th>Items</th><th>Status</th><th>Barcode</th></tr>
              <For each={listRows()}>
                {(r) => (
                  <tr>
                    <td data-label="Date">{r.date || "—"}</td>
                    <td data-label="Warehouse">{r.warehouseName || "Costco"}</td>
                    <td data-label="Total" class="money">{formatMoney(Math.round(r.total * 100))}</td>
                    <td data-label="Items">{r.totalItemCount}</td>
                    <td data-label="Status">{r.alreadyImported ? "✓ imported" : "new"}</td>
                    <td data-label="Barcode" class="mono muted">{r.barcode}</td>
                  </tr>
                )}
              </For>
            </table>
          </div>
        </Show>
      </section>

      <section class="card">
        <h2>Step 2 — Details (line items)</h2>
        <textarea
          class="mono paste-box"
          rows={8}
          placeholder="Paste one or more detail responses here (single object, JSON array, or one per line)"
          value={detailPaste()}
          onInput={(e) => setDetailPaste(e.currentTarget.value)}
        />
        <div class="row-actions">
          <button class="btn-primary" onClick={previewDetails} disabled={detailLoading() || !detailPaste()}>
            {detailLoading() ? "Parsing…" : "Preview details"}
          </button>
          <Show when={details().length > 0}>
            <span class="muted">{details().length} receipts · {newCount()} new</span>
          </Show>
        </div>
        <Show when={detailError()}><p class="error">{detailError()}</p></Show>
        <For each={details()}>
          {(d) => (
            <div class="costco-detail">
              <h3>
                {d.warehouseName || "Costco"} · {formatDate(d.date)} ·{" "}
                {formatMoney(d.totalCents)}{" "}
                <span class="muted mono">{d.barcode}</span>{" "}
                {d.alreadyImported && <span class="badge">✓ imported</span>}
              </h3>
              <div class="items-table-wrapper">
                <table class="responsive-table">
                  <tr><th>Item</th><th>SKU</th><th>Qty</th><th class="money">Unit</th><th class="money">Total</th><th>Catalog</th></tr>
                  <For each={d.lines}>
                    {(ln) => (
                      <tr>
                        <td data-label="Item">{ln.name}</td>
                        <td data-label="SKU" class="mono muted">{ln.sku}</td>
                        <td data-label="Qty">{ln.quantity}</td>
                        <td data-label="Unit" class="money muted">{formatMoney(ln.unitPriceCents)}</td>
                        <td data-label="Total" class="money">{formatMoney(ln.totalPriceCents)}</td>
                        <td data-label="Catalog">
                          {ln.matchedName ? (
                            <span class="muted">↔ {ln.matchedName}</span>
                          ) : (
                            <span class="badge-new">+ new item</span>
                          )}
                        </td>
                      </tr>
                    )}
                  </For>
                </table>
              </div>
            </div>
          )}
        </For>
      </section>

      <section class="card">
        <h2>Step 3 — Import</h2>
        <div class="form-grid">
          <label>
            Owner
            <select value={ownerId()} onChange={(e) => setOwnerId(e.currentTarget.value)}>
              <For each={users()}>
                {(u) => <option value={u.userId}>{u.name}</option>}
              </For>
            </select>
          </label>
          <label>
            Mode
            <select value={mode()} onChange={(e) => setMode(e.currentTarget.value as any)}>
              <option value="proposal">Proposal (review first — recommended)</option>
              <option value="receipt">Receipt (commit directly)</option>
            </select>
          </label>
        </div>
        <p class="muted">
          Proposal mode creates one reviewable proposal per receipt (same flow as
          photo uploads — match, categorize, approve). Receipt mode commits
          immediately and creates catalog items for new lines.
        </p>
        <div class="row-actions">
          <button
            class="btn-primary"
            onClick={doImport}
            disabled={importing() || !detailPaste()}
          >
            {importing() ? "Importing…" : `Import ${newCount() || ""} receipt(s)`.trim()}
          </button>
        </div>
        <Show when={importError()}><p class="error">{importError()}</p></Show>
        <Show when={importResult()}>
          <div class="import-result">
            <p>
              Created {(importResult() as any).created?.length ?? 0} · skipped{" "}
              {(importResult() as any).skipped?.length ?? 0} (already imported) · failed{" "}
              {(importResult() as any).failed?.length ?? 0}
            </p>
            <For each={(importResult() as any).created ?? []}>
              {(c: any) => (
                <p>
                  <span class="mono muted">{c.barcode}</span> →{" "}
                  {c.mode === "proposal" ? (
                    <a href={`#/proposals/${c.id}`} onClick={(e) => { e.preventDefault(); navigate(`/proposals/${c.id}`); }}>
                      review proposal
                    </a>
                  ) : (
                    <a href={`#/receipts/${c.id}`} onClick={(e) => { e.preventDefault(); navigate(`/receipts/${c.id}`); }}>
                      view receipt
                    </a>
                  )}
                </p>
              )}
            </For>
            <For each={(importResult() as any).failed ?? []}>
              {(f: any) => <p class="error">{f.barcode}: {f.error}</p>}
            </For>
          </div>
        </Show>
      </section>

      <details class="card">
        <summary>Experimental: direct fetch with a fresh bearer (optional)</summary>
        <p class="muted">
          Paste <code>costco-x-authorization</code> (without <code>Bearer </code>) from a
          DevTools request made <b>seconds</b> ago. The server calls Costco once for the
          list. Fails when the token expires or Kasada challenges — then use paste import.
          The token is used once and never stored.
        </p>
        <div class="form-grid">
          <label class="span-2">
            Bearer (costco-x-authorization)
            <input
              type="password"
              class="mono"
              autocomplete="off"
              spellcheck={false}
              placeholder="eyJhbGciOiJSUzI1NiIs..."
              value={bearer()}
              onInput={(e) => setBearer(e.currentTarget.value)}
            />
          </label>
          <label>
            Start (M/D/YYYY)
            <input value={startDate()} onInput={(e) => setStartDate(e.currentTarget.value)} />
          </label>
          <label>
            End (M/D/YYYY)
            <input value={endDate()} onInput={(e) => setEndDate(e.currentTarget.value)} />
          </label>
        </div>
        <div class="row-actions">
          <button class="btn-secondary" onClick={doSync} disabled={syncLoading() || !bearer()}>
            {syncLoading() ? "Fetching…" : "Fetch list from Costco"}
          </button>
        </div>
        <Show when={syncError()}><p class="error">{syncError()}</p></Show>
      </details>
    </div>
  )
}
