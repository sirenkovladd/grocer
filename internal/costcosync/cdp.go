// Package costcosync drives the user's logged-in Brave session over the
// Chrome DevTools Protocol to pull Costco warehouse receipts with no
// server running. This file is the minimal CDP client: the /json HTTP
// API plus page/browser WebSockets with incrementing numeric ids.
package costcosync

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Target is one entry of the /json/list remote-debugging API.
type Target struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// CDP is a connection to Brave's remote-debugging port.
type CDP struct {
	base    string
	http    *http.Client
	browser *wsSession
}

// Tab is one controllable page target.
type Tab struct {
	cdp *CDP
	// ID is the CDP target id (used for /json/close).
	ID     string
	target Target
	sess   *wsSession
}

// DialCDP connects to the remote-debugging HTTP API at baseURL
// (e.g. "http://localhost:9222") and opens the browser session.
func DialCDP(baseURL string) (*CDP, error) {
	c := &CDP{base: baseURL, http: &http.Client{Timeout: 30 * time.Second}}
	var ver struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := c.getJSON("/json/version", &ver); err != nil {
		return nil, fmt.Errorf("cdp version: %w", err)
	}
	if ver.WebSocketDebuggerURL == "" {
		return nil, fmt.Errorf("cdp version: no browser websocket url at %s", baseURL)
	}
	sess, err := dialSession(ver.WebSocketDebuggerURL)
	if err != nil {
		return nil, fmt.Errorf("cdp browser session: %w", err)
	}
	c.browser = sess
	return c, nil
}

// ListTargets returns the current debugger targets.
func (c *CDP) ListTargets() ([]Target, error) {
	var out []Target
	if err := c.getJSON("/json/list", &out); err != nil {
		return nil, fmt.Errorf("cdp list targets: %w", err)
	}
	return out, nil
}

// NewTab opens a blank background tab and attaches a session to it.
func (c *CDP) NewTab() (*Tab, error) {
	req, err := http.NewRequest("PUT", c.base+"/json/new?about:blank", nil)
	if err != nil {
		return nil, fmt.Errorf("cdp new tab: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cdp new tab: %w", err)
	}
	defer resp.Body.Close()
	var tgt Target
	if err := json.NewDecoder(resp.Body).Decode(&tgt); err != nil {
		return nil, fmt.Errorf("cdp new tab: %w", err)
	}
	sess, err := dialSession(tgt.WebSocketDebuggerURL)
	if err != nil {
		return nil, fmt.Errorf("cdp tab session: %w", err)
	}
	return &Tab{cdp: c, ID: tgt.ID, target: tgt, sess: sess}, nil
}

// ActivateTab brings the named target to the front (used to hand focus
// back to whatever the user was looking at).
func (c *CDP) ActivateTab(id string) error {
	_, err := c.browser.call("Target.activateTarget", map[string]any{"targetId": id})
	if err != nil {
		return fmt.Errorf("Target.activateTarget: %w", err)
	}
	return nil
}

// Close releases the browser session. Open tabs are left alone; call
// Tab.Close on tabs this client created.
func (c *CDP) Close() error {
	if c.browser != nil {
		return c.browser.close()
	}
	return nil
}

// Navigate loads url in the tab.
func (t *Tab) Navigate(url string) error {
	if _, err := t.sess.call("Page.enable", nil); err != nil {
		return fmt.Errorf("Page.enable: %w", err)
	}
	if _, err := t.sess.call("Page.navigate", map[string]any{"url": url}); err != nil {
		return fmt.Errorf("Page.navigate: %w", err)
	}
	return nil
}

// Evaluate runs expression in the tab and returns result.value: raw for
// strings, compact JSON otherwise. awaitPromise waits for promises
// (used for async fetch work).
func (t *Tab) Evaluate(expression string, awaitPromise bool) (string, error) {
	raw, err := t.sess.call("Runtime.evaluate", map[string]any{
		"expression":   expression,
		"returnByValue": true,
		"awaitPromise":  awaitPromise,
	})
	if err != nil {
		return "", fmt.Errorf("Runtime.evaluate: %w", err)
	}
	var env struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", fmt.Errorf("Runtime.evaluate: decode: %w", err)
	}
	var v any
	if err := json.Unmarshal(env.Result.Value, &v); err != nil {
		return "", fmt.Errorf("Runtime.evaluate: decode value: %w", err)
	}
	if s, ok := v.(string); ok {
		return s, nil
	}
	return string(env.Result.Value), nil
}

// Close closes the tab via the /json API and drops its session.
func (t *Tab) Close() error {
	resp, err := t.cdp.http.Get(t.cdp.base + "/json/close/" + t.ID)
	if err != nil {
		return fmt.Errorf("cdp close tab: %w", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return t.sess.close()
}

func (c *CDP) getJSON(path string, out any) error {
	resp, err := c.http.Get(c.base + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("GET %s: status %d: %.200s", path, resp.StatusCode, body)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// wsSession is one CDP WebSocket with id-dispatched calls.
type wsSession struct {
	conn    *websocket.Conn
	mu      sync.Mutex
	next    int
	pending map[int]chan wsReply
}

type wsReply struct {
	result json.RawMessage
	errMsg string
}

func dialSession(url string) (*wsSession, error) {
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, err
	}
	s := &wsSession{conn: conn, pending: make(map[int]chan wsReply)}
	go s.readLoop()
	return s, nil
}

func (s *wsSession) call(method string, params any) (json.RawMessage, error) {
	s.mu.Lock()
	s.next++
	id := s.next
	ch := make(chan wsReply, 1)
	s.pending[id] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
	}()

	env := map[string]any{"id": id, "method": method}
	if params != nil {
		env["params"] = params
	}
	if err := s.conn.WriteJSON(env); err != nil {
		return nil, err
	}
	reply := <-ch
	if reply.errMsg != "" {
		return nil, fmt.Errorf("%s", reply.errMsg)
	}
	return reply.result, nil
}

func (s *wsSession) readLoop() {
	for {
		var env struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := s.conn.ReadJSON(&env); err != nil {
			s.mu.Lock()
			for id, ch := range s.pending {
				ch <- wsReply{errMsg: "connection closed"}
				delete(s.pending, id)
			}
			s.mu.Unlock()
			return
		}
		s.mu.Lock()
		ch, ok := s.pending[env.ID]
		s.mu.Unlock()
		if !ok {
			continue
		}
		if env.Error != nil {
			ch <- wsReply{errMsg: env.Error.Message}
		} else {
			ch <- wsReply{result: env.Result}
		}
	}
}

func (s *wsSession) close() error {
	return s.conn.Close()
}
