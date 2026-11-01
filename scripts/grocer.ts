#!/usr/bin/env bun
/**
 * grocer — agent CLI for the Grocer receipt tracker.
 *
 * Wraps every HTTP endpoint the website uses so a chat agent can do
 * the same jobs: upload a receipt photo, show the parsed proposal,
 * rename items, fix prices, remove/add lines, approve, plus list
 * receipts, edit saved receipts, and run spending analysis.
 *
 * Auth: needs a session token. Get one once (same machine as server):
 *
 *   go run ./cmd/server --create-session --username vlad
 *
 * Then either export GROCER_TOKEN=<token>, pass --token, or save it to
 * ~/.config/grocer/token (the script does this on `login`).
 * Alternatively set GROCER_USER + GROCER_PASS and the script logs in.
 *
 * Base URL: --url, GROCER_URL, default http://localhost:8080
 */

import { homedir } from "os";
import { join } from "path";

const DEFAULT_URL = process.env.GROCER_URL ?? "http://localhost:8080";
const TOKEN_FILE = join(homedir(), ".config", "grocer", "token");

type Args = { cmd: string; rest: string[]; flags: Record<string, string | boolean> };

function parseArgs(argv: string[]): Args {
  const flags: Record<string, string | boolean> = {};
  const positional: string[] = [];
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a.startsWith("--")) {
      const eq = a.indexOf("=");
      if (eq !== -1) {
        flags[a.slice(2, eq)] = a.slice(eq + 1);
      } else if (i + 1 < argv.length && !argv[i + 1].startsWith("--")) {
        flags[a.slice(2)] = argv[++i];
      } else {
        flags[a.slice(2)] = true;
      }
    } else {
      positional.push(a);
    }
  }
  return { cmd: positional[0] ?? "help", rest: positional.slice(1), flags };
}

async function resolveToken(flags: Record<string, string | boolean>): Promise<string> {
  if (typeof flags.token === "string" && flags.token) return flags.token;
  if (process.env.GROCER_TOKEN) return process.env.GROCER_TOKEN;
  try {
    const t = (await Bun.file(TOKEN_FILE).text()).trim();
    if (t) return t;
  } catch { /* no cached token */ }
  const user = (flags.username as string) ?? process.env.GROCER_USER;
  const pass = (flags.password as string) ?? process.env.GROCER_PASS;
  if (user && pass) {
    const token = await login(baseUrl(flags), user, pass);
    return token;
  }
  console.error(
    "No auth token. Run one of:\n" +
      "  go run ./cmd/server --create-session --username <user>   # prints a token, then export GROCER_TOKEN=<token>\n" +
      "  grocer login --username <user> --password <pass>         # caches token to ~/.config/grocer/token\n" +
      "  GROCER_USER=<u> GROCER_PASS=<p> grocer <cmd>"
  );
  process.exit(1);
}

function baseUrl(flags: Record<string, string | boolean>): string {
  return ((flags.url as string) ?? DEFAULT_URL).replace(/\/$/, "");
}

async function login(base: string, username: string, password: string): Promise<string> {
  const res = await fetch(`${base}/api/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password }),
  });
  if (!res.ok) throw new Error(`login failed: ${res.status} ${await res.text()}`);
  const data = (await res.json()) as { token: string };
  await Bun.write(TOKEN_FILE, data.token);
  return data.token;
}

async function api(base: string, token: string, path: string, init?: RequestInit): Promise<any> {
  const res = await fetch(`${base}${path}`, {
    ...init,
    headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json", ...(init?.headers ?? {}) },
  });
  if (res.status === 204) return null;
  const text = await res.text();
  let data: any = text;
  try { data = text ? JSON.parse(text) : null; } catch { /* plain text */ }
  if (!res.ok) {
    const msg = data?.error ?? text ?? res.statusText;
    throw new Error(`${init?.method ?? "GET"} ${path} → ${res.status}: ${msg}`);
  }
  return data;
}

const fmtMoney = (cents: number) => `$${(cents / 100).toFixed(2)}`;
const fmtDate = (unix: number) => new Date(unix * 1000).toISOString().slice(0, 10);

function printProposal(p: any) {
  console.log(`Proposal ${p.proposalId ?? p.proposalID ?? p.id} [${p.status}]`);
  if (p.merchant) console.log(`Merchant: ${p.merchant} (id ${p.merchantId ?? p.merchantID})`);
  if (p.date) console.log(`Date: ${fmtDate(p.date)}`);
  if (p.totalCents != null) console.log(`Total: ${fmtMoney(p.totalCents)}`);
  const items = p.items ?? [];
  console.log(`Items (${items.length}):`);
  items.forEach((it: any, i: number) => {
    const bits = [
      `#${i}`,
      `"${it.parsedName}"`,
      `qty=${it.quantity}`,
      `unit=${fmtMoney(it.unitPriceCents ?? 0)}`,
      `total=${fmtMoney(it.totalPriceCents ?? 0)}`,
    ];
    if (it.matchedItemId && it.matchedItemId !== "0") bits.push(`matched=${it.matchedItemId}`);
    if (it.categoryId && it.categoryId !== "0") bits.push(`cat=${it.categoryId}`);
    if (it.userChoice) bits.push(`choice=${it.userChoice}`);
    console.log("  " + bits.join(" "));
  });
}

function printReceipt(r: any) {
  console.log(`Receipt ${r.receiptId ?? r.receiptID ?? r.id}`);
  console.log(`Merchant: ${r.merchantName ?? ""} (id ${r.merchantId ?? r.merchantID})  Owner: ${r.ownerName ?? r.ownerId ?? r.ownerID}  Date: ${fmtDate(r.date)}`);
  console.log(`Total: ${fmtMoney(r.totalCents)}  Items: ${(r.items ?? []).length}`);
  (r.items ?? []).forEach((it: any, i: number) => {
    if (it.name !== undefined) {
      console.log(`  #${i} "${it.name}" [${it.categoryName ?? it.categoryId}] qty=${it.quantity} unit=${fmtMoney(it.unitPriceCents)} total=${fmtMoney(it.totalPriceCents)} (item ${it.itemId})`);
    } else {
      console.log(`  #${i} item=${it.itemId} qty=${it.quantity} unit=${fmtMoney(it.unitPriceCents)}`);
    }
  });
}

async function waitForProposal(base: string, token: string, id: string, timeoutMs = 180_000): Promise<any> {
  const start = Date.now();
  for (;;) {
    const p = await api(base, token, `/api/proposals/${id}`);
    if (p.status === "pending" || p.status === "failed" || p.status === "approved") return p;
    if (Date.now() - start > timeoutMs) {
      console.error(`Timed out waiting for parse; current status=${p.status}. Re-run: grocer proposal show ${id}`);
      return p;
    }
    await Bun.sleep(2000);
  }
}

async function main() {
  const { cmd, rest, flags } = parseArgs(Bun.argv.slice(2));
  const json = flags.json === true || flags.json === "true";
  const out = (v: any) => (json ? console.log(JSON.stringify(v, null, 2)) : undefined);

  if (cmd === "help" || cmd === "--help" || cmd === "-h") {
    console.log(`grocer — agent CLI (mirrors the website)

Usage: grocer [--url URL] [--token TOK] [--json] <command> [args]

Auth:  grocer login --username U --password P
Upload: grocer upload <photo.jpg> [--owner ID]
  → uploads, waits for parse, prints the proposal (same as Upload page)

Proposals (review page):
  grocer proposals                              list pending proposals
  grocer proposal show <id>                     full proposal
  grocer proposal rename-item <id> <index> <newName>
  grocer proposal set-price <id> <index> <totalCents> [--qty N]
  grocer proposal set-qty <id> <index> <qty>
  grocer proposal remove-item <id> <index>
  grocer proposal add-item <id> --name N [--qty N] [--unit-cents C] [--total-cents C]
  grocer proposal approve <id> [--choice i:existing|new,...]
  grocer proposal delete <id>
  grocer proposal reparse <id> [--engine full|llm_text|llm_image]

Receipts:
  grocer receipts [--from YYYY-MM-DD --to YYYY-MM-DD --owner ID --category ID --item ID]
  grocer receipt show <id>                       enriched receipt (names + categories)
  grocer receipt set-merchant <id> <merchantId>
  grocer receipt set-date <id> <YYYY-MM-DD[THH:MM]>  (date-only = noon, same as website)
  grocer receipt set-total <id> <totalCents>
  grocer receipt set-item <id> <index> --item <itemId> [--qty N] [--unit-cents C]
  grocer receipt reopen <id>                     back to proposal for full re-edit

Catalog:
  grocer items [query] | grocer item show <id> | grocer item rename <id> <newName>
  grocer item set-category <id> <categoryId> | grocer item history <id>
  grocer items merge <sourceId> <targetId> | grocer item delete <id>
  grocer categories | grocer category create <name> [--parent ID] | grocer category rename <id> <name> | grocer category delete <id>
  grocer merchants  | grocer merchant create <name>  | grocer merchant rename <id> <name>  | grocer merchant delete <id>
  grocer merchants merge <sourceId> <targetId>
  grocer users

Analysis (Analysis page):
  grocer analysis spending [--from D --to D --granularity day|week|month]
  grocer analysis categories [--from D --to D --owner ID --no-rollup]
  grocer analysis family [--from D --to D]
`);
    return;
  }

  const base = baseUrl(flags);

  if (cmd === "login") {
    const user = (flags.username as string) ?? rest[0];
    const pass = (flags.password as string) ?? rest[1] ?? process.env.GROCER_PASS;
    if (!user || !pass) { console.error("Usage: grocer login --username U --password P"); process.exit(1); }
    const token = await login(base, user, pass);
    console.log(`Logged in as ${user}. Token cached to ${TOKEN_FILE}`);
    console.log(`GROCER_TOKEN=${token}`);
    return;
  }

  const token = await resolveToken(flags);
  const get = (p: string) => api(base, token, p);
  const patch = (p: string, b: any) => api(base, token, p, { method: "PATCH", body: JSON.stringify(b) });
  const post = (p: string, b?: any) => api(base, token, p, { method: "POST", body: b === undefined ? undefined : JSON.stringify(b) });
  const del = (p: string) => api(base, token, p, { method: "DELETE" });

  // ---- upload (the chat receipt flow) ----
  if (cmd === "upload") {
    const photo = rest[0];
    if (!photo) { console.error("Usage: grocer upload <photo.jpg>"); process.exit(1); }
    const file = Bun.file(photo);
    if (!(await file.exists())) { console.error(`File not found: ${photo}`); process.exit(1); }
    const form = new FormData();
    form.append("photo", new Blob([await file.arrayBuffer()]), photo.split("/").pop());
    const uploadRes = await fetch(`${base}/api/receipts/upload`, {
      method: "POST",
      headers: { Authorization: `Bearer ${token}` },
      body: form,
    });
    const uploadText = await uploadRes.text();
    if (!uploadRes.ok) throw new Error(`upload → ${uploadRes.status}: ${uploadText}`);
    const { id } = JSON.parse(uploadText) as { id: string };
    console.log(`Uploaded → proposal ${id}, parsing…`);
    const p = await waitForProposal(base, token, id);
    if (json) { console.log(JSON.stringify(p, null, 2)); return; }
    printProposal(p);
    if (p.status === "failed") console.error(`Parse failed: ${p.error ?? p.Error ?? ""}`);
    return;
  }

  // ---- proposals ----
  if (cmd === "proposals") {
    const list = await get("/api/proposals");
    if (json) { console.log(JSON.stringify(list, null, 2)); return; }
    if (!list?.length) { console.log("No pending proposals."); return; }
    for (const p of list) {
      console.log(`${p.proposalId ?? p.proposalID} [${p.status}] ${p.merchant ?? ""} ${fmtMoney(p.totalCents ?? 0)} (${(p.items ?? []).length} items)`);
    }
    return;
  }

  if (cmd === "proposal") {
    const [sub, id, ...a] = rest;
    if (sub === "show") {
      const p = await get(`/api/proposals/${id}`);
      if (json) out(p); else printProposal(p);
      return;
    }
    if (sub === "rename-item") {
      const [index, ...nameParts] = a;
      const cur = await get(`/api/proposals/${id}`);
      const item = cur.items[Number(index)];
      const updated = await patch(`/api/proposals/${id}/items/${index}`, {
        parsedName: nameParts.join(" "),
        quantity: item.quantity,
        unitPriceCents: item.unitPriceCents,
        totalPriceCents: item.totalPriceCents,
      });
      console.log(`Renamed #${index} → "${(updated as any).parsedName}"`);
      return;
    }
    if (sub === "set-price") {
      const [index, totalCents, ...extra] = a;
      const cur = await get(`/api/proposals/${id}`);
      const item = cur.items[Number(index)];
      let qty = item.quantity;
      for (let i = 0; i < extra.length; i++) if (extra[i] === "--qty") qty = Number(extra[i + 1]);
      if (typeof flags.qty === "string") qty = Number(flags.qty);
      const updated = await patch(`/api/proposals/${id}/items/${index}`, {
        parsedName: item.parsedName, quantity: qty, unitPriceCents: 0, totalPriceCents: Number(totalCents),
      });
      console.log(`Set #${index} total → ${fmtMoney(Number(totalCents))}`);
      if (json) out(updated);
      return;
    }
    if (sub === "set-qty") {
      const [index, qty] = a;
      const cur = await get(`/api/proposals/${id}`);
      const item = cur.items[Number(index)];
      const updated = await patch(`/api/proposals/${id}/items/${index}`, {
        parsedName: item.parsedName, quantity: Number(qty),
        unitPriceCents: item.unitPriceCents, totalPriceCents: item.totalPriceCents,
      });
      console.log(`Set #${index} qty → ${qty}`);
      if (json) out(updated);
      return;
    }
    if (sub === "remove-item") {
      await del(`/api/proposals/${id}/items/${a[0]}`);
      console.log(`Removed item #${a[0]} from proposal ${id}`);
      return;
    }
    if (sub === "add-item") {
      const created = await post(`/api/proposals/${id}/items`, {});
      const idx = (await get(`/api/proposals/${id}`)).items.length - 1;
      const body: any = {
        parsedName: (flags.name as string) ?? a[0] ?? "",
        quantity: Number(flags.qty ?? 1),
        unitPriceCents: Number(flags["unit-cents"] ?? 0),
        totalPriceCents: Number(flags["total-cents"] ?? 0),
      };
      const updated = await patch(`/api/proposals/${id}/items/${idx}`, body);
      console.log(`Added item #${idx}: "${(updated as any).parsedName}"`);
      if (json) out(updated);
      void created;
      return;
    }
    if (sub === "approve") {
      const choices: Record<number, string> = {};
      const raw = (flags.choice as string) ?? "";
      for (const part of raw.split(",").filter(Boolean)) {
        const [i, c] = part.split(":");
        choices[Number(i)] = c;
      }
      const receipt = await post(`/api/proposals/${id}/approve`, { choices });
      if (json) { console.log(JSON.stringify(receipt, null, 2)); return; }
      console.log(`Approved → receipt ${receipt.receiptId ?? receipt.receiptID} ${fmtMoney(receipt.totalCents)} (${(receipt.items ?? []).length} items)`);
      return;
    }
    if (sub === "delete") {
      await del(`/api/proposals/${id}`);
      console.log(`Deleted proposal ${id}`);
      return;
    }
    if (sub === "reparse") {
      const engine = (flags.engine as string) ?? "full";
      const r = await post(`/api/proposals/${id}/reparse`, { engine });
      console.log(`Reparse started for proposal ${(r as any).id ?? id} (engine=${engine})`);
      return;
    }
    throw new Error(`unknown proposal subcommand: ${sub}`);
  }

  // ---- receipts ----
  if (cmd === "receipts") {
    const q = new URLSearchParams();
    for (const k of ["from", "to", "owner", "category", "item"]) if (flags[k]) q.set(k, String(flags[k]));
    const list = await get(`/api/receipts/enriched${q.size ? "?" + q : ""}`);
    if (json) { console.log(JSON.stringify(list, null, 2)); return; }
    if (!list?.length) { console.log("No receipts."); return; }
    for (const r of list) {
      console.log(`${r.receiptId} ${r.merchantName} ${fmtDate(r.date)} ${fmtMoney(r.totalCents)} (${r.itemCount} items, ${r.ownerName})`);
    }
    return;
  }

  if (cmd === "receipt") {
    const [sub, id, ...a] = rest;
    if (sub === "show") {
      const r = await get(`/api/receipts/${id}/enriched`);
      if (json) out(r); else printReceipt(r);
      return;
    }
    if (sub === "set-merchant") {
      const r = await patch(`/api/receipts/${id}`, { merchantId: a[0] });
      console.log(`Receipt ${id} merchant → ${(r as any).merchantID ?? a[0]}`);
      return;
    }
    if (sub === "set-total") {
      const r = await patch(`/api/receipts/${id}`, { totalCents: Number(a[0]) });
      console.log(`Receipt ${id} total → ${fmtMoney((r as any).totalCents)}`);
      return;
    }
    if (sub === "set-date") {
      // Mirrors the website's datetime-local handling: parsed as
      // machine-local wall-clock (the agent machine runs on
      // America/Vancouver, same as the user). Date-only defaults
      // to noon, matching the server's LLM date convention.
      const raw = a[0] ?? "";
      const dt = /^\d{4}-\d{2}-\d{2}$/.test(raw) ? `${raw}T12:00:00` : raw;
      const unix = Math.floor(new Date(dt).getTime() / 1000);
      if (!Number.isFinite(unix)) throw new Error(`bad date: ${raw} (want YYYY-MM-DD or YYYY-MM-DDTHH:MM)`);
      await patch(`/api/receipts/${id}`, { date: unix });
      console.log(`Receipt ${id} date → ${new Date(unix * 1000).toISOString()} (unix ${unix})`);
      return;
    }
    if (sub === "set-item") {
      const [index] = a;
      const body: any = {};
      if (flags.item) body.itemId = flags.item;
      if (flags.qty) body.quantity = Number(flags.qty);
      if (flags["unit-cents"]) body.unitPriceCents = Number(flags["unit-cents"]);
      const r = await patch(`/api/receipts/${id}/items/${index}`, body);
      console.log(`Receipt ${id} item #${index} updated`);
      if (json) out(r);
      return;
    }
    if (sub === "reopen") {
      const r = await post(`/api/receipts/${id}/reopen`, {});
      console.log(`Receipt ${id} reopened → proposal ${(r as any).id}`);
      return;
    }
    throw new Error(`unknown receipt subcommand: ${sub}`);
  }

  // ---- catalog ----
  if (cmd === "items" && rest[0] === "merge") {
    const r = await post(`/api/items/${rest[1]}/merge`, { targetId: rest[2] });
    console.log(`Merged item ${rest[1]} → ${rest[2]} (${(r as any).retargeted} lines)`);
    return;
  }
  if (cmd === "items") {
    const q = rest[0];
    const list = q
      ? await get(`/api/search/items?q=${encodeURIComponent(q)}`)
      : await get(`/api/items/insights`);
    if (json) { console.log(JSON.stringify(list, null, 2)); return; }
    for (const it of list) {
      console.log(`${it.itemId ?? it.itemID} "${it.name}" cat=${it.categoryId ?? it.categoryID} bought=${it.purchaseCount ?? "?"}x`);
    }
    return;
  }
  if (cmd === "item") {
    const [sub, id, ...a] = rest;
    if (sub === "show") {
      const it = await get(`/api/items/${id}`);
      if (json) { console.log(JSON.stringify(it, null, 2)); return; }
      console.log(`${it.itemId} "${it.name}" cat=${it.categoryId} normalized="${it.normalized}" aliases=[${(it.aliases ?? []).join(", ")}]`);
      return;
    }
    if (sub === "rename") {
      const it = await patch(`/api/items/${id}`, { name: a.join(" ") });
      console.log(`Renamed item ${id} → "${(it as any).name}"`);
      return;
    }
    if (sub === "set-category") {
      const it = await patch(`/api/items/${id}`, { categoryId: a[0] });
      console.log(`Item ${id} category → ${(it as any).categoryID ?? a[0]}`);
      return;
    }
    if (sub === "history") {
      const h = await get(`/api/analysis/items/${id}`);
      if (json) { console.log(JSON.stringify(h, null, 2)); return; }
      for (const p of h) console.log(`${p.date} ${fmtMoney(p.price * 100)} receipt=${p.receiptId}`);
      return;
    }
    if (sub === "delete") {
      await del(`/api/items/${id}`);
      console.log(`Deleted item ${id}`);
      return;
    }
    throw new Error(`unknown item subcommand: ${sub}`);
  }
  if (cmd === "merge" && rest[0] === "items") {
    const r = await post(`/api/items/${rest[1]}/merge`, { targetId: rest[2] });
    console.log(`Merged item ${rest[1]} → ${rest[2]} (${(r as any).retargeted} lines)`);
    return;
  }

  if (cmd === "categories") {
    const list = await get("/api/categories");
    if (json) { console.log(JSON.stringify(list, null, 2)); return; }
    for (const c of list) console.log(`${c.categoryId} "${c.name}" parent=${c.parentId ?? c.parentID ?? "-"}`);
    return;
  }
  if (cmd === "category") {
    const [sub, ...a] = rest;
    if (sub === "create") {
      const body: any = { name: (flags.name as string) ?? a.join(" ") };
      if (flags.parent) body.parentId = flags.parent;
      const c = await post("/api/categories", body);
      console.log(`Created category ${(c as any).categoryId} "${(c as any).name}"`);
      return;
    }
    if (sub === "rename") {
      const c = await patch(`/api/categories/${a[0]}`, { name: a.slice(1).join(" ") });
      console.log(`Renamed category ${a[0]} → "${(c as any).name}"`);
      return;
    }
    if (sub === "delete") {
      await del(`/api/categories/${a[0]}`);
      console.log(`Deleted category ${a[0]}`);
      return;
    }
    throw new Error(`unknown category subcommand: ${sub}`);
  }

  if (cmd === "merchants") {
    const list = await get("/api/merchants");
    if (json) { console.log(JSON.stringify(list, null, 2)); return; }
    for (const m of list) console.log(`${m.merchantId} "${m.name}"`);
    return;
  }
  if (cmd === "merchant") {
    const [sub, ...a] = rest;
    if (sub === "create") {
      const m = await post("/api/merchants", { name: a.join(" ") });
      console.log(`Created merchant ${(m as any).merchantId} "${(m as any).name}"`);
      return;
    }
    if (sub === "rename") {
      const m = await patch(`/api/merchants/${a[0]}`, { name: a.slice(1).join(" ") });
      console.log(`Renamed merchant ${a[0]} → "${(m as any).name}"`);
      return;
    }
    if (sub === "delete") {
      await del(`/api/merchants/${a[0]}`);
      console.log(`Deleted merchant ${a[0]}`);
      return;
    }
    throw new Error(`unknown merchant subcommand: ${sub}`);
  }
  if (cmd === "merchants" && (rest[0] === "merge" || flags.source)) {
    const src = rest[1] ?? (flags.source as string);
    const dst = rest[2] ?? (flags.target as string);
    const r = await post(`/api/merchants/${src}/merge`, { targetId: dst });
    console.log(`Merged merchant ${src} → ${dst} (${(r as any).retargeted} receipts)`);
    return;
  }
  if (cmd === "merchants-merge") {
    const r = await post(`/api/merchants/${rest[0]}/merge`, { targetId: rest[1] });
    console.log(`Merged merchant ${rest[0]} → ${rest[1]} (${(r as any).retargeted} receipts)`);
    return;
  }

  if (cmd === "users") {
    const list = await get("/api/users");
    if (json) { console.log(JSON.stringify(list, null, 2)); return; }
    for (const u of list) console.log(`${u.userId ?? u.userID} ${u.username} (${u.name})`);
    return;
  }

  if (cmd === "analysis") {
    const [kind, ..._a] = rest;
    const q = new URLSearchParams();
    for (const k of ["from", "to", "owner", "granularity"]) if (flags[k]) q.set(k, String(flags[k]));
    if (kind === "categories" && (flags["no-rollup"] === true || flags["no-rollup"] === "true")) q.set("rollup", "false");
    const suffix = q.size ? "?" + q : "";
    if (kind === "spending") {
      const rows = await get(`/api/analysis/spending${suffix}`);
      if (json) { console.log(JSON.stringify(rows, null, 2)); return; }
      let total = 0;
      for (const r of rows) { console.log(`${r.period} $${r.total.toFixed(2)}`); total += r.total; }
      console.log(`TOTAL $${total.toFixed(2)}`);
      return;
    }
    if (kind === "categories") {
      const rows = await get(`/api/analysis/categories${suffix}`);
      if (json) { console.log(JSON.stringify(rows, null, 2)); return; }
      for (const r of rows) console.log(`${r.name} $${r.total.toFixed(2)}`);
      return;
    }    if (kind === "family") {
      const rows = await get(`/api/analysis/family${suffix}`);
      if (json) { console.log(JSON.stringify(rows, null, 2)); return; }
      for (const r of rows) console.log(`${r.name} $${r.total.toFixed(2)}`);
      return;
    }
    throw new Error("usage: grocer analysis spending|categories|family");
  }

  console.error(`Unknown command: ${cmd}. Run: grocer help`);
  process.exit(1);
}

try {
  await main();
} catch (e: any) {
  console.error(`Error: ${e.message}`);
  process.exit(1);
}
