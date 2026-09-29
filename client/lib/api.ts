import { getUserTimezone } from "../utils"

export const navigate = (path: string) => {
  window.location.hash = path
}

// Stringify a domain ID for JSON transport.
//
// JavaScript's Number type cannot safely represent integers above
// 2^53, but our domain IDs are timestamp-based and exceed that
// (18 digits). Sending an ID as a number silently corrupts it via
// rounding before the request even leaves the browser. The server
// accepts both forms, but always sending strings preserves
// precision end-to-end.
//
// Treats null, undefined, and empty string as null so call sites
// can use this in place of `value || null` for optional IDs.
export const idStr = (v: string | number | null | undefined): string | null => {
  if (v == null || v === "") return null
  return String(v)
}

// API helper
//
// Session auth lives in an HttpOnly cookie set by the server on login.
// The browser auto-attaches the cookie to same-origin requests, so the
// client never reads or stores the token. On 401 we just navigate to
// /login — there is no local state to clean up.
//
// Every request also carries an X-Timezone header so the server can
// anchor LLM/OCR date strings to noon in the user's local timezone.
// Without this, "2026-07-10" from the LLM becomes midnight UTC and
// displays as the previous day in negative-UTC zones. We send the
// header on ALL requests (not just the ones that produce a date) so
// the server never has to special-case which paths need it; the
// timezone middleware is a no-op for read paths and other endpoints
// that don't parse dates.
export const api = {
  async fetch(path: string, options: RequestInit = {}) {
    const headers: Record<string, string> = {
      ...options.headers as Record<string, string>,
      "X-Timezone": getUserTimezone(),
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
      headers: {
        "X-Timezone": getUserTimezone(),
      },
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
