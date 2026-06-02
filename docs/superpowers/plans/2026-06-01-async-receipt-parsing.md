# Async Receipt Parsing — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Decouple receipt upload from parsing — upload is instant, parsing runs as a background goroutine, users watch live streaming progress on the proposal detail page.

**Architecture:** Upload endpoint creates a proposal with `status=parsing` and spawns a background goroutine. An in-memory event hub broadcasts parse events to connected SSE clients. The proposal detail page opens an SSE connection and renders items as they arrive.

**Tech Stack:** Go (net/http SSE, goroutines), VanJS (EventSource, reactive state)

---

## File Map

| File | Action | Purpose |
|---|---|---|
| `internal/events/hub.go` | **Create** | In-memory pub/sub for parse events |
| `internal/store/memdb.go` | Modify | Add `UpdateProposalStatus`, `AppendProposalItem`, `ResetProposalForReparse` |
| `internal/receipt/parser.go` | Modify | Add `ParseReceiptAsync`, refactor shared logic |
| `internal/api/receipts.go` | Modify | Simplify upload handler — create proposal, spawn goroutine, return ID |
| `internal/api/proposals.go` | Modify | Add SSE stream endpoint, reparse endpoint, update list to show all statuses |
| `internal/api/router.go` | Modify | Register new routes, remove old `/upload/stream` |
| `client/pages/upload.ts` | Modify | Simplify — upload photo, navigate to proposal |
| `client/pages/proposal.ts` | Modify | Add SSE consumer, parsing/pending/failed state machine |
| `client/pages/proposals.ts` | Modify | Show all statuses with badges |

---

### Task 1: Event Hub

**Files:**
- Create: `internal/events/hub.go`

- [ ] **Step 1: Create the event hub**

```go
// internal/events/hub.go
package events

import (
	"sync"

	"code.sirenko.ca/grocer/internal/receipt"
)

// Hub manages SSE subscribers per proposal.
type Hub struct {
	mu          sync.RWMutex
	subscribers map[uint64][]chan receipt.ParseEvent
}

func NewHub() *Hub {
	return &Hub{
		subscribers: make(map[uint64][]chan receipt.ParseEvent),
	}
}

// Subscribe returns a channel that receives events for the given proposal.
func (h *Hub) Subscribe(proposalID uint64) <-chan receipt.ParseEvent {
	ch := make(chan receipt.ParseEvent, 32)
	h.mu.Lock()
	h.subscribers[proposalID] = append(h.subscribers[proposalID], ch)
	h.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber channel.
func (h *Hub) Unsubscribe(proposalID uint64, ch <-chan receipt.ParseEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	subs := h.subscribers[proposalID]
	for i, s := range subs {
		// Compare the underlying channel by converting
		if s == ch {
			h.subscribers[proposalID] = append(subs[:i], subs[i+1:]...)
			close(s)
			break
		}
	}
	if len(h.subscribers[proposalID]) == 0 {
		delete(h.subscribers, proposalID)
	}
}

// Publish sends an event to all subscribers of a proposal.
func (h *Hub) Publish(proposalID uint64, event receipt.ParseEvent) {
	h.mu.RLock()
	subs := h.subscribers[proposalID]
	h.mu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- event:
		default:
			// Skip slow subscribers
		}
	}
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/events/`
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add internal/events/hub.go
git commit -m "feat: add in-memory event hub for parse streaming"
```

---

### Task 2: Store Methods for Async Parsing

**Files:**
- Modify: `internal/store/memdb.go`

- [ ] **Step 1: Add new store methods**

Add these methods at the end of `internal/store/memdb.go`, before the `SaveSnapshotAsync` function:

```go
// UpdateProposalStatus changes a proposal's status.
func (s *Store) UpdateProposalStatus(id uint64, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	raw, err := txn.First("proposals", "id", id)
	if err != nil {
		return err
	}
	if raw == nil {
		return ErrNotFound
	}

	p := raw.(*domain.Proposal)
	p.Status = status

	txn2 := s.db.Txn(true)
	defer txn2.Abort()
	if err := txn2.Insert("proposals", p); err != nil {
		return err
	}
	txn2.Commit()
	s.SaveSnapshotAsync(context.Background())
	return nil
}

// AppendProposalItem adds an item to a proposal during streaming parse.
func (s *Store) AppendProposalItem(id uint64, item domain.ProposalItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	raw, err := txn.First("proposals", "id", id)
	if err != nil {
		return err
	}
	if raw == nil {
		return ErrNotFound
	}

	p := raw.(*domain.Proposal)
	p.Items = append(p.Items, item)

	txn2 := s.db.Txn(true)
	defer txn2.Abort()
	if err := txn2.Insert("proposals", p); err != nil {
		return err
	}
	txn2.Commit()
	s.SaveSnapshotAsync(context.Background())
	return nil
}

// ResetProposalForReparse clears items and resets status to "parsing" for retry.
func (s *Store) ResetProposalForReparse(id uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	raw, err := txn.First("proposals", "id", id)
	if err != nil {
		return err
	}
	if raw == nil {
		return ErrNotFound
	}

	p := raw.(*domain.Proposal)
	p.Status = "parsing"
	p.Items = nil
	p.TotalCents = 0
	p.MerchantID = 0
	p.Merchant = ""
	p.Date = 0

	txn2 := s.db.Txn(true)
	defer txn2.Abort()
	if err := txn2.Insert("proposals", p); err != nil {
		return err
	}
	txn2.Commit()
	s.SaveSnapshotAsync(context.Background())
	return nil
}

// UpdateProposalParseResult sets merchant, date, total, and final items after parse completes.
func (s *Store) UpdateProposalParseResult(id uint64, merchantID uint64, merchant string, date int64, totalCents int64, items []domain.ProposalItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	raw, err := txn.First("proposals", "id", id)
	if err != nil {
		return err
	}
	if raw == nil {
		return ErrNotFound
	}

	p := raw.(*domain.Proposal)
	p.MerchantID = merchantID
	p.Merchant = merchant
	p.Date = date
	p.TotalCents = totalCents
	p.Items = items
	p.Status = "pending"

	txn2 := s.db.Txn(true)
	defer txn2.Abort()
	if err := txn2.Insert("proposals", p); err != nil {
		return err
	}
	txn2.Commit()
	s.SaveSnapshotAsync(context.Background())
	return nil
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/store/`
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add internal/store/memdb.go
git commit -m "feat: add store methods for async proposal parsing"
```

---

### Task 3: Parser — Async Parse Method

**Files:**
- Modify: `internal/receipt/parser.go`

- [ ] **Step 1: Add ParseReceiptAsync method and EventHub dependency**

Add an `EventHub` field to the `Parser` struct and a `SetEventHub` method. Then add the `ParseReceiptAsync` method.

In `internal/receipt/parser.go`, update the `Parser` struct:

```go
type Parser struct {
	store    *store.Store
	llm      llm.Provider
	matcher  *Matcher
	eventHub EventPublisher
}

// EventPublisher is a minimal interface to avoid import cycle with events package.
type EventPublisher interface {
	Publish(proposalID uint64, event ParseEvent)
}

func NewParser(store *store.Store, llmProvider llm.Provider) *Parser {
	return &Parser{
		store:   store,
		llm:     llmProvider,
		matcher: NewMatcher(store),
	}
}

func (p *Parser) SetEventHub(hub EventPublisher) {
	p.eventHub = hub
}
```

Then add the `ParseReceiptAsync` method after `ParseReceiptStream`:

```go
// ParseReceiptAsync runs the full parse pipeline in the background,
// updating the proposal in the store and broadcasting events via the hub.
func (p *Parser) ParseReceiptAsync(ctx context.Context, proposalID uint64, photo []byte, ownerID uint64) {
	log.Printf("PARSE_ASYNC: starting for proposal %d", proposalID)

	publish := func(event ParseEvent) {
		if p.eventHub != nil {
			p.eventHub.Publish(proposalID, event)
		}
	}

	fail := func(msg string) {
		log.Printf("PARSE_ASYNC: failed proposal %d: %s", proposalID, msg)
		_ = p.store.UpdateProposalStatus(proposalID, "failed")
		publish(ParseEvent{Type: "error", Message: msg})
	}

	// Stream from LLM
	streamCh, err := p.llm.ParseReceiptStream(ctx, photo)
	if err != nil {
		fail(fmt.Sprintf("LLM error: %v", err))
		return
	}

	publish(ParseEvent{Type: "progress", Message: "Parsing receipt with AI..."})

	var accumulated string
	var lastItemCount int
	chunkCount := 0

	for chunk := range streamCh {
		if chunk.Error != nil {
			fail(fmt.Sprintf("LLM stream error: %v", chunk.Error))
			return
		}
		chunkCount++
		accumulated += chunk.Text

		// Try partial parse for progressive items
		var partial struct {
			Items []struct {
				Name       string  `json:"name"`
				Quantity   uint32  `json:"quantity"`
				UnitPrice  float64 `json:"unit_price"`
				TotalPrice float64 `json:"total_price"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(accumulated), &partial); err == nil && len(partial.Items) > lastItemCount {
			for i := lastItemCount; i < len(partial.Items); i++ {
				it := partial.Items[i]
				pi := domain.ProposalItem{
					ParsedName:     it.Name,
					Quantity:       it.Quantity,
					UnitPriceCents: dollarsToCents(it.UnitPrice),
				}
				_ = p.store.AppendProposalItem(proposalID, pi)
				publish(ParseEvent{Type: "item", Index: i, Item: &pi})
			}
			lastItemCount = len(partial.Items)
			publish(ParseEvent{Type: "progress", Message: fmt.Sprintf("Found %d items...", lastItemCount)})
		}
	}

	// Final parse
	parsed, err := llm.ParseReceiptResponse(accumulated)
	if err != nil {
		fail(fmt.Sprintf("parse response: %v", err))
		return
	}

	publish(ParseEvent{Type: "progress", Message: "Matching items to catalog..."})

	merchant, err := p.findOrCreateMerchant(parsed.Merchant)
	if err != nil {
		fail(fmt.Sprintf("merchant: %v", err))
		return
	}

	// Build final items with matching
	proposalItems := make([]domain.ProposalItem, len(parsed.Items))
	for i, item := range parsed.Items {
		matched, confidence, err := p.matcher.FindMatch(item.Name)
		if err != nil {
			fail(fmt.Sprintf("match: %v", err))
			return
		}

		pi := domain.ProposalItem{
			ParsedName:     item.Name,
			Quantity:       item.Quantity,
			UnitPriceCents: dollarsToCents(item.UnitPrice),
			Confidence:     confidence,
		}

		if matched != nil && confidence >= 0.99 {
			pi.MatchedItemID = matched.ItemID
			pi.CategoryID = matched.CategoryID
			pi.UserChoice = "existing"
		} else if matched != nil && confidence > 0.80 {
			pi.MatchedItemID = matched.ItemID
			pi.CategoryID = matched.CategoryID
		} else {
			cat, err := p.categorizeItem(ctx, item.Name)
			if err == nil {
				pi.CategoryID = cat.CategoryID
				pi.IsNewCategory = cat.IsNew
			}
		}

		proposalItems[i] = pi
	}

	// Update proposal with final data
	if err := p.store.UpdateProposalParseResult(proposalID, merchant.MerchantID, merchant.Name, parsed.Date.Unix(), dollarsToCents(parsed.Total), proposalItems); err != nil {
		fail(fmt.Sprintf("save result: %v", err))
		return
	}

	// Build full proposal for the done event
	proposal := &domain.Proposal{
		ProposalID: proposalID,
		OwnerID:    ownerID,
		MerchantID: merchant.MerchantID,
		Merchant:   merchant.Name,
		Date:       parsed.Date.Unix(),
		Items:      proposalItems,
		TotalCents: dollarsToCents(parsed.Total),
		Status:     "pending",
	}

	publish(ParseEvent{Type: "done", Proposal: proposal})
	log.Printf("PARSE_ASYNC: completed proposal %d", proposalID)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/receipt/`
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add internal/receipt/parser.go
git commit -m "feat: add ParseReceiptAsync for background parsing"
```

---

### Task 4: Simplify Upload Handler

**Files:**
- Modify: `internal/api/receipts.go`

- [ ] **Step 1: Replace handleUploadReceipt**

Replace the existing `handleUploadReceipt` function with the lightweight version that creates a proposal with `status=parsing` and spawns the async parse:

```go
func (r *Router) handleUploadReceipt(w http.ResponseWriter, req *http.Request) {
	userID := r.getUserID(req)

	if err := req.ParseMultipartForm(10 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "file too large")
		return
	}

	file, _, err := req.FormFile("photo")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing photo")
		return
	}
	defer file.Close()

	photoData, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read file")
		return
	}

	// Resize for LLM
	llmData := resizeImageForLLM(photoData)

	// Create proposal immediately with "parsing" status
	proposal := &domain.Proposal{
		ProposalID: r.store.ProposalID.Gen(),
		OwnerID:    userID,
		Status:     "parsing",
	}

	// Save photo if photo store is configured
	if r.photoStore != nil {
		photoURL, err := r.photoStore.Save(req.Context(), proposal.ProposalID, photoData)
		if err == nil {
			proposal.PhotoURL = photoURL
		}
	}

	if err := r.store.CreateProposal(proposal); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create proposal")
		return
	}

	// Spawn background parse goroutine
	go r.parser.ParseReceiptAsync(req.Context(), proposal.ProposalID, llmData, userID)

	writeJSON(w, http.StatusOK, map[string]uint64{"id": proposal.ProposalID})
}
```

- [ ] **Step 2: Remove handleUploadReceiptStream**

Delete the entire `handleUploadReceiptStream` function and the `handleSSEError` function from `internal/api/receipts.go`.

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/api/`
Expected: compilation error about unused imports — clean up any unused imports (like `receipt` package import if it was only used by the stream handler).

- [ ] **Step 4: Commit**

```bash
git add internal/api/receipts.go
git commit -m "feat: simplify upload handler to instant proposal + async parse"
```

---

### Task 5: SSE Stream & Reparse Endpoints

**Files:**
- Modify: `internal/api/proposals.go`

- [ ] **Step 1: Add SSE stream endpoint**

Add this function to `internal/api/proposals.go`:

```go
func (r *Router) handleProposalStream(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid proposal ID")
		return
	}

	proposal, err := r.store.GetProposal(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "proposal not found")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Del("Content-Length")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	writeSSE := func(event string, data interface{}) {
		jsonData, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, jsonData)
		flusher.Flush()
	}

	// Always send snapshot first
	writeSSE("snapshot", proposal)

	// If not parsing, we're done
	if proposal.Status != "parsing" {
		return
	}

	// Subscribe to live events
	ch := r.eventHub.Subscribe(id)
	defer r.eventHub.Unsubscribe(id, ch)

	ctx := req.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			writeSSE(event.Type, event)
			if event.Type == "done" || event.Type == "error" {
				return
			}
		}
	}
}
```

- [ ] **Step 2: Add reparse endpoint**

```go
func (r *Router) handleReparseProposal(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid proposal ID")
		return
	}

	proposal, err := r.store.GetProposal(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "proposal not found")
		return
	}

	if proposal.Status != "failed" {
		writeError(w, http.StatusBadRequest, "proposal is not in failed state")
		return
	}

	// Reset proposal for reparse
	if err := r.store.ResetProposalForReparse(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reset proposal")
		return
	}

	// Get photo data for re-parsing
	// We need to re-read the photo. The photo is stored via photoStore.
	// For now, we'll use a placeholder approach - the photo URL is on the proposal.
	// The actual photo bytes need to be retrieved from photoStore.
	if r.photoStore == nil {
		writeError(w, http.StatusInternalServerError, "photo store not configured")
		return
	}

	photoData, err := r.photoStore.Get(req.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retrieve photo")
		return
	}

	llmData := resizeImageForLLM(photoData)
	go r.parser.ParseReceiptAsync(req.Context(), id, llmData, proposal.OwnerID)

	writeJSON(w, http.StatusOK, map[string]uint64{"id": id})
}
```

- [ ] **Step 3: Update handleListProposals to show all statuses**

Replace the existing `handleListProposals` function:

```go
func (r *Router) handleListProposals(w http.ResponseWriter, req *http.Request) {
	proposals, err := r.store.ListProposals()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Filter out approved proposals (they became receipts)
	var active []*domain.Proposal
	for _, p := range proposals {
		if p.Status != "approved" {
			active = append(active, p)
		}
	}

	writeJSON(w, http.StatusOK, active)
}
```

- [ ] **Step 4: Verify it compiles**

Run: `go build ./internal/api/`
Expected: compilation errors likely — need to add `eventHub` field to `Router` and update constructor. Fix in next task.

- [ ] **Step 5: Commit**

```bash
git add internal/api/proposals.go
git commit -m "feat: add SSE stream endpoint and reparse endpoint"
```

---

### Task 6: Wire Event Hub into Router

**Files:**
- Modify: `internal/api/router.go`

- [ ] **Step 1: Add eventHub field to Router**

Update the `Router` struct:

```go
type Router struct {
	store      *store.Store
	parser     *receipt.Parser
	photoStore photo.Store
	photoCache *photo.LocalCache
	mux        *http.ServeMux
	eventHub   *events.Hub
}
```

Update the constructor:

```go
func NewRouter(store *store.Store, parser *receipt.Parser, photoStore photo.Store, photoCache *photo.LocalCache, eventHub *events.Hub) *Router {
	r := &Router{
		store:      store,
		parser:     parser,
		photoStore: photoStore,
		photoCache: photoCache,
		mux:        http.NewServeMux(),
		eventHub:   eventHub,
	}

	r.setupRoutes()
	return r
}
```

Add the import:

```go
"code.sirenko.ca/grocer/internal/events"
```

- [ ] **Step 2: Register new routes, remove old one**

In `setupRoutes()`, replace the receipt upload routes:

```go
r.mux.HandleFunc("POST /api/receipts/upload", r.withCORS(r.withAuth(r.withAuditLogging("upload", "receipt", r.handleUploadReceipt))))
// Remove: r.mux.HandleFunc("POST /api/receipts/upload/stream", ...)
```

Add the new proposal routes:

```go
r.mux.HandleFunc("GET /api/proposals/{id}/stream", r.withCORS(r.withAuth(r.handleProposalStream)))
r.mux.HandleFunc("POST /api/proposals/{id}/reparse", r.withCORS(r.withAuth(r.withAuditLogging("reparse", "proposal", r.handleReparseProposal))))
```

- [ ] **Step 3: Update cmd/server/main.go to wire event hub**

Read `cmd/server/main.go`, find where `NewRouter` is called, and update it to pass the event hub. Also wire the hub into the parser.

Find the relevant section and update:

```go
// Create event hub
eventHub := events.NewHub()

// Create parser and wire event hub
parser := receipt.NewParser(store, llmProvider)
parser.SetEventHub(eventHub)

// Create router with event hub
router := api.NewRouter(store, parser, photoStore, photoCache, eventHub)
```

Add the import for `"code.sirenko.ca/grocer/internal/events"`.

- [ ] **Step 4: Verify it compiles**

Run: `go build ./cmd/server/`
Expected: no output (success)

- [ ] **Step 5: Commit**

```bash
git add internal/api/router.go cmd/server/main.go
git commit -m "feat: wire event hub into router and parser"
```

---

### Task 7: Simplify Upload Page

**Files:**
- Modify: `client/pages/upload.ts`

- [ ] **Step 1: Rewrite upload page**

Replace the entire file content:

```typescript
import van from "vanjs-core"
import { api, navigate } from "../main"

const { div, h1, button, img, input, p } = van.tags

const UploadPage = () => {
  const photo = van.state<File | null>(null)
  const preview = van.state<string | null>(null)
  const uploading = van.state(false)
  const error = van.state("")

  const handleFileSelect = (e: Event) => {
    const input = e.target as HTMLInputElement
    if (input.files && input.files[0]) {
      photo.val = input.files[0]
      preview.val = URL.createObjectURL(photo.val)
      error.val = ""
    }
  }

  const handleDrop = (e: DragEvent) => {
    e.preventDefault()
    if (e.dataTransfer?.files && e.dataTransfer.files[0]) {
      photo.val = e.dataTransfer.files[0]
      preview.val = URL.createObjectURL(photo.val)
      error.val = ""
    }
  }

  const handleDragOver = (e: Event) => {
    e.preventDefault()
  }

  const handleSubmit = async (e: Event) => {
    e.preventDefault()
    if (!photo.val) {
      error.val = "Please select a photo"
      return
    }

    uploading.val = true
    error.val = ""

    try {
      const formData = new FormData()
      formData.append("photo", photo.val)

      const data = await api.postFormData("/receipts/upload", formData)
      navigate(`/proposals/${data.id}`)
    } catch (err) {
      error.val = err instanceof Error ? err.message : "Upload failed"
    } finally {
      uploading.val = false
    }
  }

  return div({ class: "upload-page" },
    div({ class: "page-header" },
      h1("Upload Receipt"),
      button({ onclick: () => navigate("/receipts") }, "Back"),
    ),
    div({ class: "upload-form" },
      div({
        class: "dropzone",
        ondrop: handleDrop,
        ondragover: handleDragOver,
        onclick: () => document.getElementById("file-input")?.click(),
      },
        () => preview.val
          ? img({ src: preview.val, class: "preview" })
          : div({ class: "dropzone-text" },
              p("Drag & drop receipt photo here"),
              p({ class: "dropzone-hint" }, "or click to select"),
            ),
      ),
      input({
        id: "file-input",
        type: "file",
        accept: "image/*",
        capture: "environment",
        style: "display: none",
        onchange: handleFileSelect,
      }),
      () => error.val ? p({ class: "error" }, error.val) : "",
      button({
        type: "button",
        disabled: uploading,
        class: "upload-btn",
        onclick: handleSubmit,
      }, uploading.val ? "Uploading..." : "Upload"),
    ),
  )
}

export default UploadPage
```

- [ ] **Step 2: Add postFormData to api helper**

Check `client/main.ts` for the `api` object. Add a `postFormData` method if it doesn't exist:

```typescript
async postFormData(path: string, formData: FormData): Promise<any> {
  const token = localStorage.getItem("token")
  const response = await fetch(`/api${path}`, {
    method: "POST",
    headers: {
      "Authorization": `Bearer ${token}`,
    },
    body: formData,
  })
  if (!response.ok) {
    const data = await response.json()
    throw new Error(data.error || "Request failed")
  }
  return response.json()
}
```

- [ ] **Step 3: Commit**

```bash
git add client/pages/upload.ts client/main.ts
git commit -m "feat: simplify upload page to instant upload + redirect"
```

---

### Task 8: Proposal Detail — SSE Consumer & State Machine

**Files:**
- Modify: `client/pages/proposal.ts`

- [ ] **Step 1: Rewrite proposal detail page**

Replace the entire file content:

```typescript
import van from "vanjs-core"
import { api, navigate } from "../main"

const { div, h1, h2, table, tr, td, th, button, select, option, img, p, span } = van.tags

interface ProposalItem {
  parsedName: string
  quantity: number
  unitPriceCents: number
  matchedItemId: number
  confidence: number
  categoryId: number
  isNewCategory: boolean
  userChoice: string
}

interface Proposal {
  proposalId: number
  ownerId: number
  merchant: string
  date: number
  photoUrl: string
  items: ProposalItem[]
  totalCents: number
  status: string
}

const ProposalDetailPage = () => {
  const proposal = van.state<Proposal | null>(null)
  const status = van.state<string>("loading") // loading, parsing, pending, failed, approved
  const streamingItems = van.state<ProposalItem[]>([])
  const progressMsg = van.state("")
  const choices = van.state<Record<number, string>>({})
  const approving = van.state(false)
  const error = van.state("")
  let eventSource: EventSource | null = null

  const id = window.location.hash.split("/").pop()

  const connectSSE = () => {
    if (!id) return

    const token = localStorage.getItem("token")
    eventSource = new EventSource(`/api/proposals/${id}/stream`, {
      headers: { "Authorization": `Bearer ${token}` },
    } as any)

    // EventSource doesn't support custom headers, so we need a workaround.
    // Use fetch-based SSE instead.
    eventSource?.close()
    eventSource = null

    fetchSSE()
  }

  const fetchSSE = async () => {
    if (!id) return

    const token = localStorage.getItem("token")
    try {
      const response = await fetch(`/api/proposals/${id}/stream`, {
        headers: { "Authorization": `Bearer ${token}` },
      })

      if (!response.ok) {
        throw new Error("Failed to connect")
      }

      const reader = response.body!.getReader()
      const decoder = new TextDecoder()
      let buffer = ""

      while (true) {
        const { done, value } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })
        const parts = buffer.split("\n\n")
        buffer = parts.pop()!

        for (const part of parts) {
          let eventType = ""
          let dataStr = ""
          for (const line of part.split("\n")) {
            if (line.startsWith("event: ")) {
              eventType = line.slice(7)
            } else if (line.startsWith("data: ")) {
              dataStr = line.slice(6)
            }
          }
          if (!eventType || !dataStr) continue

          try {
            const data = JSON.parse(dataStr)

            if (eventType === "snapshot") {
              proposal.val = data
              status.val = data.status
              if (data.items) {
                streamingItems.val = data.items
              }
              if (data.status !== "parsing") {
                return // Done, no need to keep listening
              }
            } else if (eventType === "progress") {
              progressMsg.val = data.message || ""
            } else if (eventType === "item") {
              if (data.item) {
                streamingItems.val = [...streamingItems.val, data.item]
              }
            } else if (eventType === "done") {
              proposal.val = data.proposal
              status.val = "pending"
              streamingItems.val = data.proposal?.items || streamingItems.val
              return
            } else if (eventType === "error") {
              status.val = "failed"
              error.val = data.message || "Parse failed"
              return
            }
          } catch (parseErr) {
            console.warn("SSE parse error:", parseErr)
          }
        }
      }
    } catch (err) {
      error.val = err instanceof Error ? err.message : "Connection failed"
      status.val = "failed"
    }
  }

  connectSSE()

  const handleChoice = (index: number, choice: string) => {
    choices.val = { ...choices.val, [index]: choice }
  }

  const handleApprove = async () => {
    if (!proposal.val) return

    approving.val = true
    error.val = ""

    try {
      await api.post(`/proposals/${proposal.val.proposalId}/approve`, {
        choices: choices.val,
      })
      navigate("/receipts")
    } catch (err) {
      error.val = err instanceof Error ? err.message : "Approval failed"
    } finally {
      approving.val = false
    }
  }

  const handleRetry = async () => {
    if (!id) return
    error.val = ""
    status.val = "loading"
    streamingItems.val = []

    try {
      await api.post(`/proposals/${id}/reparse`, {})
      status.val = "parsing"
      fetchSSE()
    } catch (err) {
      error.val = err instanceof Error ? err.message : "Retry failed"
      status.val = "failed"
    }
  }

  // Parsing view — skeleton + streaming items
  const renderParsing = () => div({ class: "proposal-parsing" },
    div({ class: "page-header" },
      h1("Parsing Receipt..."),
      button({ onclick: () => navigate("/proposals") }, "Back"),
    ),
    div({ class: "parsing-progress" },
      div({ class: "skeleton-header" },
        div({ class: "skeleton-line skeleton-merchant" }),
        div({ class: "skeleton-line skeleton-date" }),
        div({ class: "skeleton-line skeleton-total" }),
      ),
      progressMsg.val ? p({ class: "progress-msg" }, progressMsg.val) : "",
      () => streamingItems.val.length > 0
        ? div({ class: "streaming-items" },
            h2(`Items (${streamingItems.val.length})`),
            table(
              tr(th("Item"), th("Qty"), th("Price")),
              ...streamingItems.val.map((it) =>
                tr(
                  td(it.parsedName),
                  td(it.quantity.toString()),
                  td(`$${(it.unitPriceCents / 100).toFixed(2)}`),
                )
              ),
            ),
          )
        : div({ class: "parsing-placeholder" },
            div({ class: "spinner" }),
            p("Waiting for items..."),
          ),
    ),
  )

  // Pending view — full review form
  const renderPending = () => {
    const p = proposal.val!
    return div({ class: "proposal-detail-page" },
      div({ class: "page-header" },
        h1(`Proposal from ${p.merchant}`),
        button({ onclick: () => navigate("/proposals") }, "Back"),
      ),
      div({ class: "proposal-layout" },
        div({ class: "proposal-photo" },
          p.photoUrl
            ? img({ src: `/api/photos/${p.proposalId}`, alt: "Receipt" })
            : p("No photo available"),
        ),
        div({ class: "proposal-items" },
          h2("Items"),
          table(
            tr(th("Item"), th("Qty"), th("Price"), th("Confidence"), th("Action")),
            ...streamingItems.val.map((item, index) =>
              tr(
                td(item.parsedName),
                td(item.quantity.toString()),
                td(`$${(item.unitPriceCents / 100).toFixed(2)}`),
                td(`${(item.confidence * 100).toFixed(0)}%`),
                td(
                  item.confidence >= 0.99
                    ? "Auto-matched"
                    : item.confidence > 0.80
                      ? select(
                          { onchange: (e: Event) => handleChoice(index, (e.target as HTMLSelectElement).value) },
                          option({ value: "" }, "Choose..."),
                          option({ value: "existing" }, "Use existing"),
                          option({ value: "new" }, "Create new"),
                        )
                      : "New item"
                ),
              )
            ),
          ),
          div({ class: "proposal-summary" },
            p(`Total: $${(p.totalCents / 100).toFixed(2)}`),
            p(`Date: ${new Date(p.date * 1000).toLocaleDateString()}`),
          ),
          () => error.val ? p({ class: "error" }, error.val) : "",
          button({
            onclick: handleApprove,
            disabled: approving,
            class: "approve-btn",
          }, approving.val ? "Approving..." : "Approve Receipt"),
        ),
      ),
    )
  }

  // Failed view
  const renderFailed = () => div({ class: "proposal-failed" },
    div({ class: "page-header" },
      h1("Parse Failed"),
      button({ onclick: () => navigate("/proposals") }, "Back"),
    ),
    div({ class: "failed-content" },
      p({ class: "error" }, error.val || "An error occurred while parsing the receipt"),
      proposal.val?.photoUrl
        ? div({ class: "proposal-photo" },
            img({ src: `/api/photos/${proposal.val.proposalId}`, alt: "Receipt" }),
          )
        : "",
      button({
        onclick: handleRetry,
        class: "retry-btn",
      }, "Retry Parsing"),
    ),
  )

  return div({ class: "proposal-detail-wrapper" },
    () => {
      switch (status.val) {
        case "loading":
          return div("Loading...")
        case "parsing":
          return renderParsing()
        case "pending":
          return renderPending()
        case "failed":
          return renderFailed()
        case "approved":
          return div(
            p("This proposal has been approved."),
            button({ onclick: () => navigate("/receipts") }, "View Receipts"),
          )
        default:
          return div("Unknown state")
      }
    },
  )
}

export default ProposalDetailPage
```

Note: `EventSource` doesn't support custom headers, so we use `fetch` with streaming body instead — same pattern as the old upload page.

- [ ] **Step 2: Commit**

```bash
git add client/pages/proposal.ts
git commit -m "feat: add SSE consumer and state machine to proposal detail"
```

---

### Task 9: Proposals List — Show All Statuses

**Files:**
- Modify: `client/pages/proposals.ts`

- [ ] **Step 1: Update proposals page to show all statuses**

Replace the entire file content:

```typescript
import van from "vanjs-core"
import { api, navigate } from "../main"

const { div, h1, h2, table, tr, td, th, button, select, option, span, p } = van.tags

interface ProposalItem {
  parsedName: string
  quantity: number
  unitPriceCents: number
  matchedItemId: number
  confidence: number
  categoryId: number
  isNewCategory: boolean
  userChoice: string
}

interface Proposal {
  proposalId: number
  ownerId: number
  merchant: string
  date: number
  photoUrl: string
  items: ProposalItem[]
  totalCents: number
  status: string
}

const statusBadge = (status: string) => {
  const classes: Record<string, string> = {
    parsing: "badge-parsing",
    pending: "badge-pending",
    failed: "badge-failed",
    approved: "badge-approved",
  }
  return span({ class: `badge ${classes[status] || ""}` }, status)
}

const ProposalCard = (proposal: Proposal, onAction: () => void) => {
  const choices = van.state<Record<number, string>>({})
  const approving = van.state(false)

  const handleChoice = (index: number, choice: string) => {
    choices.val = { ...choices.val, [index]: choice }
  }

  const handleApprove = async () => {
    approving.val = true
    try {
      await api.post(`/proposals/${proposal.proposalId}/approve`, {
        choices: choices.val,
      })
      onAction()
    } catch (err) {
      console.error("Failed to approve proposal:", err)
    } finally {
      approving.val = false
    }
  }

  const handleRetry = async () => {
    try {
      await api.post(`/proposals/${proposal.proposalId}/reparse`, {})
      onAction()
    } catch (err) {
      console.error("Failed to retry proposal:", err)
    }
  }

  // Parsing — show spinner card
  if (proposal.status === "parsing") {
    return div({ class: "proposal-form card" },
      div({ class: "card-header" },
        h2("Parsing receipt..."),
        statusBadge("parsing"),
      ),
      div({ class: "parsing-indicator" },
        div({ class: "spinner" }),
        span(`${proposal.items?.length || 0} items found so far`),
      ),
      button({ onclick: () => navigate(`/proposals/${proposal.proposalId}`) }, "Watch Progress"),
    )
  }

  // Failed — show error card
  if (proposal.status === "failed") {
    return div({ class: "proposal-form card" },
      div({ class: "card-header" },
        h2("Parse Failed"),
        statusBadge("failed"),
      ),
      p("An error occurred while parsing this receipt"),
      button({ onclick: handleRetry, class: "retry-btn" }, "Retry"),
    )
  }

  // Pending — show full form
  return div({ class: "proposal-form card" },
    div({ class: "card-header" },
      h2(`Proposal from ${proposal.merchant || "Unknown"}`),
      statusBadge("pending"),
    ),
    table(
      tr(th("Item"), th("Qty"), th("Price"), th("Confidence"), th("Action")),
      ...proposal.items.map((item, index) =>
        tr(
          td(item.parsedName),
          td(item.quantity.toString()),
          td(`$${(item.unitPriceCents / 100).toFixed(2)}`),
          td(`${(item.confidence * 100).toFixed(0)}%`),
          td(
            item.confidence >= 0.99
              ? "Auto-matched"
              : item.confidence > 0.80
                ? select(
                    { onchange: (e: Event) => handleChoice(index, (e.target as HTMLSelectElement).value) },
                    option({ value: "" }, "Choose..."),
                    option({ value: "existing" }, "Use existing"),
                    option({ value: "new" }, "Create new"),
                  )
                : "New item"
          ),
        )
      ),
    ),
    div({ class: "proposal-summary" },
      p(`Total: $${(proposal.totalCents / 100).toFixed(2)}`),
      p(`Date: ${new Date(proposal.date * 1000).toLocaleDateString()}`),
    ),
    button({
      onclick: handleApprove,
      disabled: approving,
      class: "approve-btn",
    }, approving.val ? "Approving..." : "Approve Receipt"),
  )
}

const ProposalsPage = () => {
  const proposals = van.state<Proposal[]>([])
  const loading = van.state(true)

  const loadProposals = async () => {
    loading.val = true
    try {
      const data = await api.get("/proposals")
      proposals.val = data || []
    } catch (err) {
      console.error("Failed to load proposals:", err)
    }
    loading.val = false
  }

  loadProposals()

  const handleAction = () => {
    loadProposals()
  }

  return div({ class: "proposals-page" },
    h1("Proposals"),
    () => loading.val
      ? div("Loading...")
      : proposals.val.length === 0
        ? div("No active proposals")
        : proposals.val.map(p => ProposalCard(p, handleAction)),
  )
}

export default ProposalsPage
```

- [ ] **Step 2: Add CSS for badges and parsing state**

Add to the relevant CSS file (check where styles live — likely `client/styles.css` or similar):

```css
.badge {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 12px;
  font-size: 0.75rem;
  font-weight: 600;
  text-transform: uppercase;
}

.badge-parsing {
  background: #e3f2fd;
  color: #1565c0;
}

.badge-pending {
  background: #fff3e0;
  color: #e65100;
}

.badge-failed {
  background: #fce4ec;
  color: #c62828;
}

.badge-approved {
  background: #e8f5e9;
  color: #2e7d32;
}

.spinner {
  width: 20px;
  height: 20px;
  border: 2px solid #e0e0e0;
  border-top: 2px solid #1565c0;
  border-radius: 50%;
  animation: spin 1s linear infinite;
  display: inline-block;
}

@keyframes spin {
  0% { transform: rotate(0deg); }
  100% { transform: rotate(360deg); }
}

.parsing-indicator {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 0;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.skeleton-header {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-bottom: 16px;
}

.skeleton-line {
  height: 20px;
  background: linear-gradient(90deg, #f0f0f0 25%, #e0e0e0 50%, #f0f0f0 75%);
  background-size: 200% 100%;
  animation: shimmer 1.5s infinite;
  border-radius: 4px;
}

.skeleton-merchant { width: 60%; }
.skeleton-date { width: 40%; }
.skeleton-total { width: 30%; }

@keyframes shimmer {
  0% { background-position: 200% 0; }
  100% { background-position: -200% 0; }
}

.progress-msg {
  color: #666;
  font-style: italic;
  margin-bottom: 12px;
}

.parsing-placeholder {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  padding: 32px 0;
}

.failed-content {
  text-align: center;
  padding: 32px 0;
}

.retry-btn {
  background: #1565c0;
  color: white;
  border: none;
  padding: 8px 16px;
  border-radius: 4px;
  cursor: pointer;
  margin-top: 12px;
}

.retry-btn:hover {
  background: #0d47a1;
}
```

- [ ] **Step 3: Commit**

```bash
git add client/pages/proposals.ts
git commit -m "feat: show all proposal statuses with badges and actions"
```

---

### Task 10: Handle Photo Store Get for Reparse

**Files:**
- Modify: `internal/photo/` (check interface)

- [ ] **Step 1: Verify photo.Store has a Get method**

Check the photo store interface. If it doesn't have a `Get(ctx, proposalID)` method that returns `([]byte, error)`, add one. The reparse endpoint needs to read the original photo bytes.

Check `internal/photo/` for the interface definition. If `Get` exists, skip to step 3. If not:

In the photo store implementation, add:

```go
func (s *LocalCache) Get(ctx context.Context, proposalID uint64) ([]byte, error) {
    // Try local cache first
    path := s.cachePath(proposalID)
    data, err := os.ReadFile(path)
    if err == nil {
        return data, nil
    }
    // Fall back to GCloud
    if s.gcloud != nil {
        return s.gcloud.GetPhoto(ctx, proposalID)
    }
    return nil, fmt.Errorf("photo not found for proposal %d", proposalID)
}
```

Also add `Get` to the `photo.Store` interface if needed.

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/photo/`
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add internal/photo/
git commit -m "feat: add Get method to photo store for reparse support"
```

---

### Task 11: Final Integration Test

- [ ] **Step 1: Build everything**

Run: `go build ./...`
Expected: no output (success)

- [ ] **Step 2: Build frontend**

Run: `bun install && bun run build_client`
Expected: no errors

- [ ] **Step 3: Manual smoke test**

Start the server with `mise run start_server`, then:

1. Open the web app
2. Navigate to Upload Receipt
3. Select a photo and click Upload
4. Verify: redirects immediately to proposal detail page with parsing state
5. Verify: items appear one by one
6. Verify: transitions to pending state when done
7. Navigate to Proposals list — verify the proposal appears with correct status
8. Test retry: force an error (e.g., bad LLM key), verify failed state and retry button

- [ ] **Step 4: Commit any fixes**

```bash
git add -A
git commit -m "fix: integration fixes for async receipt parsing"
```
