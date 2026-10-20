package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"code.sirenko.ca/grocer/internal/domain"
	"code.sirenko.ca/grocer/internal/llm"
	"code.sirenko.ca/grocer/internal/receipt"
	"code.sirenko.ca/grocer/internal/store"
)

// stubProvider implements llm.Provider without network access. Categorize
// returns "Uncategorized" (category 0, not new) so imports fall back to the
// default category path.
type stubProvider struct{}

func (stubProvider) ParseReceipt(ctx context.Context, photo []byte) (*llm.ParsedReceipt, error) {
	return nil, nil
}
func (stubProvider) ParseReceiptStream(ctx context.Context, photo []byte) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}
func (stubProvider) ParseReceiptFromText(ctx context.Context, ocr *llm.OCRResult) (*llm.ParsedReceipt, error) {
	return nil, nil
}
func (stubProvider) ParseReceiptFromTextStream(ctx context.Context, ocr *llm.OCRResult) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}
func (stubProvider) CategorizeItem(ctx context.Context, itemName string, existing []domain.Category) (*llm.Categorization, error) {
	return &llm.Categorization{CategoryID: 0, IsNew: false}, nil
}

func setupCostcoRouter(t *testing.T) (*Router, *store.Store, *http.Cookie) {
	t.Helper()
	s, err := store.NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	parser := receipt.NewParser(s, stubProvider{})
	router := NewRouter(s, parser, nil, nil, nil)
	cookie := sessionForTest(t, router, "costco-user")
	// Seed one category so imports have a default.
	cat := &domain.Category{CategoryID: s.CategoryID.Gen(), Name: "Groceries"}
	if err := s.CreateCategory(cat); err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	return router, s, cookie
}

const costcoListJSON = `{"data":{"receiptsWithCounts":{"receipts":[{"warehouseName":"Burnaby","transactionDateTime":"08/15/2026 14:23:11","transactionBarcode":"22055220303942610191902","total":42.55,"totalItemCount":2}]}}}`

const costcoDetailJSON = `{"data":{"receiptsWithCounts":{"receipts":[{"warehouseName":"Burnaby","transactionDateTime":"08/15/2026 14:23:11","transactionDate":"08/15/2026","transactionBarcode":"22055220303942610191902","total":18.98,"subTotal":18.98,"taxes":0,"totalItemCount":2,"itemArray":[{"itemNumber":"1234567","itemDescription01":"KIRKLAND MILK 4L","unit":"1","amount":9.99,"itemUnitPriceAmount":9.99},{"itemNumber":"7654321","itemDescription01":"BUTTER 1KG","unit":"1","amount":8.99,"itemUnitPriceAmount":8.99}],"tenderArray":[{"tenderDescription":"VISA","amountTender":18.98}]}]}}}`

func doCostco(t *testing.T, router *Router, cookie *http.Cookie, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestCostcoPreviewList(t *testing.T) {
	router, _, cookie := setupCostcoRouter(t)
	w := doCostco(t, router, cookie, "/api/costco/preview-list",
		`{"content":`+strconvQuote(costcoListJSON)+`}`)
	if w.Code != http.StatusOK {
		t.Fatalf("preview-list: %d %s", w.Code, w.Body.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0]["barcode"] != "22055220303942610191902" {
		t.Errorf("barcode = %v", rows[0]["barcode"])
	}
	if rows[0]["alreadyImported"] == true {
		t.Errorf("should not be imported yet")
	}
}

func TestCostcoImportProposalIdempotent(t *testing.T) {
	router, s, cookie := setupCostcoRouter(t)

	// Owner = the session user.
	var ownerID uint64
	users, _ := s.ListUsers()
	if len(users) == 0 {
		t.Fatal("no users")
	}
	ownerID = users[0].UserID

	importBody, _ := json.Marshal(map[string]any{
		"ownerId": ownerID,
		"details": json.RawMessage(strconvQuote(costcoDetailJSON)),
		"mode":    "proposal",
	})
	w := doCostco(t, router, cookie, "/api/costco/import", string(importBody))
	if w.Code != http.StatusOK {
		t.Fatalf("import: %d %s", w.Code, w.Body.String())
	}
	var res struct {
		Created []map[string]string `json:"created"`
		Skipped []string            `json:"skipped"`
		Failed  []map[string]string `json:"failed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Created) != 1 {
		t.Fatalf("want 1 created, got %+v", res)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("unexpected failures: %+v", res.Failed)
	}

	// Second import of the same barcode must skip, not duplicate.
	w2 := doCostco(t, router, cookie, "/api/costco/import", string(importBody))
	var res2 struct {
		Created []map[string]string `json:"created"`
		Skipped []string            `json:"skipped"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &res2); err != nil {
		t.Fatalf("decode2: %v", err)
	}
	if len(res2.Created) != 0 {
		t.Errorf("re-import should create 0, got %d", len(res2.Created))
	}
	if len(res2.Skipped) != 1 {
		t.Errorf("re-import should skip 1, got %+v", res2)
	}

	// Receipts: still zero (proposal mode doesn't commit).
	receipts, _ := s.ListReceipts()
	if len(receipts) != 0 {
		t.Errorf("proposal mode should not create receipts, got %d", len(receipts))
	}
	proposals, _ := s.ListProposals()
	if len(proposals) != 1 {
		t.Fatalf("want 1 proposal, got %d", len(proposals))
	}
	if proposals[0].ExternalID != "costco:22055220303942610191902" {
		t.Errorf("externalID = %q", proposals[0].ExternalID)
	}
	if len(proposals[0].Items) != 2 {
		t.Fatalf("want 2 items, got %d", len(proposals[0].Items))
	}
}

func TestCostcoImportReceiptDirect(t *testing.T) {
	router, s, cookie := setupCostcoRouter(t)
	users, _ := s.ListUsers()
	ownerID := users[0].UserID

	importBody, _ := json.Marshal(map[string]any{
		"ownerId": ownerID,
		"details": json.RawMessage(strconvQuote(costcoDetailJSON)),
		"mode":    "receipt",
	})
	w := doCostco(t, router, cookie, "/api/costco/import", string(importBody))
	if w.Code != http.StatusOK {
		t.Fatalf("import receipt: %d %s", w.Code, w.Body.String())
	}
	receipts, _ := s.ListReceipts()
	if len(receipts) != 1 {
		t.Fatalf("want 1 receipt, got %d", len(receipts))
	}
	if receipts[0].ExternalID != "costco:22055220303942610191902" {
		t.Errorf("externalID = %q", receipts[0].ExternalID)
	}
	if len(receipts[0].Items) != 2 {
		t.Errorf("want 2 line items, got %d", len(receipts[0].Items))
	}
	items, _ := s.ListItems()
	if len(items) != 2 {
		t.Errorf("want 2 catalog items, got %d", len(items))
	}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
