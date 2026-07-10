package api

import (
	"log"
	"net/http"
	"strings"
	"time"

	"code.sirenko.ca/grocer/internal/llm"
)

// timezoneHeader is the request header carrying the user's IANA
// timezone (e.g. "America/Los_Angeles"). The browser sets it via
// Intl.DateTimeFormat().resolvedOptions().timeZone on every API
// request; the server reads it to anchor date-only LLM/OCR output
// to noon in the user's local timezone.
//
// The header is the source of truth for timezone-aware date parsing
// because the LLM/OCR returns dates with no timezone (e.g. "2026-07-10"),
// and the server can't infer the user's timezone from anything else.
// Storing receipts at noon UTC would display correctly for most users
// but produces a weird time-of-day (4am in PST, 9pm in JST) — anchoring
// to noon in the user's timezone makes the displayed time useful.
const timezoneHeader = "X-Timezone"

// withTimezone extracts the X-Timezone header and stores it on the
// request context via llm.WithTimezone, so downstream LLM/OCR date
// parsing is anchored to the user's local timezone. Invalid or
// missing timezones silently fall back to UTC — the request still
// succeeds, just with the legacy "noon UTC" anchor.
//
// Only applied to endpoints that produce a receipt date (upload,
// reparse, apply-external). Read endpoints and pure-data updates
// (PATCH receipt) don't need it because the client sends Unix
// seconds directly.
func (r *Router) withTimezone(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		raw := strings.TrimSpace(req.Header.Get(timezoneHeader))
		if raw != "" {
			tz, err := time.LoadLocation(raw)
			if err != nil {
				// Don't fail the request — bad timezone is the
				// client's bug, not the user's. Log so we can
				// notice stale client builds.
				log.Printf("timezone: ignoring invalid X-Timezone %q: %v", raw, err)
			} else {
				req = req.WithContext(llm.WithTimezone(req.Context(), tz))
			}
		}
		next(w, req)
	}
}
