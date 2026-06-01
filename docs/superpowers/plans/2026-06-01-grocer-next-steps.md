# Grocer — Next Steps Implementation Plan

## Phase 1: GCloud Persistence

### 1.1 Snapshot Storage

**Files:** `internal/store/snapshot.go` (new), `internal/store/gcloud.go` (new)

Create `Snapshot` proto message (reuse existing types):
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

**Snapshot flow:**
- `Serialize()` — collect all memdb tables → proto → gzip → bytes
- `Deserialize()` — bytes → gunzip → proto → insert into memdb

**GCloud client:**
- `Pull(ctx)` → download `snapshots/latest.pb.gz`
- `Push(ctx, data)` → upload (overwrite)

**Store integration:**
- `NewStore()` → if GCS_BUCKET set, pull snapshot, populate memdb
- After every write operation → push snapshot (async, don't block)

**Config:** GCS_BUCKET, GCS_PREFIX (default "snapshots/"), GCS_CREDENTIALS_FILE

### 1.2 Photo Storage

**Files:** `internal/photo/gcloud.go` (new), `internal/photo/cache.go` (new)

**GCloud store:**
- `Save(receiptID, data)` → upload to `photos/{receiptID}.jpg`
- `Get(url)` → download bytes

**Local cache (LRU):**
- Check `./cache/photos/{hash}.jpg` first
- If miss → download from GCloud → cache → return
- Evict oldest when cache > 500MB

**Receipt upload flow:**
1. POST /api/receipts/upload → get photo bytes
2. Save to GCloud → get URL
3. Store URL in Receipt.PhotoURL
4. Parse with LLM → create proposal

---

## Phase 2: Bot User Mapping

### 2.1 User Identity

**Files:** `internal/store/memdb.go` (add BotUser table), `internal/bot/telegram.go`, `internal/bot/discord.go`

Add to memdb schema:
```go
"bot_users": {
    Name: "bot_users",
    Indexes: map[string]*memdb.IndexSchema{
        "id": {Name: "id", Unique: true, Indexer: &memdb.StringFieldIndex{Field: "ExternalID"}},
    },
},
```

Type:
```go
type BotUser struct {
    ExternalID string // "telegram:12345" or "discord:67890"
    UserID     uint64 // internal user ID
}
```

**Flow:**
1. User sends photo to bot
2. Bot looks up BotUser by external ID
3. If not found → reply "Unknown user. Register at web app first."
4. If found → parse receipt with UserID

### 2.2 Bot Registration

Add CLI flag: `--link-bot --username dad --telegram 12345`

Or web UI page: Settings → Link Telegram/Discord account

---

## Phase 3: Receipt Upload Page

### 3.1 Upload UI

**Files:** `client/pages/upload.ts` (new)

Page with:
- Drag-and-drop photo area
- Camera capture button (mobile)
- Preview before submit
- Submit → POST /api/receipts/upload
- Redirect to proposals page

### 3.2 Proposal Approval Polish

**Files:** `client/pages/proposals.ts` (update)

Improve proposal view:
- Show receipt photo alongside items
- Highlight items needing review (yellow)
- Show matched item name for "existing" choice
- Batch approve (approve all confident, review rest)

---

## Phase 4: Analysis Enhancements

### 4.1 Date Range Filters

**Files:** `client/pages/analysis.ts` (update), `internal/api/analysis.go` (update)

Add to analysis page:
- Date range picker (from/to)
- Quick selects: This week, This month, Last 3 months, This year
- Pass ?from=&to= to all analysis endpoints

### 4.2 Per-Member Breakdown

**Files:** `client/pages/analysis.ts` (update)

Add chart:
- Bar chart showing spending per family member
- Filter by date range
- Click member → see their receipts

### 4.3 Item Price Tracking

**Files:** `client/pages/item-detail.ts` (new)

Item detail page:
- Price history line chart (from /api/analysis/items/{id})
- List of receipts containing this item
- Price trend (up/down/stable)

### 4.4 Merchant Comparison

**Files:** `client/pages/merchants.ts` (new), `internal/api/analysis.go` (update)

New endpoint: GET /api/analysis/merchants?itemId=X
- Returns: merchant name, last price, average price

UI:
- Select item → show prices across merchants
- Highlight cheapest

### 4.5 Similar Item Comparison

**Files:** `internal/receipt/matcher.go` (update), `client/pages/items.ts` (update)

Group items by normalized name:
- Show all variants (2% Milk, Whole Milk 2%, etc.)
- Price comparison across variants
- Merge UI (combine variants into one item)

---

## Phase 5: Production Readiness

### 5.1 Docker Compose

**Files:** `deploy/docker-compose.yml` (update)

```yaml
services:
  server:
    build: .
    ports: ["8080:8080"]
    env_file: .env
    volumes:
      - ./cache:/app/cache
```

### 5.2 Health Check

**Files:** `internal/api/router.go` (update)

Add: GET /api/health → {"status": "ok", "version": "..."}

### 5.3 Request Logging

**Files:** `internal/api/middleware.go` (new)

Middleware logging: method, path, status, duration

### 5.4 Backup CLI

**Files:** `cmd/backup/main.go` (new)

Commands:
- `backup export` → download snapshot, save locally
- `backup import <file>` → upload local snapshot to GCloud

---

## Execution Order

```
1.1 Snapshot Storage ─────────────────┐
                                       ├─→ 1.2 Photo Storage
                                       │
2.1 Bot User Mapping ─────────────────┤
                                       │
3.1 Upload Page ──────────────────────┤
                                       │
3.2 Proposal Polish ──────────────────┤
                                       │
4.1 Date Filters ─────────────────────┤
                                       │
4.2 Member Breakdown ─────────────────┤
                                       │
4.3 Price Tracking ───────────────────┤
                                       │
4.4 Merchant Comparison ──────────────┤
                                       │
4.5 Similar Items ────────────────────┤
                                       │
5.1 Docker Compose ───────────────────┘
```

Each task is independent after 1.1 (persistence). Can be done in any order.

## Estimated Effort

| Phase | Tasks | Hours |
|-------|-------|-------|
| 1. Persistence | 2 | 5 |
| 2. Bot Mapping | 2 | 4 |
| 3. Upload UI | 2 | 4 |
| 4. Analysis | 5 | 10 |
| 5. Production | 4 | 4 |
| **Total** | **15** | **27** |
