# AGENTS.md

## Project Overview

Grocer is a family-shared grocery receipt tracker. Users photograph receipts (via web upload, Telegram, or Discord), an LLM parses them into structured data, and the system tracks spending by category, merchant, and family member over time.

## Architecture

**Modular monolith** — single Go binary, clean internal package boundaries.

- `cmd/server/` — entry point, wiring
- `internal/domain/` — core types (User, Receipt, Item, Merchant, Category), no dependencies
- `internal/store/` — memdb wrapper, GCloud snapshot pull/push
- `internal/llm/` — provider interface + implementations (Kimi, Qwen)
- `internal/receipt/` — parsing orchestration (photo → LLM → proposal → approval)
- `internal/bot/` — Telegram + Discord bot handlers
- `internal/api/` — HTTP handlers, routing
- `internal/photo/` — GCloud storage + local LRU cache
- `client/` — VanJS frontend + Chart.js

## Data Model

Protobuf-based, normalized, compact for GCloud snapshots. Key entities:
- **User** — family members, CLI-created only
- **Category** — hierarchical via `parentId`, predefined + user-created
- **Item** — `normalized` for grouping similar items, `aliases` for LLM variations
- **Receipt** — `ownerId` tags who bought it, `date` as Unix timestamp
- **ReceiptItem** — references `itemId` only (no denormalization)

Full schema: `proto/grocer.proto`

## Building and Running

### Backend

```bash
# Development (hot reload)
mise run start_server

# Production build
mise run build_server
./dist/server
```

Requires Go 1.25+. Entry point: `cmd/server/main.go`

### Frontend

```bash
bun install
mise run build_client       # dev build
mise run build_client_prod  # production build
```

Built with Bun to `dist/`. Served by the Go server.

### Create users

```bash
go run cmd/server/main.go --create-user --name "Dad" --username dad --password secret
```

## Development Conventions

### Backend

- Go standard library patterns, no heavy frameworks
- memdb for in-memory storage — single process, no external DB
- Protobuf for snapshot serialization (compact, gzip-compressed)
- Argon2id for password hashing
- Timestamp-based UIDs (`internal/store/id-gen.go`)
- Errors returned, not panicked (except startup snapshot failure → crash)

### Frontend

- VanJS for reactive UI — see `vanjs_skill.md` for patterns
- Chart.js for analysis charts
- CSS in separate files per component
- API calls via `client/api.ts` wrapper with auth token

### LLM Integration

- Provider interface in `internal/llm/llm.go`
- `ParseReceipt(photo)` → structured `ParsedReceipt`
- `CategorizeItem(name, categories)` → suggested category (new items only)
- Two implementations: Kimi (OpenAI-compatible), Qwen (Anthropic-compatible)
- Configured via `LLM_PROVIDER`, `LLM_API_KEY`, `LLM_MODEL` env vars

### Receipt Parsing Flow

1. Photo → store → LLM parse → `ParsedReceipt`
2. Fuzzy match items against catalog (normalized + aliases)
3. ≥99% confidence → auto-match
4. >80% confidence → user review
5. ≤80% → new item, LLM suggests category
6. User approves proposal → commit to store
7. User corrections become aliases (learning over time)

### Bots

- Telegram + Discord, photo input only
- Parse receipt, send summary with link to web app for approval
- Map bot user IDs to internal `userId`

### Persistence

- memdb as primary store
- Snapshot to GCloud on every write (full state, gzip protobuf)
- Pull snapshot on startup — crash if pull fails
- Photos: GCloud primary, local LRU cache (500MB default)

## Config (Environment Variables)

| Variable | Required | Description |
|----------|----------|-------------|
| `LLM_PROVIDER` | Yes | `kimi` or `qwen` |
| `LLM_API_KEY` | Yes | API key |
| `LLM_MODEL` | Yes | Model ID |
| `GCS_BUCKET` | Yes | GCloud Storage bucket |
| `GCS_PREFIX` | No | Snapshot prefix (default: `snapshots/`) |
| `GCS_CREDENTIALS_FILE` | Yes | Service account JSON path |
| `TELEGRAM_BOT_TOKEN` | No | Telegram bot token |
| `DISCORD_BOT_TOKEN` | No | Discord bot token |
| `BOT_WEB_URL` | Yes | Web app URL |
| `PHOTO_CACHE_DIR` | No | Local cache (default: `./cache/photos`) |
| `PHOTO_CACHE_SIZE` | No | Max cache MB (default: `500`) |

## Key Design Decisions

- **No PostgreSQL** — memdb only, synced via GCloud snapshots
- **No user registration** — users created via CLI
- **Family-shared** — everyone sees all receipts, tagged by owner
- **Hierarchical categories** — folder-like with parent-child, rollup sums
- **Normalized protobuf** — no denormalization, compact dumps
- **Snapshot on every write** — simple, last-write-wins (acceptable for family tool)
- **Fail fast** — crash if snapshot pull fails on startup

## Design Spec

Full design document: `docs/superpowers/specs/2026-06-01-grocer-design.md`
