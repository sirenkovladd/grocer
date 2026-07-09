package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"code.sirenko.ca/grocer/internal/domain"
)

// uintToStr converts a uint64 to its base-10 string. Used in URL paths.
func uintToStr(n uint64) string { return fmt.Sprintf("%d", n) }

// sessionForTest creates a user and a session directly in the store, then
// returns the session cookie. Bypasses /api/auth/login to avoid the
// global login rate limit (5 req/min) that would otherwise fail when the
// test suite is run end-to-end.
func sessionForTest(t *testing.T, router *Router, username string) *http.Cookie {
	t.Helper()
	s := router.store
	user := &domain.User{
		UserID:       s.UserID.Gen(),
		Username:     username,
		Name:         username,
		PasswordHash: hashPasswordForTest("test-password"),
	}
	if err := s.CreateUser(user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	token := createTestSession(t, s, user.UserID)
	return &http.Cookie{Name: "session", Value: token}
}

// makeTestProposal creates a proposal with a few items, saves it, and
// returns its ID. Used by the add/delete item tests.
func makeTestProposal(t *testing.T, router *Router, items []domain.ProposalItem) uint64 {
	t.Helper()
	// We need to inject a proposal directly into the store. The store
	// doesn't expose a public Set method, but we can use the transaction.
	// Simpler: create via a fake photo URL and an ID we control.
	// For tests, the easiest path is to insert via the memdb transaction.
	// Since we don't expose that, fall back to creating via the public
	// CreateProposal path with a real (synthetic) merchant and item IDs.
	s := router.store
	merchant := &domain.Merchant{
		MerchantID: s.MerchantID.Gen(),
		Name:       "TestStore",
	}
	if err := s.CreateMerchant(merchant); err != nil {
		t.Fatalf("CreateMerchant: %v", err)
	}
	proposal := &domain.Proposal{
		ProposalID: s.ProposalID.Gen(),
		OwnerID:    1,
		MerchantID: merchant.MerchantID,
		Merchant:   merchant.Name,
		Items:      items,
		Status:     "pending",
	}
	if err := s.CreateProposal(proposal); err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	return proposal.ProposalID
}

func TestAddProposalItem(t *testing.T) {
	router, s := setupTestRouter(t)
	id := makeTestProposal(t, router, []domain.ProposalItem{
		{ParsedName: "Milk", Quantity: 1, UnitPriceCents: 449, TotalPriceCents: 449},
	})

	// Need a logged-in session to call the endpoint
	_ = s
	login := sessionForTest(t, router, "test-add-item")

	req := httptest.NewRequest("POST", "/api/proposals/"+uintToStr(id)+"/items", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(login)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got domain.ProposalItem
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Quantity != 1 {
		t.Errorf("expected default quantity 1, got %v", got.Quantity)
	}

	// Verify the proposal now has 2 items
	proposal, _ := s.GetProposal(id)
	if len(proposal.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(proposal.Items))
	}
}

func TestAddProposalItemRejectsBadID(t *testing.T) {
	router, _ := setupTestRouter(t)
	login := sessionForTest(t, router, "test-add-bad")

	req := httptest.NewRequest("POST", "/api/proposals/notanumber/items", bytes.NewReader([]byte("{}")))
	req.AddCookie(login)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestDeleteProposalItem(t *testing.T) {
	router, s := setupTestRouter(t)
	id := makeTestProposal(t, router, []domain.ProposalItem{
		{ParsedName: "Milk", Quantity: 1, UnitPriceCents: 449, TotalPriceCents: 449},
		{ParsedName: "Bread", Quantity: 1, UnitPriceCents: 299, TotalPriceCents: 299},
		{ParsedName: "Eggs", Quantity: 1, UnitPriceCents: 399, TotalPriceCents: 399},
	})
	login := sessionForTest(t, router, "test-delete-item")

	// Delete the middle one
	req := httptest.NewRequest("DELETE", "/api/proposals/"+uintToStr(id)+"/items/1", nil)
	req.AddCookie(login)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	proposal, _ := s.GetProposal(id)
	if len(proposal.Items) != 2 {
		t.Fatalf("expected 2 items after delete, got %d", len(proposal.Items))
	}
	if proposal.Items[0].ParsedName != "Milk" {
		t.Errorf("expected Milk at 0, got %q", proposal.Items[0].ParsedName)
	}
	if proposal.Items[1].ParsedName != "Eggs" {
		t.Errorf("expected Eggs at 1 (was 2), got %q", proposal.Items[1].ParsedName)
	}
}

func TestDeleteProposalItemRejectsOutOfRange(t *testing.T) {
	router, _ := setupTestRouter(t)
	id := makeTestProposal(t, router, []domain.ProposalItem{
		{ParsedName: "Milk", Quantity: 1, UnitPriceCents: 449, TotalPriceCents: 449},
	})
	login := sessionForTest(t, router, "test-delete-oor")

	req := httptest.NewRequest("DELETE", "/api/proposals/"+uintToStr(id)+"/items/99", nil)
	req.AddCookie(login)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestDeleteProposalItemRequiresAuth(t *testing.T) {
	router, _ := setupTestRouter(t)
	id := makeTestProposal(t, router, []domain.ProposalItem{
		{ParsedName: "Milk", Quantity: 1, UnitPriceCents: 449, TotalPriceCents: 449},
	})

	req := httptest.NewRequest("DELETE", "/api/proposals/"+uintToStr(id)+"/items/0", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}
