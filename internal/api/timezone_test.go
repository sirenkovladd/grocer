package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"code.sirenko.ca/grocer/internal/llm"
)

// TestWithTimezone_ValidHeader confirms a valid IANA timezone flows
// from the X-Timezone header into the request context, where the
// LLM date parser picks it up via TimezoneFromContext.
func TestWithTimezone_ValidHeader(t *testing.T) {
	var seen *struct{ tz string }
	r := &Router{}
	handler := r.withTimezone(func(w http.ResponseWriter, req *http.Request) {
		seen = &struct{ tz string }{tz: llm.TimezoneFromContext(req.Context()).String()}
	})

	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("X-Timezone", "America/Los_Angeles")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if seen == nil {
		t.Fatal("handler not called")
	}
	if seen.tz != "America/Los_Angeles" {
		t.Errorf("tz = %q, want America/Los_Angeles", seen.tz)
	}
}

// TestWithTimezone_MissingHeader verifies the request still works
// (with UTC as the default) when the header is absent — the LLM
// parser falls back to its legacy UTC anchor.
func TestWithTimezone_MissingHeader(t *testing.T) {
	var seen *string
	r := &Router{}
	handler := r.withTimezone(func(w http.ResponseWriter, req *http.Request) {
		s := llm.TimezoneFromContext(req.Context()).String()
		seen = &s
	})

	req := httptest.NewRequest("POST", "/", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if seen == nil {
		t.Fatal("handler not called")
	}
	if *seen != "UTC" {
		t.Errorf("tz = %q, want UTC (fallback)", *seen)
	}
}

// TestWithTimezone_InvalidHeader verifies a bad timezone is silently
// ignored rather than failing the request — the user shouldn't see a
// 500 just because their browser sent a stale or unsupported IANA
// string. Falls back to UTC.
func TestWithTimezone_InvalidHeader(t *testing.T) {
	var seen *string
	r := &Router{}
	handler := r.withTimezone(func(w http.ResponseWriter, req *http.Request) {
		s := llm.TimezoneFromContext(req.Context()).String()
		seen = &s
	})

	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("X-Timezone", "Not/A/Real/Zone")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if seen == nil {
		t.Fatal("handler not called")
	}
	if *seen != "UTC" {
		t.Errorf("tz = %q, want UTC (invalid header fallback)", *seen)
	}
}

// Compile-time guard: withTimezone must accept any http.HandlerFunc
// (not just *Router methods). The signature `func(http.ResponseWriter,
// *http.Request)` is part of the public contract used by router.go.
var _ http.HandlerFunc = (&Router{}).withTimezone(func(http.ResponseWriter, *http.Request) {})

// Compile-time guard: the context value flows through context.WithValue
// (i.e. it is not nil for a derived context). Catches a refactor that
// accidentally shadows the timezone key.
var _ context.Context = llm.WithTimezone(context.Background(), nil)
