package costcosync

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

// stubCDP is an in-process fake of Brave's remote-debugging endpoints:
// the /json/* HTTP API plus page/browser WebSockets speaking CDP.
type stubCDP struct {
	t   *testing.T
	srv *httptest.Server

	mu          sync.Mutex
	activateIDs []string
	evalAwait   []bool
	evalExprs   []string
}

func newStubCDP(t *testing.T) *stubCDP {
	t.Helper()
	s := &stubCDP{t: t}
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
		if r.Method != "PUT" {
			http.Error(w, "want PUT", http.StatusMethodNotAllowed)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{
			"id":                   "T9",
			"webSocketDebuggerUrl": "ws://" + r.Host + "/devtools/page/T9",
		})
	})
	mux.HandleFunc("/json/close/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/devtools/browser/", s.serveBrowserWS)
	mux.HandleFunc("/devtools/page/", s.servePageWS)
	s.srv = httptest.NewServer(mux)
	return s
}

func (s *stubCDP) baseURL() string { return s.srv.URL }

func (s *stubCDP) close() { s.srv.Close() }

type cdpMsg struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

func (s *stubCDP) serveBrowserWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Upgrade(w, r, nil, 1024, 1024)
	if err != nil {
		s.t.Logf("browser ws upgrade: %v", err)
		return
	}
	defer conn.Close()
	for {
		var m cdpMsg
		if err := conn.ReadJSON(&m); err != nil {
			return
		}
		if m.Method == "Target.activateTarget" {
			var p struct {
				TargetID string `json:"targetId"`
			}
			_ = json.Unmarshal(m.Params, &p)
			s.mu.Lock()
			s.activateIDs = append(s.activateIDs, p.TargetID)
			s.mu.Unlock()
		}
		_ = conn.WriteJSON(map[string]any{"id": m.ID, "result": map[string]any{}})
	}
}

func (s *stubCDP) servePageWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Upgrade(w, r, nil, 1024, 1024)
	if err != nil {
		s.t.Logf("page ws upgrade: %v", err)
		return
	}
	defer conn.Close()
	for {
		var m cdpMsg
		if err := conn.ReadJSON(&m); err != nil {
			return
		}
		switch m.Method {
		case "Page.enable":
			_ = conn.WriteJSON(map[string]any{"id": m.ID, "result": map[string]any{}})
		case "Page.navigate":
			_ = conn.WriteJSON(map[string]any{"id": m.ID, "result": map[string]any{"frameId": "F1"}})
		case "Runtime.evaluate":
			var p struct {
				Expression   string `json:"expression"`
				AwaitPromise bool   `json:"awaitPromise"`
			}
			_ = json.Unmarshal(m.Params, &p)
			s.mu.Lock()
			s.evalAwait = append(s.evalAwait, p.AwaitPromise)
			s.evalExprs = append(s.evalExprs, p.Expression)
			s.mu.Unlock()
			_ = conn.WriteJSON(map[string]any{"id": m.ID, "result": map[string]any{
				"result": map[string]any{"type": "string", "value": "echo:" + p.Expression},
			}})
		default:
			_ = conn.WriteJSON(map[string]any{"id": m.ID, "error": map[string]string{"message": "unknown " + m.Method}})
		}
	}
}

func TestTabLifecycle(t *testing.T) {
	stub := newStubCDP(t)
	defer stub.close()

	cdp, err := DialCDP(stub.baseURL())
	if err != nil {
		t.Fatalf("DialCDP: %v", err)
	}
	defer cdp.Close()

	targets, err := cdp.ListTargets()
	if err != nil {
		t.Fatalf("ListTargets: %v", err)
	}
	if len(targets) != 1 || targets[0].ID != "P1" {
		t.Fatalf("targets = %+v", targets)
	}

	tab, err := cdp.NewTab()
	if err != nil {
		t.Fatalf("NewTab: %v", err)
	}
	if tab.ID != "T9" {
		t.Fatalf("tab ID = %q", tab.ID)
	}

	if err := tab.Navigate("http://example.com/"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	val, err := tab.Evaluate("1+1", true)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if val != "echo:1+1" {
		t.Fatalf("Evaluate = %q", val)
	}
	stub.mu.Lock()
	awaited := len(stub.evalAwait) == 1 && stub.evalAwait[0]
	stub.mu.Unlock()
	if !awaited {
		t.Fatalf("awaitPromise was not forwarded")
	}

	if err := cdp.ActivateTab("P1"); err != nil {
		t.Fatalf("ActivateTab: %v", err)
	}
	stub.mu.Lock()
	gotActivate := len(stub.activateIDs) == 1 && stub.activateIDs[0] == "P1"
	stub.mu.Unlock()
	if !gotActivate {
		t.Fatalf("activateTarget params = %v", stub.activateIDs)
	}

	if err := tab.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestDialRefused(t *testing.T) {
	_, err := DialCDP("http://127.0.0.1:1")
	if err == nil {
		t.Fatalf("expected error dialing closed port")
	}
	if !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Fatalf("error should name the address, got: %v", err)
	}
}
