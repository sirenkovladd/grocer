import { test, expect, describe } from "bun:test"
import {
  formatMoney,
  formatDate,
  formatRelativeDate,
  formatQuantity,
  shortId,
  indexBy,
} from "./utils"

describe("formatMoney", () => {
  test("whole dollars", () => {
    expect(formatMoney(100)).toBe("$1.00")
  })
  test("fractional cents", () => {
    expect(formatMoney(3670)).toBe("$36.70")
  })
  test("zero", () => {
    expect(formatMoney(0)).toBe("$0.00")
  })
  test("large value with thousands separator", () => {
    expect(formatMoney(123456)).toBe("$1,234.56")
  })
})

describe("formatDate", () => {
  test("zero returns Unknown date", () => {
    expect(formatDate(0)).toBe("Unknown date")
  })
  test("specific date", () => {
    // 2026-05-30 00:00:00 UTC. formatDate displays in local time, so the
    // exact calendar day depends on the test runner's timezone. Match
    // the structure instead of the literal string.
    const unix = Math.floor(Date.UTC(2026, 4, 30) / 1000)
    const result = formatDate(unix)
    expect(result).toMatch(/^May \d{1,2}, 2026$/)
  })
})

describe("formatRelativeDate", () => {
  const now = Date.now()

  test("zero returns Unknown", () => {
    expect(formatRelativeDate(0)).toBe("Unknown")
  })
  test("today", () => {
    const today = Math.floor(now / 1000) // same day
    expect(formatRelativeDate(today)).toBe("Today")
  })
  test("yesterday", () => {
    const yesterday = Math.floor((now - 24 * 60 * 60 * 1000) / 1000)
    expect(formatRelativeDate(yesterday)).toBe("Yesterday")
  })
  test("3 days ago", () => {
    const days3 = Math.floor((now - 3 * 24 * 60 * 60 * 1000) / 1000)
    expect(formatRelativeDate(days3)).toBe("3 days ago")
  })
  test("2 weeks ago (14 days)", () => {
    const days14 = Math.floor((now - 14 * 24 * 60 * 60 * 1000) / 1000)
    expect(formatRelativeDate(days14)).toBe("2 weeks ago")
  })
  test("2 months ago (60 days)", () => {
    const days60 = Math.floor((now - 60 * 24 * 60 * 60 * 1000) / 1000)
    expect(formatRelativeDate(days60)).toBe("2 months ago")
  })
  test("2 years ago (730 days)", () => {
    const days730 = Math.floor((now - 730 * 24 * 60 * 60 * 1000) / 1000)
    expect(formatRelativeDate(days730)).toBe("2 years ago")
  })

  // Bug fix from grill: future dates should not print "−1 days ago".
  test("future date falls back to absolute formatDate", () => {
    const tomorrow = Math.floor((now + 24 * 60 * 60 * 1000) / 1000)
    const result = formatRelativeDate(tomorrow)
    // Should NOT be "Yesterday" or "−1 days ago"; should be an absolute date.
    expect(result).not.toBe("Yesterday")
    expect(result).not.toMatch(/-?\d+ days ago/)
    // Should look like "May 30, 2026" etc.
    expect(result).toMatch(/[A-Z][a-z]{2} \d{1,2}, \d{4}/)
  })
})

describe("formatQuantity", () => {
  test("integer", () => {
    expect(formatQuantity(1)).toBe("1")
  })
  test("fractional", () => {
    expect(formatQuantity(0.875)).toBe("0.875")
  })
  test("half", () => {
    expect(formatQuantity(0.5)).toBe("0.5")
  })
  test("non-finite returns empty", () => {
    expect(formatQuantity(NaN)).toBe("")
    expect(formatQuantity(Infinity)).toBe("")
  })

  // Bug fix from grill: float-precision artifacts should be clamped.
  test("clamps 0.1 + 0.2 artifact", () => {
    expect(formatQuantity(0.1 + 0.2)).toBe("0.3")
  })
  test("clamps 1.1 + 2.2 artifact", () => {
    // 1.1 + 2.2 = 3.3000000000000003, should display as "3.3"
    expect(formatQuantity(1.1 + 2.2)).toBe("3.3")
  })
})

describe("shortId", () => {
  test("short id unchanged", () => {
    expect(shortId("123")).toBe("123")
  })
  test("exactly 10 chars unchanged", () => {
    expect(shortId("1234567890")).toBe("1234567890")
  })
  test("11+ chars truncated", () => {
    expect(shortId("12345678901")).toBe("12345678…")
  })
  test("accepts number", () => {
    expect(shortId(935128556887867392)).toMatch(/^93512855…$/)
  })
})

describe("indexBy", () => {
  test("builds map by key", () => {
    const users = [
      { id: 1, name: "Alice" },
      { id: 2, name: "Bob" },
    ]
    const byId = indexBy(users, u => u.id)
    expect(byId["1"].name).toBe("Alice")
    expect(byId["2"].name).toBe("Bob")
  })
  test("empty list", () => {
    expect(indexBy([], (x: unknown) => x as string)).toEqual({})
  })
  test("string keys", () => {
    const items = [{ key: "a", v: 1 }, { key: "b", v: 2 }]
    const byKey = indexBy(items, i => i.key)
    expect(byKey["a"].v).toBe(1)
    expect(byKey["b"].v).toBe(2)
  })
})
