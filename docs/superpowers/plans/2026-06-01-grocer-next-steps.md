# Grocer — Next Steps Implementation Plan

## Current State

```
internal/
├── domain/types.go          ✅ User, Category, Merchant, Item, Receipt, Proposal
├── store/memdb.go           ✅ CRUD for all entities
├── store/idgen.go           ✅ Timestamp-based ID generator
├── llm/llm.go               ✅ Provider interface
├── llm/kimi.go              ✅ Kimi K2.6 implementation
├── llm/qwen.go              ✅ Qwen 3.6 Plus implementation
├── receipt/parser.go        ✅ Receipt parsing orchestration
├── receipt/matcher.go       ✅ Fuzzy item matching (Jaccard + Levenshtein)
├── api/router.go            ✅ HTTP router with auth middleware
├── api/auth.go              ✅ Login endpoint
├── api/receipts.go          ✅ Receipt CRUD + upload
├── api/proposals.go         ✅ Proposal CRUD + approve
├── api/items.go             ✅ Item CRUD
├── api/categories.go        ✅ Category CRUD
├── api/merchants.go         ✅ Merchant CRUD
├── api/analysis.go          ✅ Spending, category, family, item analysis
├── bot/bot.go               ✅ Bot interface
├── bot/telegram.go          ✅ Telegram bot
├── bot/discord.go           ✅ Discord bot
└── photo/store.go           ✅ Photo store interface + GCloud + local cache

cmd/server/main.go           ✅ Server entry point, CLI flags
client/                      ✅ VanJS frontend with all pages
```

**What's missing:**
- Snapshot persistence (data lost on restart)
- Photo upload flow
- Bot user mapping
- Analysis polish
- Production readiness

---

## Phase 1: GCloud Persistence

### 1.1 Snapshot Proto

**File:** `proto/grocer.proto` (update)

Add Snapshot message:

```protobuf
message Snapshot {
  repeated User users = 1;
  repeated Category categories = 2;
  repeated Merchant merchants = 3;
  repeated Item items = 4;
  repeated Receipt receipts = 5;
  repeated Proposal proposals = 6;
}
```

Regenerate: `protoc --go_out=proto/out_proto/ proto/grocer.proto`

### 1.2 Snapshot Serialization

**File:** `internal/store/snapshot.go` (new)

```go
type SnapshotData struct {
    Users      []*domain.User
    Categories []*domain.Category
    Merchants  []*domain.Merchant
    Items      []*domain.Item
    Receipts   []*domain.Receipt
    Proposals  []*domain.Proposal
}

// Serialize: SnapshotData → proto.Snapshot → gzip → []byte
// Deserialize: []byte → gunzip → proto.Snapshot → SnapshotData
```

Conversion functions for each entity type (domain ↔ proto).

### 1.3 GCloud Client

**File:** `internal/store/gcloud.go` (new)

```go
type GCloudStorage struct {
    client *storage.Client
    bucket string
    prefix string
}

func (g *GCloudStorage) Pull(ctx context.Context) ([]byte, error)
// Downloads g.prefix + "snapshot.pb.gz"
// Returns nil if not exists (first run)

func (g *GCloudStorage) Push(ctx context.Context, data []byte) error
// Uploads with ContentType "application/gzip"
// Overwrites existing
```

**Config:**
- `GCS_BUCKET` — bucket name (required)
- `GCS_PREFIX` — path prefix (default: "snapshots/")
- `GCS_CREDENTIALS_FILE` — service account JSON (required)

### 1.4 Store Integration

**File:** `internal/store/memdb.go` (update)

Add to Store struct:
```go
type Store struct {
    // ... existing fields
    snapshot   *GCloudStorage
    snapshotMu sync.Mutex
}

func (s *Store) LoadSnapshot(ctx context.Context) error
// Pull from GCloud → Deserialize → Insert into memdb
// If no snapshot exists, return nil (empty store)

func (s *Store) SaveSnapshot(ctx context.Context) error
// Lock → Collect all tables → Serialize → Push to GCloud
// Call after every write operation
```

**Integration points:**
- `NewStore()` — if GCS_BUCKET set, create GCloudStorage
- After `CreateUser`, `CreateReceipt`, `CreateProposal`, etc. — call `SaveSnapshot()`
- `SaveSnapshot()` should be async (don't block the request)

### 1.5 Startup Sequence

**File:** `cmd/server/main.go` (update)

```go
func main() {
    // 1. Create store
    s, _ := store.NewStore()
    
    // 2. If GCS configured, load snapshot
    if os.Getenv("GCS_BUCKET") != "" {
        gcs, _ := store.NewGCloudStorage(ctx, credsFile, bucket, prefix)
        s.SetSnapshotStorage(gcs)
        if err := s.LoadSnapshot(ctx); err != nil {
            log.Fatalf("Failed to load snapshot: %v", err)
        }
    }
    
    // 3. Create user (if --create-user flag)
    // 4. Start server
}
```

### 1.6 Shutdown Sequence

```go
go func() {
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh
    
    // Save final snapshot
    if err := s.SaveSnapshot(ctx); err != nil {
        log.Printf("Failed to save snapshot: %v", err)
    }
    
    // Stop bots, shutdown server
}()
```

---

## Phase 2: Photo Storage

### 2.1 Photo Upload Endpoint

**File:** `internal/api/receipts.go` (update)

```go
func (r *Router) handleUploadReceipt(w http.ResponseWriter, req *http.Request) {
    // 1. Parse multipart form (10MB limit)
    // 2. Get photo bytes
    // 3. Save to photo store → get URL
    // 4. Parse with LLM → get proposal
    // 5. Set proposal.PhotoURL
    // 6. Return proposal
}
```

**Dependencies:** Need photo.Store injected into Router

### 2.2 Photo Serving

**File:** `internal/api/photos.go` (new)

```go
func (r *Router) handleGetPhoto(w http.ResponseWriter, req *http.Request) {
    // GET /api/photos/{receiptId}
    // 1. Check local cache
    // 2. If miss, download from GCloud
    // 3. Cache locally
    // 4. Return with Content-Type: image/jpeg
}
```

### 2.3 Photo Store Initialization

**File:** `cmd/server/main.go` (update)

```go
// Initialize photo store
photoStore := photo.NewGCloudStore(gcsClient, bucket, "photos/")
photoCache := photo.NewLocalCache("./cache/photos", 500)

// Inject into router
router := api.NewRouter(s, parser, photoStore, photoCache)
```

---

## Phase 3: Bot User Mapping

### 3.1 BotUser Table

**File:** `internal/store/memdb.go` (update)

Add to schema:
```go
"bot_users": {
    Name: "bot_users",
    Indexes: map[string]*memdb.IndexSchema{
        "id": {
            Name:    "id",
            Unique:  true,
            Indexer: &memdb.StringFieldIndex{Field: "ExternalID"},
        },
    },
},
```

Type:
```go
type BotUser struct {
    ExternalID string // "telegram:12345" or "discord:67890"
    UserID     uint64
}
```

CRUD:
```go
func (s *Store) CreateBotUser(bu *BotUser) error
func (s *Store) GetBotUser(externalID string) (*BotUser, error)
func (s *Store) ListBotUsers() ([]*BotUser, error)
func (s *Store) DeleteBotUser(externalID string) error
```

### 3.2 Bot User Lookup

**File:** `internal/bot/telegram.go` (update)

```go
func (t *TelegramBot) handleUpdate(ctx context.Context, update tgbotapi.Update) {
    if update.Message.Photo != nil {
        externalID := fmt.Sprintf("telegram:%d", update.Message.From.ID)
        
        botUser, err := t.store.GetBotUser(externalID)
        if err != nil {
            t.sendMessage(chatID, "Unknown user. Link your account at the web app.")
            return
        }
        
        proposalID, err := t.handler.HandlePhoto(ctx, photoData, botUser.UserID)
        // ...
    }
}
```

### 3.3 Link Bot Account CLI

**File:** `cmd/server/main.go` (update)

Add flag: `--link-bot --username dad --telegram 12345`

```go
if *linkBot {
    user, _ := s.GetUserByUsername(*username)
    if user == nil {
        log.Fatal("User not found")
    }
    
    externalID := ""
    if *telegramID != "" {
        externalID = "telegram:" + *telegramID
    }
    if *discordID != "" {
        externalID = "discord:" + *discordID
    }
    
    s.CreateBotUser(&store.BotUser{
        ExternalID: externalID,
        UserID:     user.UserID,
    })
}
```

### 3.4 Bot Users API

**File:** `internal/api/bot-users.go` (new)

```go
GET    /api/bot-users           → list all bot user mappings
POST   /api/bot-users           → { externalId, userId }
DELETE /api/bot-users/{id}      → delete mapping
```

### 3.5 Bot Users Page

**File:** `client/pages/settings.ts` (new)

Settings page:
- List of bot user mappings
- Form to add new mapping
- Delete button

---

## Phase 4: Receipt Upload Page

### 4.1 Upload Page

**File:** `client/pages/upload.ts` (new)

```typescript
const UploadPage = () => {
    const photo = van.state<File | null>(null)
    const preview = van.state<string | null>(null)
    const uploading = van.state(false)
    
    const handleDrop = (e: DragEvent) => {
        e.preventDefault()
        photo.val = e.dataTransfer.files[0]
        preview.val = URL.createObjectURL(photo.val)
    }
    
    const handleSubmit = async () => {
        uploading.val = true
        const formData = new FormData()
        formData.append("photo", photo.val)
        
        const result = await fetch("/api/receipts/upload", {
            method: "POST",
            headers: { "Authorization": `Bearer ${token}` },
            body: formData,
        })
        
        const proposal = await result.json()
        navigate(`/proposals/${proposal.proposalId}`)
    }
    
    return div({ class: "upload-page" },
        h1("Upload Receipt"),
        div({ class: "dropzone", ondrop: handleDrop, ondragover: preventDefault },
            preview.val 
                ? img({ src: preview.val })
                : "Drag photo here or click to select"
        ),
        button({ onclick: handleSubmit, disabled: uploading }, "Upload"),
    )
}
```

### 4.2 Camera Capture (Mobile)

```typescript
const CameraCapture = () => {
    const video = van.state<HTMLVideoElement | null>(null)
    
    const startCamera = async () => {
        const stream = await navigator.mediaDevices.getUserMedia({ video: true })
        video.val.srcObject = stream
    }
    
    const capture = () => {
        const canvas = document.createElement("canvas")
        canvas.width = video.val.videoWidth
        canvas.height = video.val.videoHeight
        canvas.getContext("2d").drawImage(video.val, 0, 0)
        return canvas.toBlob("image/jpeg")
    }
}
```

### 4.3 Proposal Detail Page

**File:** `client/pages/proposal.ts` (new)

Split proposal view:
- Left: receipt photo (zoomable)
- Right: items table with actions

```typescript
const ProposalDetailPage = () => {
    return div({ class: "proposal-detail" },
        div({ class: "proposal-photo" },
            img({ src: proposal.photoUrl }),
        ),
        div({ class: "proposal-items" },
            table(
                tr(th("Item"), th("Qty"), th("Price"), th("Match"), th("Action")),
                ...items.map((item, i) =>
                    tr(
                        td(item.parsedName),
                        td(item.quantity),
                        td(`$${item.unitPrice}`),
                        td(item.matchedItemId ? `→ ${matchedName}` : "New"),
                        td(
                            item.confidence > 0.80
                                ? select(/* existing/new choice */)
                                : "Auto"
                        ),
                    )
                ),
            ),
            button({ onclick: handleApprove }, "Approve"),
        ),
    )
}
```

---

## Phase 5: Analysis Enhancements

### 5.1 Date Range Filter Component

**File:** `client/components/date-range.ts` (new)

```typescript
const DateRange = (from: State<string>, to: State<string>) => {
    const presets = [
        { label: "This week", from: startOfWeek(), to: today() },
        { label: "This month", from: startOfMonth(), to: today() },
        { label: "Last 3 months", from: monthsAgo(3), to: today() },
        { label: "This year", from: startOfYear(), to: today() },
    ]
    
    return div({ class: "date-range" },
        div({ class: "presets" },
            presets.map(p => 
                button({ onclick: () => { from.val = p.from; to.val = p.to } }, p.label)
            ),
        ),
        input({ type: "date", value: from, onchange: ... }),
        span("to"),
        input({ type: "date", value: to, onchange: ... }),
    )
}
```

### 5.2 Spending Chart with Filters

**File:** `client/pages/analysis.ts` (update)

```typescript
const AnalysisPage = () => {
    const from = van.state("")
    const to = van.state("")
    const granularity = van.state("month")
    
    const loadData = async () => {
        const params = new URLSearchParams({ granularity: granularity.val })
        if (from.val) params.set("from", from.val)
        if (to.val) params.set("to", to.val)
        
        const [spending, categories, family] = await Promise.all([
            api.get(`/analysis/spending?${params}`),
            api.get(`/analysis/categories?${params}`),
            api.get(`/analysis/family?${params}`),
        ])
        // ...
    }
    
    return div(
        DateRange(from, to),
        // ... charts
    )
}
```

### 5.3 Member Breakdown Chart

Add to analysis page:

```typescript
const MemberChart = (data) => {
    return div({ class: "chart-container card" },
        h2("Family Members"),
        canvas({ id: "member-chart" }),
    )
}

// In initCharts():
if (familyCanvas && familyData.val.length > 0) {
    familyChart = createBarChart(familyCanvas, {
        labels: familyData.val.map(d => d.name),
        datasets: [{
            label: "Spending",
            data: familyData.val.map(d => d.total),
            backgroundColor: ["#3b82f6", "#22c55e", "#eab308", "#ef4444"],
        }],
    })
}
```

### 5.4 Item Price History

**File:** `client/pages/item-detail.ts` (new)

```typescript
const ItemDetailPage = () => {
    const item = van.state(null)
    const history = van.state([])
    
    const loadData = async () => {
        const id = window.location.hash.split("/").pop()
        item.val = await api.get(`/items/${id}`)
        history.val = await api.get(`/analysis/items/${id}`)
    }
    
    return div(
        h1(item.val?.name),
        div({ class: "chart-container card" },
            h2("Price History"),
            canvas({ id: "price-chart" }),
        ),
        h2("Purchase History"),
        table(
            ...history.val.map(h =>
                tr(td(h.date), td(`$${h.price.toFixed(2)}`))
            ),
        ),
    )
}
```

### 5.5 Merchant Comparison Endpoint

**File:** `internal/api/analysis.go` (update)

```go
func (r *Router) handleAnalysisMerchantComparison(w http.ResponseWriter, req *http.Request) {
    itemID := req.URL.Query().Get("itemId")
    
    // Find all receipts containing this item
    // Group by merchant
    // Return: merchant name, last price, average price, receipt count
}
```

Response:
```json
[
    { "merchant": "Costco", "lastPrice": 3.99, "avgPrice": 4.12, "count": 5 },
    { "merchant": "Walmart", "lastPrice": 4.49, "avgPrice": 4.35, "count": 3 }
]
```

### 5.6 Merchant Comparison Page

**File:** `client/pages/merchants.ts` (new)

```typescript
const MerchantComparisonPage = () => {
    const items = van.state([])
    const selectedItem = van.state(null)
    const comparison = van.state([])
    
    return div(
        select({ onchange: ... },
            items.val.map(i => option({ value: i.itemId }, i.name)),
        ),
        table(
            tr(th("Merchant"), th("Last Price"), th("Avg Price"), th("Times")),
            ...comparison.val.map(c =>
                tr(td(c.merchant), td(`$${c.lastPrice}`), td(`$${c.avgPrice}`), td(c.count))
            ),
        ),
    )
}
```

### 5.7 Similar Items Grouping

**File:** `internal/receipt/matcher.go` (update)

Add method:
```go
func (m *Matcher) FindSimilarItems(normalized string) ([]*domain.Item, error) {
    // Find all items with same normalized name
    // Return grouped list
}
```

**File:** `client/pages/items.ts` (update)

Group items by normalized name:
```typescript
const grouped = groupBy(items, "normalized")
// Show: "Milk" → [2% Milk, Whole Milk, Skim Milk]
```

### 5.8 Item Merge UI

**File:** `client/pages/items.ts` (update)

Add merge functionality:
- Select 2+ items
- Click "Merge"
- Choose canonical name
- All receipts updated to reference merged item

```go
// API endpoint
POST /api/items/merge
{
    "targetId": 123,
    "sourceIds": [456, 789]
}
```

---

## Phase 6: Production Readiness

### 6.1 Health Check

**File:** `internal/api/router.go` (update)

```go
r.mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
    writeJSON(w, http.StatusOK, map[string]string{
        "status":  "ok",
        "version": version,
    })
})
```

### 6.2 Request Logging Middleware

**File:** `internal/api/middleware.go` (new)

```go
func loggingMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        
        // Wrap response writer to capture status
        ww := &responseWriter{ResponseWriter: w, statusCode: 200}
        
        next.ServeHTTP(ww, r)
        
        log.Printf("%s %s %d %s",
            r.Method, r.URL.Path,
            ww.statusCode, time.Since(start))
    })
}
```

### 6.3 Error Handling

**File:** `internal/api/middleware.go` (update)

```go
func recoveryMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        defer func() {
            if err := recover(); err != nil {
                log.Printf("PANIC: %v\n%s", err, debug.Stack())
                writeError(w, http.StatusInternalServerError, "internal error")
            }
        }()
        next.ServeHTTP(w, r)
    })
}
```

### 6.4 Backup CLI

**File:** `cmd/backup/main.go` (new)

```go
func main() {
    command := os.Args[1]
    
    switch command {
    case "export":
        // Pull snapshot from GCloud
        // Save to local file
        
    case "import":
        // Read local file
        // Push to GCloud
        
    case "list":
        // List snapshots in GCloud
    }
}
```

### 6.5 Docker Compose

**File:** `deploy/docker-compose.yml` (update)

```yaml
version: '3.8'

services:
  server:
    build: .
    ports:
      - "8080:8080"
    env_file:
      - .env
    environment:
      - GCS_BUCKET=${GCS_BUCKET}
      - GCS_CREDENTIALS_FILE=/app/credentials.json
      - LLM_PROVIDER=${LLM_PROVIDER}
      - LLM_API_KEY=${LLM_API_KEY}
      - LLM_MODEL=${LLM_MODEL}
    volumes:
      - ./credentials.json:/app/credentials.json:ro
      - ./cache:/app/cache
    restart: unless-stopped
```

### 6.6 Environment Template

**File:** `.env.example` (new)

```bash
# LLM
LLM_PROVIDER=kimi
LLM_API_KEY=your-api-key
LLM_MODEL=kimi-k2.6

# GCloud
GCS_BUCKET=grocer-snapshots
GCS_CREDENTIALS_FILE=credentials.json

# Bots (optional)
TELEGRAM_BOT_TOKEN=
DISCORD_BOT_TOKEN=
BOT_WEB_URL=http://localhost:8080
```

---

## Execution Order

```
Phase 1: GCloud Persistence (5h)
├── 1.1 Snapshot Proto (30m)
├── 1.2 Snapshot Serialization (1h)
├── 1.3 GCloud Client (1h)
├── 1.4 Store Integration (1h)
├── 1.5 Startup Sequence (30m)
└── 1.6 Shutdown Sequence (30m)
    │
    ▼
Phase 2: Photo Storage (4h)
├── 2.1 Photo Upload Endpoint (1h)
├── 2.2 Photo Serving (1h)
└── 2.3 Photo Store Initialization (2h)
    │
    ▼
Phase 3: Bot User Mapping (4h)
├── 3.1 BotUser Table (1h)
├── 3.2 Bot User Lookup (1h)
├── 3.3 Link Bot Account CLI (1h)
├── 3.4 Bot Users API (30m)
└── 3.5 Bot Users Page (30m)
    │
    ▼
Phase 4: Receipt Upload Page (4h)
├── 4.1 Upload Page (2h)
├── 4.2 Camera Capture (1h)
└── 4.3 Proposal Detail Page (1h)
    │
    ▼
Phase 5: Analysis Enhancements (10h)
├── 5.1 Date Range Filter (1h)
├── 5.2 Spending Chart with Filters (1h)
├── 5.3 Member Breakdown Chart (1h)
├── 5.4 Item Price History (2h)
├── 5.5 Merchant Comparison Endpoint (1h)
├── 5.6 Merchant Comparison Page (2h)
├── 5.7 Similar Items Grouping (1h)
└── 5.8 Item Merge UI (1h)
    │
    ▼
Phase 6: Production Readiness (4h)
├── 6.1 Health Check (15m)
├── 6.2 Request Logging (30m)
├── 6.3 Error Handling (30m)
├── 6.4 Backup CLI (1h)
├── 6.5 Docker Compose (1h)
└── 6.6 Environment Template (15m)
```

**Total: 15 tasks, ~27 hours**

---

## Dependencies

```
1.1 → 1.2 → 1.3 → 1.4 → 1.5 → 1.6
                    ↓
                    2.1 → 2.2 → 2.3
                    ↓
                    3.1 → 3.2 → 3.3 → 3.4 → 3.5
                    ↓
                    4.1 → 4.2 → 4.3
                    ↓
                    5.1 → 5.2 → 5.3 → 5.4
                    ↓
                    5.5 → 5.6 → 5.7 → 5.8
                    ↓
                    6.1 → 6.2 → 6.3 → 6.4 → 6.5 → 6.6
```

After Phase 1, all other phases can be done in parallel.

---

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| GCloud quota exceeded | Implement exponential backoff on push |
| Snapshot too large | Compress with gzip, consider incremental |
| Photo cache disk full | LRU eviction, configurable max size |
| Bot rate limiting | Queue messages, batch sends |
| LLM timeout | 30s timeout, retry once, fallback to manual |
| Concurrent writes | Last-write-wins (acceptable for family tool) |

---

## Testing Strategy

### Unit Tests
- Snapshot serialization/deserialization
- Fuzzy matching accuracy
- Date range filtering

### Integration Tests
- GCloud pull/push (with mock)
- Photo upload/download flow
- Bot user mapping

### E2E Tests
- Receipt upload → proposal → approve → receipt
- Analysis with date filters
- Bot photo → web approval

---

## Success Criteria

- [ ] Data persists across server restarts
- [ ] Photos stored in GCloud with local cache
- [ ] Bots identify family members
- [ ] Upload page works on mobile
- [ ] Analysis shows date-filtered trends
- [ ] Can compare item prices across merchants
- [ ] Docker deployment works
