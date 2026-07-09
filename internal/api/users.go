package api

import (
	"net/http"
	"sort"

	"code.sirenko.ca/grocer/internal/domain"
)

// handleListUsers returns all users sorted by Name ascending.
//
// The endpoint is consumed by the client to seed an in-memory lookup map
// (userId → name) for owner display. Per the UX overhaul spec, the owner
// name is fetched here but not yet displayed in the UI; the data is loaded
// so that enabling owner display later requires no backend work.
//
// Sorting is intentional: at family scale (handful of users) the cost is
// negligible, and a stable, alphabetical order makes the endpoint easy to
// reason about in logs and test snapshots.
//
// The `domain.User` type already tags `PasswordHash` with `json:"-"`, so
// serializing the slice directly is safe — no DTO wrapper is required.
func (r *Router) handleListUsers(w http.ResponseWriter, req *http.Request) {
	users, err := r.store.ListUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Guard against `json.Encode(nil) == "null"`. The contract is an array,
	// even when empty. This same guard will be needed in ticket 03 for the
	// enriched receipts list endpoint.
	if users == nil {
		users = []*domain.User{}
	}

	sort.Slice(users, func(i, j int) bool {
		return users[i].Name < users[j].Name
	})

	writeJSON(w, http.StatusOK, users)
}
