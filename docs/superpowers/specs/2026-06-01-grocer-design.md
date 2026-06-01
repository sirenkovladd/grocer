# Grocer — Design Spec

A family-shared grocery receipt tracker with LLM-powered parsing, category management, and spending analysis.

## Overview

Grocer lets families photograph grocery receipts, automatically extract and categorize items using an LLM, track spending over time, and analyze purchasing patterns across family members.

**Core workflow:**
1. Family member sends receipt photo (via web upload, Telegram bot, or Discord bot)
2. LLM parses photo into structured receipt data
3. System matches items against known catalog, flags uncertain matches
4. User reviews and approves the proposal in the web app
5. Receipt committed to store, available for analysis

## Architecture

**Modular monolith** — single Go binary, clean internal package boundaries.

```
grocer/
├── cmd/
│   └── server/          # Entry point, wires everything together
├── internal/
│   ├── domain/          # Core types: User, Receipt, Item, Merchant, Category
│   ├── store/           # memdb wrapper, snapshot pull/push to GCloud
│   ├── llm/             # Provider interface + implementations (Kimi, Qwen, etc.)
│   ├── receipt/         # Receipt parsing orchestration (photo → LLM → domain objects)
│   ├── bot/             # Telegram + Discord bot handlers
│   ├── api/             # HTTP handlers, routing
│   └── photo/           # Photo storage (GCloud + local cache)
├── client/              # VanJS frontend
├── deploy/              # Dockerfile, docker-compose
└── proto/               # Protobuf definitions (snapshot format)
```

## Data Model (Protobuf)

Compact, normalized format for GCloud snapshots:

```protobuf
message User {
  fixed64 userId = 1;
  string name = 2;
  string username = 3;
  string passwordHash = 4;
}

message Category {
  fixed64 categoryId = 1;
  string name = 2;
  optional fixed64 parentId = 3;
  int32 sortOrder = 4;
}

message Merchant {
  fixed64 merchantId = 1;
  string name = 2;
}

message Item {
  fixed64 itemId = 1;
  string name = 2;
  fixed64 categoryId = 3;
  fixed64 merchantId = 4;
  string normalized = 5;
  repeated string aliases = 6;
}

message Receipt {
  fixed64 receiptId = 1;
  fixed64 merchantId = 2;
  fixed64 ownerId = 3;
  fixed64 date = 4;
  string photoUrl = 5;
  repeated ReceiptItem items = 6;
  float total = 7;
}

message ReceiptItem {
  fixed64 itemId = 1;
  uint32 quantity = 2;
  float unitPrice = 3;
}

message Snapshot {
  repeated User users = 1;
  repeated Category categories = 2;
  repeated Merchant merchants = 3;
  repeated Item items = 4;
  repeated Receipt receipts = 5;
}
```

**Key decisions:**
- `ReceiptItem` references `itemId` only — category resolved via Item (no denormalization)
- `Receipt.date` is `fixed64` Unix timestamp (compact)
- `Item.normalized` groups similar items ("Whole Milk 2%" and "2% Milk" → same normalized)
- `Item.aliases` stores variations the LLM has seen (learning over time)

## LLM Interface

Provider-agnostic for easy extension:

```go
type Provider interface {
    ParseReceipt(ctx context.Context, photo []byte) (*ParsedReceipt, error)
    CategorizeItem(ctx context.Context, itemName string, existingCategories []Category) (*Categorization, error)
}
```

**Implementations:**
- `kimi.Provider` — `opencode.ai/zen/go/v1/chat/completions` (OpenAI-compatible)
- `qwen.Provider` — `opencode.ai/zen/go/v1/messages` (Anthropic-compatible)
- Configured via env vars: `LLM_PROVIDER`, `LLM_API_KEY`, `LLM_MODEL`

## Receipt Parsing Flow

1. Photo arrives → store photo → call `Provider.ParseReceipt(photo)` → `ParsedReceipt`
2. For each parsed item, fuzzy-match against existing items (by `normalized` + `aliases`)
3. **Three outcomes per item:**
   - **Exact match (≥99% confidence)** → auto-link to existing `itemId`
   - **Possible match (>80% and <99%)** → flag for user review
   - **No match (≤80%)** → new item, call `CategorizeItem`, create new record
4. Return a `Proposal` to the user with:
   - Auto-matched items (shown, no action needed)
   - Possible matches (show both options, user picks existing or confirms new)
   - New items with suggested category (user confirms or edits)
5. User approves → commit receipt to store

**Item matching learns over time:** user corrections become new aliases, improving future matches.

## Bots (Telegram + Discord)

Both bots handle photo input only:

```go
type Bot interface {
    Start(ctx context.Context) error
    Stop() error
}
```

**Flow:**
1. Receive photo message
2. Trigger receipt parsing → get proposal
3. Send summary with link: *"Receipt from Costco, 12 items, 3 need review. [View & approve →](https://app.example.com/proposal/123)"*
4. User finishes approval in web app

**Config:**
- `TELEGRAM_BOT_TOKEN`
- `DISCORD_BOT_TOKEN`
- `BOT_WEB_URL` — link to web app

**User identity:** Map Telegram `user_id` / Discord `user_id` to internal `userId`.

## Persistence

**memdb** as primary store, synced to GCloud via snapshots.

**On startup:**
1. Pull latest snapshot from GCloud
2. Deserialize protobuf into memdb
3. If pull fails → crash (fail fast, no silent data loss)

**On every write:**
1. Apply change to memdb
2. Serialize full state → gzip → upload to GCloud (overwrites previous)

**Config:**
- `GCS_BUCKET`
- `GCS_PREFIX` (e.g. `snapshots/`)
- `GCS_CREDENTIALS_FILE`

## Photo Storage

**GCloud (primary):** Upload to same bucket, prefix `photos/{receiptId}.jpg`

**Local cache:**
- On read, check local disk first (`./cache/photos/`)
- If miss, download from GCloud, cache locally
- LRU eviction, max 500MB

**Config:**
- `PHOTO_CACHE_DIR` (default `./cache/photos`)
- `PHOTO_CACHE_SIZE` (default `500` MB)

**Limits:** Max 10MB per photo, accept jpg/png/heic.

## HTTP API

```
# Auth
POST   /api/auth/login          → { username, password } → { token }

# Receipts
GET    /api/receipts             → ?from=&to=&owner=&category= → [Receipt]
GET    /api/receipts/:id         → Receipt
POST   /api/receipts/upload      → multipart photo → Proposal

# Proposals
GET    /api/proposals            → [Proposal] (pending approvals)
GET    /api/proposals/:id        → Proposal
POST   /api/proposals/:id/approve → { choices } → Receipt

# Items
GET    /api/items                → [Item]
GET    /api/items/:id            → Item (with receipt history)
PATCH  /api/items/:id            → update name/category/aliases

# Categories
GET    /api/categories           → [Category] (tree)
POST   /api/categories           → { name, parentId }
PATCH  /api/categories/:id       → { name, parentId, sortOrder }
DELETE /api/categories/:id       → merge into parent or delete

# Merchants
GET    /api/merchants            → [Merchant]
POST   /api/merchants            → { name }
PATCH  /api/merchants/:id        → { name }

# Analysis
GET    /api/analysis/spending    → ?from=&to=&granularity=day|week|month
GET    /api/analysis/categories  → ?from=&to=&owner=
GET    /api/analysis/family      → ?from=&to=
GET    /api/analysis/items/:id   → ?from=&to=
```

Auth via `Authorization: Bearer <token>` header (session-based, argon2id).

Users created via CLI only — no registration endpoint.

## Frontend (VanJS + Chart.js)

```
client/
├── index.html
├── main.ts              # Entry point, router
├── api.ts               # Fetch wrapper, auth
├── components/
│   ├── layout.ts        # Header, nav
│   ├── receipt-card.ts  # Receipt display
│   ├── proposal-form.ts # Approval flow
│   ├── category-tree.ts # Hierarchical picker
│   ├── item-detail.ts   # Item + price history
│   └── charts.ts        # Chart.js wrappers
├── pages/
│   ├── login.ts
│   ├── receipts.ts      # List with filters
│   ├── receipt.ts       # Detail view
│   ├── proposals.ts     # Pending approvals
│   ├── items.ts         # Item catalog
│   ├── categories.ts    # Category management
│   └── analysis.ts      # Dashboard
└── styles/
    └── main.css
```

**Pages:**
- **Receipts** — list with date range, owner, category filters
- **Proposals** — pending receipts, review & approve
- **Items** — catalog, click for price history
- **Analysis** — spending trends, category pie chart, family breakdown
- **Categories** — tree view, reorder, edit/add/delete

## Analysis Features

**v1 (core):**
- Spending over time (daily/weekly/monthly totals)
- Category breakdown (pie chart)
- Family member spending breakdown

**v2 (bonus):**
- Item price tracking over time
- Merchant comparison
- Similar item comparison (different brands/sizes of same product)

## Edge Cases

- **LLM failure:** Retry once, then show "Could not parse, enter manually"
- **LLM timeout:** 30s default, same fallback
- **Duplicate receipt:** Detect by photo hash, show "Already processed on [date]"
- **Concurrent edits:** Last write wins (acceptable for family tool)
- **Snapshot push failure:** Log error, retry with backoff, don't block request

## Config Summary

| Variable | Description |
|----------|-------------|
| `LLM_PROVIDER` | `kimi` or `qwen` |
| `LLM_API_KEY` | API key |
| `LLM_MODEL` | Model ID |
| `TELEGRAM_BOT_TOKEN` | Telegram bot token |
| `DISCORD_BOT_TOKEN` | Discord bot token |
| `BOT_WEB_URL` | Web app URL for bot links |
| `GCS_BUCKET` | GCloud Storage bucket |
| `GCS_PREFIX` | Snapshot prefix (e.g. `snapshots/`) |
| `GCS_CREDENTIALS_FILE` | Service account JSON |
| `PHOTO_CACHE_DIR` | Local photo cache (default `./cache/photos`) |
| `PHOTO_CACHE_SIZE` | Max cache MB (default `500`) |
| `POSTGRES_*` | Not used — memdb only |
