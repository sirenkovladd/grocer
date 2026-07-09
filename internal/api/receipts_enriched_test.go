package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"code.sirenko.ca/grocer/internal/domain"
	"code.sirenko.ca/grocer/internal/store"
)

// enrichedFixture builds a complete receipt (user, merchant, category,
// item, receipt) so enriched tests can exercise the full join path.
type enrichedFixture struct {
	router   *Router
	store    *store.Store
	user     *domain.User
	merchant *domain.Merchant
	category *domain.Category
	item     *domain.Item
	receipt  *domain.Receipt
	token    string
}

func newEnrichedFixture(t *testing.T) *enrichedFixture {
	t.Helper()

	router, s := setupTestRouter(t)

	user := &domain.User{
		UserID:       s.UserID.Gen(),
		Username:     "alice",
		Name:         "Alice",
		PasswordHash: "x",
	}
	if err := s.CreateUser(user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	merchant := &domain.Merchant{
		MerchantID: s.MerchantID.Gen(),
		Name:       "Whole Foods",
	}
	if err := s.CreateMerchant(merchant); err != nil {
		t.Fatalf("create merchant: %v", err)
	}

	category := &domain.Category{
		CategoryID: s.CategoryID.Gen(),
		Name:       "Produce",
	}
	if err := s.CreateCategory(category); err != nil {
		t.Fatalf("create category: %v", err)
	}

	item := &domain.Item{
		ItemID:     s.ItemID.Gen(),
		Name:       "Bananas",
		CategoryID: category.CategoryID,
		MerchantID: merchant.MerchantID,
		Normalized: "bananas",
	}
	if err := s.CreateItem(item); err != nil {
		t.Fatalf("create item: %v", err)
	}

	// Two items, second one tests rounding: 0.5 * 333 = 166.5 → 167.
	receipt := &domain.Receipt{
		ReceiptID:  s.ReceiptID.Gen(),
		MerchantID: merchant.MerchantID,
		OwnerID:    user.UserID,
		Date:       1717000000, // 2024-05-29
		Items: []domain.ReceiptItem{
			{ItemID: item.ItemID, Quantity: 2, UnitPriceCents: 333},
			{ItemID: item.ItemID, Quantity: 0.5, UnitPriceCents: 333},
		},
		TotalCents: 666,
	}
	if err := s.CreateReceipt(receipt); err != nil {
		t.Fatalf("create receipt: %v", err)
	}

	token := createTestSession(t, s, user.UserID)

	return &enrichedFixture{
		router:   router,
		store:    s,
		user:     user,
		merchant: merchant,
		category: category,
		item:     item,
		receipt:  receipt,
		token:    token,
	}
}

// ---------------------------------------------------------------------------
// /api/receipts/enriched
// ---------------------------------------------------------------------------

func TestListEnrichedReceiptsRequiresAuth(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/api/receipts/enriched", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestListEnrichedReceiptsEmpty(t *testing.T) {
	// Use a router whose store has no receipts.
	router, s := setupTestRouter(t)
	user := &domain.User{UserID: s.UserID.Gen(), Username: "u", Name: "U", PasswordHash: "x"}
	if err := s.CreateUser(user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := createTestSession(t, s, user.UserID)

	req := httptest.NewRequest("GET", "/api/receipts/enriched", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
	if got := strings.TrimSpace(w.Body.String()); got != "[]" {
		t.Errorf("Expected empty array `[]`, got %q", w.Body.String())
	}
}

func TestListEnrichedReceiptsShapeAndFallback(t *testing.T) {
	f := newEnrichedFixture(t)

	req := httptest.NewRequest("GET", "/api/receipts/enriched", nil)
	req.Header.Set("Authorization", "Bearer "+f.token)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var got []EnrichedReceiptSummary
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Expected 1 receipt, got %d", len(got))
	}

	r := got[0]
	if r.ReceiptID != f.receipt.ReceiptID {
		t.Errorf("ReceiptID: got %d, want %d", r.ReceiptID, f.receipt.ReceiptID)
	}
	if r.MerchantID != f.merchant.MerchantID {
		t.Errorf("MerchantID mismatch")
	}
	if r.MerchantName != "Whole Foods" {
		t.Errorf("MerchantName: got %q, want %q", r.MerchantName, "Whole Foods")
	}
	if r.OwnerID != f.user.UserID {
		t.Errorf("OwnerID mismatch")
	}
	if r.OwnerName != "Alice" {
		t.Errorf("OwnerName: got %q, want %q", r.OwnerName, "Alice")
	}
	if r.ItemCount != 2 {
		t.Errorf("ItemCount: got %d, want 2", r.ItemCount)
	}
	if r.TotalCents != 666 {
		t.Errorf("TotalCents: got %d, want 666", r.TotalCents)
	}
	if r.Date != 1717000000 {
		t.Errorf("Date: got %d, want 1717000000", r.Date)
	}
}

func TestListEnrichedReceiptsSortedByDateDesc(t *testing.T) {
	router, s := setupTestRouter(t)
	user := &domain.User{UserID: s.UserID.Gen(), Username: "u", Name: "U", PasswordHash: "x"}
	if err := s.CreateUser(user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	merchant := &domain.Merchant{MerchantID: s.MerchantID.Gen(), Name: "M"}
	if err := s.CreateMerchant(merchant); err != nil {
		t.Fatalf("create merchant: %v", err)
	}
	token := createTestSession(t, s, user.UserID)

	// Three receipts with ascending dates — sorted output should be DESC.
	for _, d := range []int64{1700000000, 1710000000, 1690000000} {
		if err := s.CreateReceipt(&domain.Receipt{
			ReceiptID:  s.ReceiptID.Gen(),
			MerchantID: merchant.MerchantID,
			OwnerID:    user.UserID,
			Date:       d,
		}); err != nil {
			t.Fatalf("create receipt: %v", err)
		}
	}

	req := httptest.NewRequest("GET", "/api/receipts/enriched", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	var got []EnrichedReceiptSummary
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Expected 3 receipts, got %d", len(got))
	}

	// Newest first: 1710, 1700, 1690.
	if got[0].Date != 1710000000 || got[1].Date != 1700000000 || got[2].Date != 1690000000 {
		t.Errorf("Expected newest-first sort, got dates: %d, %d, %d",
			got[0].Date, got[1].Date, got[2].Date)
	}
}

func TestListEnrichedReceiptsOwnerFilter(t *testing.T) {
	router, s := setupTestRouter(t)

	alice := &domain.User{UserID: s.UserID.Gen(), Username: "alice", Name: "Alice", PasswordHash: "x"}
	bob := &domain.User{UserID: s.UserID.Gen(), Username: "bob", Name: "Bob", PasswordHash: "x"}
	for _, u := range []*domain.User{alice, bob} {
		if err := s.CreateUser(u); err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	merchant := &domain.Merchant{MerchantID: s.MerchantID.Gen(), Name: "M"}
	if err := s.CreateMerchant(merchant); err != nil {
		t.Fatalf("create merchant: %v", err)
	}
	token := createTestSession(t, s, alice.UserID)

	// 2 receipts for Alice, 1 for Bob.
	for i := 0; i < 2; i++ {
		if err := s.CreateReceipt(&domain.Receipt{
			ReceiptID: s.ReceiptID.Gen(), MerchantID: merchant.MerchantID,
			OwnerID: alice.UserID, Date: 1700000000 + int64(i),
		}); err != nil {
			t.Fatalf("create receipt: %v", err)
		}
	}
	if err := s.CreateReceipt(&domain.Receipt{
		ReceiptID: s.ReceiptID.Gen(), MerchantID: merchant.MerchantID,
		OwnerID: bob.UserID, Date: 1700000000,
	}); err != nil {
		t.Fatalf("create receipt: %v", err)
	}

	url := "/api/receipts/enriched?owner=" + strconv.FormatUint(alice.UserID, 10)
	req := httptest.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	var got []EnrichedReceiptSummary
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("Expected 2 receipts (Alice's), got %d", len(got))
	}
	for _, r := range got {
		if r.OwnerID != alice.UserID {
			t.Errorf("Expected only Alice's receipts, got owner %d", r.OwnerID)
		}
	}
}

func TestListEnrichedReceiptsCategoryFilter(t *testing.T) {
	f := newEnrichedFixture(t)
	s := f.store

	// Add a second category and item NOT in that category, plus a receipt
	// for it. Then filter by the original category.
	otherCat := &domain.Category{CategoryID: s.CategoryID.Gen(), Name: "Dairy"}
	if err := s.CreateCategory(otherCat); err != nil {
		t.Fatalf("create other category: %v", err)
	}
	otherItem := &domain.Item{
		ItemID: s.ItemID.Gen(), Name: "Milk", CategoryID: otherCat.CategoryID,
		MerchantID: f.merchant.MerchantID, Normalized: "milk",
	}
	if err := s.CreateItem(otherItem); err != nil {
		t.Fatalf("create other item: %v", err)
	}
	if err := s.CreateReceipt(&domain.Receipt{
		ReceiptID: s.ReceiptID.Gen(), MerchantID: f.merchant.MerchantID,
		OwnerID: f.user.UserID, Date: 1700000000,
		Items: []domain.ReceiptItem{
			{ItemID: otherItem.ItemID, Quantity: 1, UnitPriceCents: 500},
		},
		TotalCents: 500,
	}); err != nil {
		t.Fatalf("create other receipt: %v", err)
	}

	url := "/api/receipts/enriched?category=" + strconv.FormatUint(f.category.CategoryID, 10)
	req := httptest.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+f.token)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	var got []EnrichedReceiptSummary
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Expected 1 receipt (Produce category), got %d", len(got))
	}
	if got[0].ReceiptID != f.receipt.ReceiptID {
		t.Errorf("Wrong receipt returned")
	}
}

func TestListEnrichedReceiptsMissingMerchantFallback(t *testing.T) {
	router, s := setupTestRouter(t)
	user := &domain.User{UserID: s.UserID.Gen(), Username: "u", Name: "U", PasswordHash: "x"}
	if err := s.CreateUser(user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := createTestSession(t, s, user.UserID)

	// Create a receipt whose merchant ID does not exist.
	bogusMerchantID := s.MerchantID.Gen() // gen but never created
	if err := s.CreateReceipt(&domain.Receipt{
		ReceiptID: s.ReceiptID.Gen(), MerchantID: bogusMerchantID,
		OwnerID: user.UserID, Date: 1700000000,
	}); err != nil {
		t.Fatalf("create receipt: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/receipts/enriched", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	var got []EnrichedReceiptSummary
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Expected 1 receipt, got %d", len(got))
	}
	if got[0].MerchantName != UnknownMerchant {
		t.Errorf("Expected MerchantName=%q, got %q", UnknownMerchant, got[0].MerchantName)
	}
	if got[0].OwnerName != "U" {
		t.Errorf("Expected OwnerName=%q, got %q", "U", got[0].OwnerName)
	}
}

// ---------------------------------------------------------------------------
// /api/receipts/{id}/enriched
// ---------------------------------------------------------------------------

func TestGetEnrichedReceiptRequiresAuth(t *testing.T) {
	router, _ := setupTestRouter(t)
	req := httptest.NewRequest("GET", "/api/receipts/1/enriched", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", w.Code)
	}
}

func TestGetEnrichedReceiptNotFound(t *testing.T) {
	router, s := setupTestRouter(t)
	user := &domain.User{UserID: s.UserID.Gen(), Username: "u", Name: "U", PasswordHash: "x"}
	if err := s.CreateUser(user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := createTestSession(t, s, user.UserID)

	req := httptest.NewRequest("GET", "/api/receipts/999999/enriched", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestGetEnrichedReceiptInvalidID(t *testing.T) {
	router, s := setupTestRouter(t)
	user := &domain.User{UserID: s.UserID.Gen(), Username: "u", Name: "U", PasswordHash: "x"}
	if err := s.CreateUser(user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := createTestSession(t, s, user.UserID)

	req := httptest.NewRequest("GET", "/api/receipts/notanumber/enriched", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

func TestGetEnrichedReceiptFullShape(t *testing.T) {
	f := newEnrichedFixture(t)

	url := "/api/receipts/" + strconv.FormatUint(f.receipt.ReceiptID, 10) + "/enriched"
	req := httptest.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+f.token)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var got EnrichedReceipt
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.ReceiptID != f.receipt.ReceiptID {
		t.Errorf("ReceiptID: got %d, want %d", got.ReceiptID, f.receipt.ReceiptID)
	}
	if got.MerchantName != "Whole Foods" {
		t.Errorf("MerchantName: got %q", got.MerchantName)
	}
	if got.OwnerName != "Alice" {
		t.Errorf("OwnerName: got %q", got.OwnerName)
	}
	if got.TotalCents != 666 {
		t.Errorf("TotalCents: got %d, want 666", got.TotalCents)
	}
	if len(got.Items) != 2 {
		t.Fatalf("Expected 2 items, got %d", len(got.Items))
	}

	// First item: 2 * 333 = 666.
	first := got.Items[0]
	if first.ItemID != f.item.ItemID {
		t.Errorf("ItemID mismatch")
	}
	if first.Name != "Bananas" {
		t.Errorf("Item name: got %q, want %q", first.Name, "Bananas")
	}
	if first.CategoryName != "Produce" {
		t.Errorf("CategoryName: got %q, want %q", first.CategoryName, "Produce")
	}
	if first.TotalPriceCents != 666 {
		t.Errorf("Item[0] total: got %d, want 666", first.TotalPriceCents)
	}

	// Second item: 0.5 * 333 = 166.5 → math.Round → 167.
	second := got.Items[1]
	if second.TotalPriceCents != 167 {
		t.Errorf("Item[1] total (0.5*333, rounded): got %d, want 167", second.TotalPriceCents)
	}
}

func TestGetEnrichedReceiptMissingCategoryFallback(t *testing.T) {
	router, s := setupTestRouter(t)
	user := &domain.User{UserID: s.UserID.Gen(), Username: "u", Name: "U", PasswordHash: "x"}
	if err := s.CreateUser(user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	merchant := &domain.Merchant{MerchantID: s.MerchantID.Gen(), Name: "M"}
	if err := s.CreateMerchant(merchant); err != nil {
		t.Fatalf("create merchant: %v", err)
	}
	// Create item, then delete its category. Category gen but not created.
	bogusCatID := s.CategoryID.Gen()
	item := &domain.Item{
		ItemID: s.ItemID.Gen(), Name: "Item",
		CategoryID: bogusCatID, MerchantID: merchant.MerchantID,
		Normalized: "item",
	}
	if err := s.CreateItem(item); err != nil {
		t.Fatalf("create item: %v", err)
	}
	receipt := &domain.Receipt{
		ReceiptID: s.ReceiptID.Gen(), MerchantID: merchant.MerchantID,
		OwnerID: user.UserID, Date: 1700000000,
		Items: []domain.ReceiptItem{
			{ItemID: item.ItemID, Quantity: 1, UnitPriceCents: 100},
		},
		TotalCents: 100,
	}
	if err := s.CreateReceipt(receipt); err != nil {
		t.Fatalf("create receipt: %v", err)
	}
	token := createTestSession(t, s, user.UserID)

	url := "/api/receipts/" + strconv.FormatUint(receipt.ReceiptID, 10) + "/enriched"
	req := httptest.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	var got EnrichedReceipt
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(got.Items))
	}
	if got.Items[0].CategoryName != UnknownCategory {
		t.Errorf("Expected CategoryName=%q, got %q", UnknownCategory, got.Items[0].CategoryName)
	}
}

func TestGetEnrichedReceiptMissingItemFallback(t *testing.T) {
	router, s := setupTestRouter(t)
	user := &domain.User{UserID: s.UserID.Gen(), Username: "u", Name: "U", PasswordHash: "x"}
	if err := s.CreateUser(user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	merchant := &domain.Merchant{MerchantID: s.MerchantID.Gen(), Name: "M"}
	if err := s.CreateMerchant(merchant); err != nil {
		t.Fatalf("create merchant: %v", err)
	}
	category := &domain.Category{CategoryID: s.CategoryID.Gen(), Name: "C"}
	if err := s.CreateCategory(category); err != nil {
		t.Fatalf("create category: %v", err)
	}
	bogusItemID := s.ItemID.Gen() // gen but never created
	receipt := &domain.Receipt{
		ReceiptID: s.ReceiptID.Gen(), MerchantID: merchant.MerchantID,
		OwnerID: user.UserID, Date: 1700000000,
		Items: []domain.ReceiptItem{
			{ItemID: bogusItemID, Quantity: 1, UnitPriceCents: 100},
		},
		TotalCents: 100,
	}
	if err := s.CreateReceipt(receipt); err != nil {
		t.Fatalf("create receipt: %v", err)
	}
	token := createTestSession(t, s, user.UserID)

	url := "/api/receipts/" + strconv.FormatUint(receipt.ReceiptID, 10) + "/enriched"
	req := httptest.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	var got EnrichedReceipt
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Items[0].Name != UnknownItem {
		t.Errorf("Expected item name=%q, got %q", UnknownItem, got.Items[0].Name)
	}
	if got.Items[0].CategoryName != UnknownCategory {
		t.Errorf("Expected category name=%q, got %q", UnknownCategory, got.Items[0].CategoryName)
	}
}

// ---------------------------------------------------------------------------
// helper-function tests
// ---------------------------------------------------------------------------

func TestEnrichReceiptsToSummaryEmpty(t *testing.T) {
	got := enrichReceiptsToSummary(nil, map[uint64]*domain.Merchant{}, map[uint64]*domain.User{})
	if len(got) != 0 {
		t.Errorf("Expected empty slice, got len=%d", len(got))
	}
	if got == nil {
		t.Errorf("Expected non-nil empty slice (for JSON `[]` contract)")
	}
}

func TestEnrichReceiptTotalPriceCentsRounding(t *testing.T) {
	// Direct test of the rounding behavior independent of HTTP.
	rcpt := &domain.Receipt{
		ReceiptID: 1, MerchantID: 1, OwnerID: 1, Date: 1700000000,
		Items: []domain.ReceiptItem{
			{ItemID: 1, Quantity: 0.5, UnitPriceCents: 333},   // 166.5 → 167
			{ItemID: 1, Quantity: 1.5, UnitPriceCents: 100},   // 150
			{ItemID: 1, Quantity: 0.333, UnitPriceCents: 100}, // 33.3 → 33
		},
	}
	enriched := enrichReceipt(rcpt,
		map[uint64]*domain.Merchant{},
		map[uint64]*domain.User{},
		map[uint64]*domain.Item{},
		map[uint64]*domain.Category{},
	)
	if len(enriched.Items) != 3 {
		t.Fatalf("Expected 3 items, got %d", len(enriched.Items))
	}
	if enriched.Items[0].TotalPriceCents != 167 {
		t.Errorf("0.5*333: got %d, want 167", enriched.Items[0].TotalPriceCents)
	}
	if enriched.Items[1].TotalPriceCents != 150 {
		t.Errorf("1.5*100: got %d, want 150", enriched.Items[1].TotalPriceCents)
	}
	if enriched.Items[2].TotalPriceCents != 33 {
		t.Errorf("0.333*100: got %d, want 33", enriched.Items[2].TotalPriceCents)
	}
}
