package costcosync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"code.sirenko.ca/grocer/internal/domain"
	"code.sirenko.ca/grocer/internal/llm"
	"code.sirenko.ca/grocer/internal/receipt"
	"code.sirenko.ca/grocer/internal/store"
	"github.com/gorilla/websocket"
)

// stubProvider mirrors internal/api/costco_test.go: no network, new items
// fall back to the default category path.
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

const (
	syncBarcodeA = "22055220303702610291741"
	syncBarcodeB = "22055220303942610191902"
)

const syncListJSON = `{"data":{"receiptsWithCounts":{"receipts":[{"warehouseName":"VANCOUVER BC","transactionDateTime":"2026-10-29T17:41:00","transactionBarcode":"` + syncBarcodeA + `","total":28.49,"totalItemCount":1},{"warehouseName":"VANCOUVER BC","transactionDateTime":"2026-10-23T19:24:00","transactionBarcode":"` + syncBarcodeB + `","total":9.99,"totalItemCount":1}]}}}`

const syncDetailA = `{"data":{"receiptsWithCounts":{"receipts":[{"warehouseName":"VANCOUVER BC","transactionDateTime":"2026-10-29T17:41:00","transactionBarcode":"` + syncBarcodeA + `","total":28.49,"subTotal":28.49,"taxes":0,"totalItemCount":1,"itemArray":[{"itemNumber":"1377112","itemDescription01":"NAT PLUS 2%","unit":1,"amount":28.49,"itemUnitPriceAmount":28.49}]}]}}}`

const syncDetailB = `{"data":{"receiptsWithCounts":{"receipts":[{"warehouseName":"VANCOUVER BC","transactionDateTime":"2026-10-23T19:24:00","transactionBarcode":"` + syncBarcodeB + `","total":9.99,"subTotal":9.99,"taxes":0,"totalItemCount":1,"itemArray":[{"itemNumber":"1234567","itemDescription01":"KIRKLAND MILK 4L","unit":"1","amount":9.99,"itemUnitPriceAmount":9.99}]}]}}}`

// scriptedCDP fakes Brave at the CDP layer: the test controls the login
// token, the list response, and per-barcode detail responses. Dispatch is
// by expression content: sessionStorage → token lookup, startDate → list
// query, barcode → detail query.
type scriptedCDP struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	token   string
	list    string
	details map[string]string
}

func newScriptedCDP(t *testing.T, token, list string, details map[string]string) *scriptedCDP {
	t.Helper()
	s := &scriptedCDP{t: t, token: token, list: list, details: details}
	mux := http.NewServeMux()
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"webSocketDebuggerUrl": "ws://" + r.Host + "/devtools/browser/B1",
		})
	})
	mux.HandleFunc("/json/list", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]string{{
			"id":                   "P1",
			"type":                 "page",
			"url":                  "http://example.com/",
			"webSocketDebuggerUrl": "ws://" + r.Host + "/devtools/page/P1",
		}})
	})
	mux.HandleFunc("/json/new", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"id":                   "T9",
			"webSocketDebuggerUrl": "ws://" + r.Host + "/devtools/page/T9",
		})
	})
	mux.HandleFunc("/json/close/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/devtools/browser/", func(w http.ResponseWriter, r *http.Request) {
		s.serveWS(w, r, func(m cdpMsg) any { return map[string]any{} })
	})
	mux.HandleFunc("/devtools/page/", func(w http.ResponseWriter, r *http.Request) {
		s.serveWS(w, r, s.respond)
	})
	s.srv = httptest.NewServer(mux)
	return s
}

func (s *scriptedCDP) baseURL() string { return s.srv.URL }

func (s *scriptedCDP) close() { s.srv.Close() }

func (s *scriptedCDP) serveWS(w http.ResponseWriter, r *http.Request, respond func(m cdpMsg) any) {
	conn, err := websocket.Upgrade(w, r, nil, 1024, 1024)
	if err != nil {
		s.t.Logf("ws upgrade: %v", err)
		return
	}
	defer conn.Close()
	for {
		var m cdpMsg
		if err := conn.ReadJSON(&m); err != nil {
			return
		}
		_ = conn.WriteJSON(map[string]any{"id": m.ID, "result": respond(m)})
	}
}

func (s *scriptedCDP) respond(m cdpMsg) any {
	if m.Method != "Runtime.evaluate" {
		return map[string]any{}
	}
	var p struct {
		Expression string `json:"expression"`
	}
	_ = json.Unmarshal(m.Params, &p)
	s.mu.Lock()
	defer s.mu.Unlock()
	strVal := func(v string) any {
		return map[string]any{"result": map[string]any{"type": "string", "value": v}}
	}
	if strings.Contains(p.Expression, "sessionStorage") {
		if s.token == "" {
			return map[string]any{"result": map[string]any{"type": "object", "subtype": "null"}}
		}
		return strVal(s.token)
	}
	if strings.Contains(p.Expression, "startDate") {
		return strVal(s.list)
	}
	for bc, detail := range s.details {
		if strings.Contains(p.Expression, bc) {
			return strVal(detail)
		}
	}
	return strVal(`{"errors":[{"message":"unexpected evaluate in stub"}]}`)
}

func testStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	cat := &domain.Category{CategoryID: s.CategoryID.Gen(), Name: "Groceries"}
	if err := s.CreateCategory(cat); err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	return s
}

func testCfg(s *store.Store, cdp *CDP, dryRun bool) Cfg {
	return Cfg{
		CDP:      cdp,
		Store:    s,
		Parser:   receipt.NewParser(s, stubProvider{}),
		OwnerID:  1,
		Since:    "1/01/2025",
		Until:    "11/01/2026",
		Timezone: time.UTC,
		DryRun:   dryRun,
	}
}

func TestBuildListEval(t *testing.T) {
	js := BuildListEval("tok123", "1/01/2025", "11/01/2026")
	for _, want := range []string{
		"https://ecom-api.costco.com/ebusiness/order/v1/orders/graphql",
		"Bearer tok123",
		"receiptsWithCounts",
		"1/01/2025",
		"11/01/2026",
		"credentials",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("list eval missing %q", want)
		}
	}
}

func TestRunImports(t *testing.T) {
	s := testStore(t)
	stub := newScriptedCDP(t, "tok123", syncListJSON, map[string]string{syncBarcodeA: syncDetailA, syncBarcodeB: syncDetailB})
	defer stub.close()
	cdp, err := DialCDP(stub.baseURL())
	if err != nil {
		t.Fatalf("DialCDP: %v", err)
	}
	defer cdp.Close()

	sum, err := Run(context.Background(), testCfg(s, cdp, false))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum.Created != 2 || sum.Skipped != 0 || len(sum.Failed) != 0 {
		t.Fatalf("summary = %+v", sum)
	}
	for _, bc := range []string{syncBarcodeA, syncBarcodeB} {
		if _, err := s.FindReceiptByExternalID("costco:" + bc); err != nil {
			t.Errorf("receipt costco:%s missing: %v", bc, err)
		}
	}
}

func TestRunIdempotent(t *testing.T) {
	s := testStore(t)
	newScripted := func() (*scriptedCDP, *CDP) {
		stub := newScriptedCDP(t, "tok123", syncListJSON, map[string]string{syncBarcodeA: syncDetailA, syncBarcodeB: syncDetailB})
		cdp, err := DialCDP(stub.baseURL())
		if err != nil {
			t.Fatalf("DialCDP: %v", err)
		}
		return stub, cdp
	}
	stub1, cdp1 := newScripted()
	defer stub1.close()
	defer cdp1.Close()
	if _, err := Run(context.Background(), testCfg(s, cdp1, false)); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	stub2, cdp2 := newScripted()
	defer stub2.close()
	defer cdp2.Close()
	sum, err := Run(context.Background(), testCfg(s, cdp2, false))
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if sum.Created != 0 || sum.Skipped != 2 || len(sum.Failed) != 0 {
		t.Fatalf("second summary = %+v", sum)
	}
}

func TestRunPollTimeout(t *testing.T) {
	s := testStore(t)
	stub := newScriptedCDP(t, "", syncListJSON, nil)
	defer stub.close()
	cdp, err := DialCDP(stub.baseURL())
	if err != nil {
		t.Fatalf("DialCDP: %v", err)
	}
	defer cdp.Close()

	_, err = Run(context.Background(), testCfg(s, cdp, false))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "costco") {
		t.Fatalf("expected Costco login error, got: %v", err)
	}
	if _, findErr := s.FindReceiptByExternalID("costco:" + syncBarcodeA); findErr == nil {
		t.Fatalf("nothing should be imported on login timeout")
	}
}

func TestRunErrorEnvelope(t *testing.T) {
	s := testStore(t)
	stub := newScriptedCDP(t, "tok123", `{"errors":[{"message":"denied"}]}`, nil)
	defer stub.close()
	cdp, err := DialCDP(stub.baseURL())
	if err != nil {
		t.Fatalf("DialCDP: %v", err)
	}
	defer cdp.Close()

	_, err = Run(context.Background(), testCfg(s, cdp, false))
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("expected denied error, got: %v", err)
	}
	if _, findErr := s.FindReceiptByExternalID("costco:" + syncBarcodeA); findErr == nil {
		t.Fatalf("nothing should be imported on error envelope")
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	s := testStore(t)
	stub := newScriptedCDP(t, "tok123", syncListJSON, map[string]string{syncBarcodeA: syncDetailA, syncBarcodeB: syncDetailB})
	defer stub.close()
	cdp, err := DialCDP(stub.baseURL())
	if err != nil {
		t.Fatalf("DialCDP: %v", err)
	}
	defer cdp.Close()

	sum, err := Run(context.Background(), testCfg(s, cdp, true))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum.Created != 2 {
		t.Fatalf("dry-run summary = %+v", sum)
	}
	if _, findErr := s.FindReceiptByExternalID("costco:" + syncBarcodeA); findErr == nil {
		t.Fatalf("dry run must not write receipts")
	}
}

func TestShouldPush(t *testing.T) {
	if ShouldPush(0) {
		t.Errorf("ShouldPush(0) = true")
	}
	if !ShouldPush(2) {
		t.Errorf("ShouldPush(2) = false")
	}
}
