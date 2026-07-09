# Grocer

A family-shared grocery receipt tracker with LLM-powered parsing, category management, and spending analysis.

## Features

- **Receipt parsing** — photograph receipts, LLM extracts structured data automatically
- **Smart matching** — matches items against your catalog, learns aliases over time
- **Category management** — hierarchical categories, LLM-suggested for new items
- **Family sharing** — all receipts visible to everyone, tagged by purchaser
- **Spending analysis** — trends over time, category breakdown, per-member spending
- **Multi-channel input** — upload via web, Telegram bot, or Discord bot

## Architecture

Modular monolith — single Go binary with clean internal package boundaries.

```
grocer/
├── cmd/server/          # Entry point
├── internal/
│   ├── domain/          # Core types (User, Receipt, Item, Merchant, Category)
│   ├── store/           # memdb + GCloud snapshot persistence
│   ├── llm/             # Provider interface (Kimi, Qwen, etc.)
│   ├── receipt/         # Parsing orchestration
│   ├── bot/             # Telegram + Discord handlers
│   ├── api/             # HTTP API
│   └── photo/           # GCloud storage + local cache
├── client/              # VanJS frontend
└── deploy/              # Docker setup
```

## Prerequisites

- Go 1.25+
- Bun (for frontend)
- GCloud Storage bucket
- LLM API key (Kimi or Qwen via [opencode.ai](https://opencode.ai))
- Telegram and/or Discord bot tokens (optional)

## Setup

### 1. Clone and configure

```bash
git clone https://github.com/your-username/grocer.git
cd grocer
cp .env.example .env
# Edit .env with your values
```

### 2. Create users

```bash
go run cmd/server/main.go --create-user --name "Dad" --username dad --password secret
```

### 3. Run

```bash
# Development (with hot reload)
mise run start_server

# Production
mise run build_server
./dist/server
```

Server starts on `:8080`.

### 4. Build frontend

```bash
bun install
mise run build_client
```

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `LLM_PROVIDER` | Yes | `kimi`, `qwen`, or `minimax` |
| `LLM_API_KEY` | Yes | API key for LLM provider |
| `LLM_MODEL` | Yes | Model ID (e.g. `kimi-k2.6`, `qwen3.6-plus`, `minimax-m3`) |
| `OCR_PROVIDER` | No | OCR engine. `mistral` (default) or `none` to disable |
| `MISTRAL_API_KEY` | When `OCR_PROVIDER=mistral` | Mistral API key for OCR 4 |
| `MISTRAL_OCR_MODEL` | No | OCR model (default `mistral-ocr-4-0`) |
| `GCS_BUCKET` | Yes | GCloud Storage bucket name |
| `GCS_PREFIX` | No | Snapshot prefix (default: `snapshots/`) |
| `GCS_CREDENTIALS_FILE` | Yes | Path to GCloud service account JSON |
| `TELEGRAM_BOT_TOKEN` | No | Telegram bot token |
| `DISCORD_BOT_TOKEN` | No | Discord bot token |
| `BOT_WEB_URL` | Yes | Web app URL (for bot message links) |
| `PHOTO_CACHE_DIR` | No | Local photo cache (default: `./cache/photos`) |
| `PHOTO_CACHE_SIZE` | No | Max cache size in MB (default: `500`) |

## Receipt parsing pipeline

Receipts are parsed in two stages when `OCR_PROVIDER=mistral` (the default):

1. **OCR** — Mistral OCR 4 extracts text, structural blocks, and per-word confidence from the photo. The markdown is stored on the proposal and a `parsed_ocr` status is set.
2. **LLM extraction** — The OCR markdown is passed to `LLM_PROVIDER` (`minimax-m3`, `qwen3.6-plus`, or `kimi-k2.6`) which extracts merchant, date, and line items as JSON. A `parsed_llm` status is set, then `pending` when ready for review.

When `OCR_PROVIDER=none` the legacy single-stage path is used: the photo is sent directly to the LLM as a multimodal message.

Per-item auto-match to the catalog is gated on OCR confidence: items whose OCR confidence is below 0.85 are always routed to human review, even when the parsed name matches an existing item.

## API

See [design spec](docs/superpowers/specs/2026-06-01-grocer-design.md#7-http-api) for full API reference.

**Key endpoints:**

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/auth/login` | Login, get session token |
| GET | `/api/receipts` | List receipts (filter by date, owner, category) |
| POST | `/api/receipts/upload` | Upload receipt photo → proposal |
| GET | `/api/proposals` | List pending proposals |
| POST | `/api/proposals/:id/approve` | Approve proposal → receipt |
| GET | `/api/analysis/spending` | Spending over time |
| GET | `/api/analysis/categories` | Category breakdown |
| GET | `/api/analysis/family` | Per-member spending |

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Backend | Go, memdb, protobuf |
| Frontend | VanJS, Chart.js, TypeScript |
| LLM | Kimi K2.6 / Qwen3.6 Plus (via opencode.ai) |
| Storage | GCloud Storage (snapshots + photos) |
| Bots | Telegram Bot API, Discord Bot API |
| Build | Bun, Mise |

## License

Private — family use only.
