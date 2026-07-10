// Shared formatting and lookup helpers used across every page.
//
// Centralizing formatters here means one place to change locale, format,
// or rounding behavior. Importers: ticket 07 (receipts list), 08 (receipt
// detail), 09 (items list), 10 (item detail), 11 (home).

// ---------------------------------------------------------------------------
// Money
// ---------------------------------------------------------------------------

// Cents (integer) → "$36.70".
//
// Uses Intl.NumberFormat for locale-aware currency formatting (USD is
// hardcoded per the UX overhaul spec, Risk 4).
export const formatMoney = (cents: number): string => {
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
  }).format(cents / 100)
}

// ---------------------------------------------------------------------------
// Dates
// ---------------------------------------------------------------------------

// Unix seconds → "May 30, 2026" (en-US, short month).
export const formatDate = (unixSeconds: number): string => {
  if (!unixSeconds) return "Unknown date"
  return new Intl.DateTimeFormat("en-US", {
    year: "numeric",
    month: "short",
    day: "numeric",
  }).format(new Date(unixSeconds * 1000))
}

// Unix seconds → "May 30, 2026, 2:30 PM" (en-US, short month, 12-hour
// time, user's local timezone). Used for receipt/proposal display
// where the user wants to see when the purchase happened, not just
// the calendar date.
//
// Both this and formatRelativeDate are timezone-correct because they
// rely on Intl.DateTimeFormat (which defaults to the user's local
// timezone) — the storage is UTC but the display is local. The
// "day-shift bug" we used to have (midnight UTC being the previous
// day in negative-UTC zones) is fixed at the server by anchoring
// LLM/OCR dates to noon in the user's timezone; this function then
// renders the resulting instant in the user's local timezone.
export const formatDateTime = (unixSeconds: number): string => {
  if (!unixSeconds) return "Unknown date"
  return new Intl.DateTimeFormat("en-US", {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
    hour12: true,
  }).format(new Date(unixSeconds * 1000))
}

// Return the user's IANA timezone (e.g. "America/Los_Angeles").
// Intl.DateTimeFormat().resolvedOptions().timeZone is the standard
// way to detect this in the browser — it returns the OS-level
// timezone setting, not a hard-coded value.
//
// Falls back to "UTC" if the browser doesn't support
// resolvedOptions().timeZone (very old browsers) so the server
// still gets a parseable value and can fall back to its legacy
// UTC anchor.
export const getUserTimezone = (): string => {
  try {
    const tz = Intl.DateTimeFormat().resolvedOptions().timeZone
    if (tz) return tz
  } catch {
    // Intl not available — fall through.
  }
  return "UTC"
}

// Unix seconds → "3 days ago" / "Today" / "Yesterday" / "May 30, 2026" (fallback).
//
// Switches from relative to absolute formatting at 30 days (the days→weeks
// transition) so precision isn't lost after a month. Thresholds:
//   0   days   → "Today"
//   1   day    → "Yesterday"
//   < 7 days   → "N days ago"
//   < 30 days  → "N weeks ago"
//   < 365 days → "N months ago"
//   else       → "N years ago"
//
// Future dates (diffDays < 0) fall back to the absolute formatDate — this
// happens if a clock skew or future-dated record is present, and we
// don't want to print "−1 days ago" or "Yesterday" for tomorrow.
export const formatRelativeDate = (unixSeconds: number): string => {
  if (!unixSeconds) return "Unknown"
  const date = new Date(unixSeconds * 1000)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24))

  // Future or same-day: future → absolute, same-day → "Today".
  if (diffDays < 0) return formatDate(unixSeconds)
  if (diffDays === 0) return "Today"
  if (diffDays === 1) return "Yesterday"
  if (diffDays < 7) return `${diffDays} days ago`
  if (diffDays < 30) return `${Math.floor(diffDays / 7)} weeks ago`
  if (diffDays < 365) return `${Math.floor(diffDays / 30)} months ago`
  return `${Math.floor(diffDays / 365)} years ago`
}

// ---------------------------------------------------------------------------
// Quantities
// ---------------------------------------------------------------------------

// 0.875 → "0.875", 1 → "1", 1.5 → "1.5".
//
// Clamps to 3 decimal places before stringification so float-precision
// artifacts (e.g. 0.1 + 0.2 = 0.30000000000000004) don't leak into the UI.
// Three decimals is more than enough for receipt quantities (1/8, 1/4, 1/2
// pounds; 0.5 L; etc.) and avoids most of the artifacts that come from
// computed quantities.
export const formatQuantity = (qty: number): string => {
  if (!Number.isFinite(qty)) return ""
  const rounded = Math.round(qty * 1000) / 1000
  return rounded.toString()
}

// ---------------------------------------------------------------------------
// ID display
// ---------------------------------------------------------------------------

// Long numeric/ID string → "93512855…" for display.
//
// Accepts string OR number because:
//   - Backend returns IDs as JSON strings (uint64, see json:"...,string")
//   - Existing pages type IDs as number, which works for navigation but
//     loses precision at the edges. Tickets 07-11 should type IDs as
//     string; this helper accepts both for transition.
export const shortId = (id: number | string): string => {
  const s = String(id)
  return s.length > 10 ? `${s.slice(0, 8)}…` : s
}

// ---------------------------------------------------------------------------
// CSV
// ---------------------------------------------------------------------------

// Escape a single CSV field per RFC 4180:
//   - Always wrap in double quotes (simplest correct behavior)
//   - Escape any embedded double quotes by doubling them
// Always-quoting is acceptable for our use case (small CSVs); it
// avoids the "does this field need quoting?" branch.
export const csvField = (v: string | number | null | undefined): string => {
  if (v === null || v === undefined) return '""'
  return `"${String(v).replace(/"/g, '""')}"`
}

// Build a CSV string from a header row and an array of row arrays.
// Each cell is passed through csvField automatically.
export const toCsv = (
  headers: string[],
  rows: (string | number | null | undefined)[][],
): string => {
  const lines: string[] = []
  lines.push(headers.map(csvField).join(","))
  for (const row of rows) {
    lines.push(row.map(csvField).join(","))
  }
  // CRLF per RFC 4180; Excel and Numbers both accept it.
  return lines.join("\r\n") + "\r\n"
}

// Trigger a browser download of a string as a file. Creates a blob,
// generates an object URL, clicks an invisible <a download="...">,
// then revokes the URL. Works in all evergreen browsers.
export const downloadFile = (filename: string, content: string, mime: string) => {
  const blob = new Blob([content], { type: mime })
  const url = URL.createObjectURL(blob)
  const a = document.createElement("a")
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  // Defer revoke so the download has time to start. 0ms is enough
  // in Chrome/Firefox/Safari for object URLs; the actual download
  // reads from the blob directly.
  setTimeout(() => URL.revokeObjectURL(url), 0)
}

// ---------------------------------------------------------------------------
// Lookups
// ---------------------------------------------------------------------------

// Build an index map from a list, using keyFn to extract the key.
// Coerces keys to string so callers can mix number/string IDs safely.
//
// Example:
//   const userById = indexBy(users, u => u.userId)
export const indexBy = <T>(
  items: T[],
  keyFn: (item: T) => string | number
): Record<string, T> => {
  const result: Record<string, T> = {}
  for (const item of items) {
    result[String(keyFn(item))] = item
  }
  return result
}
