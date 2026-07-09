package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"code.sirenko.ca/grocer/internal/domain"
)

func TestListUsersRequiresAuth(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/api/users", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestListUsersSortedNoPasswordHash(t *testing.T) {
	router, s := setupTestRouter(t)

	// Create users out of order to verify alphabetical sort.
	users := []*domain.User{
		{UserID: s.UserID.Gen(), Username: "carol", Name: "Carol", PasswordHash: "should-not-leak"},
		{UserID: s.UserID.Gen(), Username: "alice", Name: "Alice", PasswordHash: "should-not-leak"},
		{UserID: s.UserID.Gen(), Username: "bob", Name: "Bob", PasswordHash: "should-not-leak"},
	}
	for _, u := range users {
		if err := s.CreateUser(u); err != nil {
			t.Fatalf("Failed to create user %q: %v", u.Username, err)
		}
	}

	// The first user also needs a session so the auth check passes.
	token := createTestSession(t, s, users[0].UserID)

	req := httptest.NewRequest("GET", "/api/users", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var got []*domain.User
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("Expected 3 users, got %d", len(got))
	}

	// Verify alphabetical by Name.
	expectedOrder := []string{"Alice", "Bob", "Carol"}
	for i, want := range expectedOrder {
		if got[i].Name != want {
			t.Errorf("Position %d: expected Name %q, got %q", i, want, got[i].Name)
		}
	}

	// Verify no passwordHash leakage at the raw JSON level.
	body := w.Body.String()
	if strings.Contains(body, "passwordHash") {
		t.Errorf("Response must not include passwordHash field. Body: %s", body)
	}
	if strings.Contains(body, "should-not-leak") {
		t.Errorf("Response must not include password hash value. Body: %s", body)
	}

	// Spot-check field shape.
	if got[0].UserID == 0 || got[0].Name == "" || got[0].Username == "" {
		t.Errorf("Expected non-zero userId/name/username, got %+v", got[0])
	}
}

func TestListUsersEmpty(t *testing.T) {
	// Auth requires a session, which requires a user. To exercise the
	// "empty list" path, create a user, mint a session, then delete the
	// user. The session is still valid (auth middleware only checks the
	// session row, not that the user still exists), but ListUsers returns
	// an empty slice — which the handler must serialize as `[]`, not
	// `null`.
	router, s := setupTestRouter(t)

	seed := &domain.User{
		UserID:       s.UserID.Gen(),
		Username:     "throwaway",
		Name:         "Throwaway User",
		PasswordHash: "irrelevant",
	}
	if err := s.CreateUser(seed); err != nil {
		t.Fatalf("Failed to create seed user: %v", err)
	}
	token := createTestSession(t, s, seed.UserID)

	if err := s.DeleteUser(seed.Username); err != nil {
		t.Fatalf("Failed to delete seed user: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/users", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	// json.Encoder appends a trailing newline; strip before comparing.
	if got := strings.TrimSpace(body); got != "[]" {
		t.Errorf("Expected empty array `[]`, got %q", body)
	}
}
