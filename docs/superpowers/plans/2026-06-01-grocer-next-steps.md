# Grocer — Next Steps Roadmap

## Current State

Core functionality is implemented:
- ✅ Domain types, memdb store, ID generation
- ✅ LLM interface with Kimi/Qwen providers
- ✅ Receipt parsing with fuzzy item matching
- ✅ HTTP API with all endpoints
- ✅ Telegram + Discord bots
- ✅ VanJS frontend with Chart.js
- ✅ Server entry point with CLI

## Gaps to Close

### Phase 1: Persistence & Storage

| Task | Priority | Effort |
|------|----------|--------|
| GCloud snapshot pull on startup | Critical | 1h |
| GCloud snapshot push on every write | Critical | 1h |
| Photo upload to GCloud | High | 2h |
| Photo serving with local cache | Medium | 1h |

**Why:** Without snapshot persistence, all data is lost on restart. Photos need to be stored for receipt reference.

### Phase 2: Bot Integration Polish

| Task | Priority | Effort |
|------|----------|--------|
| Bot user ID → internal user mapping | High | 2h |
| Bot proposal notification with deep link | Medium | 1h |
| Bot error handling and retry | Medium | 1h |

**Why:** Bots need to know which family member sent the receipt. Currently falls back to first user.

### Phase 3: Analysis Enhancements

| Task | Priority | Effort |
|------|----------|--------|
| Date range filters on analysis page | High | 1h |
| Per-member spending breakdown | High | 1h |
| Item price tracking over time | Medium | 2h |
| Merchant comparison view | Low | 2h |
| Similar item comparison | Low | 3h |

**Why:** Core analysis features (spending trends, category breakdown) work. Bonus features need more UI.

### Phase 4: UX Polish

| Task | Priority | Effort |
|------|----------|--------|
| Receipt upload page | High | 2h |
| Proposal approval with item details | High | 2h |
| Category drag-and-drop reorder | Medium | 2h |
| Item edit with alias management | Medium | 1h |
| Mobile responsive layout | Medium | 2h |

**Why:** Core flows work but need polish for daily use.

### Phase 5: Production Readiness

| Task | Priority | Effort |
|------|----------|--------|
| Docker Compose with env vars | High | 1h |
| Health check endpoint | Medium | 30m |
| Request logging | Medium | 1h |
| Error rate monitoring | Low | 1h |
| Backup/restore CLI commands | Low | 2h |

**Why:** Need to deploy and run reliably.

## Recommended Order

1. **Persistence** — Fix the critical gap (data loss on restart)
2. **Bot user mapping** — Make bots usable for family
3. **Receipt upload page** — Complete the web upload flow
4. **Analysis date filters** — Make analysis useful
5. **Production hardening** — Deploy and monitor

## Decision Points

- **GCloud vs local file for snapshots?** — GCloud for reliability, local for simplicity
- **Photo storage strategy?** — GCloud primary, local cache for speed
- **Bot user mapping approach?** — Config file vs database vs bot-specific registration
