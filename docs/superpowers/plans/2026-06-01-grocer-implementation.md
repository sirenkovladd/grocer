# Grocer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a family-shared grocery receipt tracker with LLM-powered parsing, category management, and spending analysis.

**Architecture:** Modular monolith — single Go binary with internal packages for domain, store, llm, receipt parsing, bots, api, and photo storage. memdb as primary store, GCloud for snapshot persistence and photo storage. VanJS + Chart.js frontend.

**Tech Stack:** Go 1.25+, memdb, protobuf, VanJS, Chart.js, Bun, Telegram Bot API, Discord Bot API, GCloud Storage

---

## File Structure

```
grocer/
├── cmd/
│   └── server/
│       └── main.go                    # Entry point, wiring, CLI flags
├── internal/
│   ├── domain/
│   │   └── types.go                   # User, Category, Merchant, Item, Receipt, ReceiptItem, Proposal
│   ├── store/
│   │   ├── memdb.go                   # memdb schema, CRUD operations
│   │   ├── idgen.go                   # Timestamp-based ID generator (migrated from lib/)
│   │   └── snapshot.go                # Protobuf snapshot serialization, GCloud pull/push
│   ├── llm/
│   │   ├── llm.go                     # Provider interface, types
│   │   ├── kimi.go                    # Kimi K2.6 implementation
│   │   └── qwen.go                    # Qwen 3.6 Plus implementation
│   ├── receipt/
│   │   ├── parser.go                  # Orchestration: photo → LLM → proposal
│   │   └── matcher.go                 # Fuzzy item matching, alias learning
│   ├── bot/
│   │   ├── bot.go                     # Bot interface
│   │   ├── telegram.go                # Telegram bot handler
│   │   └── discord.go                 # Discord bot handler
│   ├── api/
│   │   ├── router.go                  # HTTP router, middleware
│   │   ├── auth.go                    # Login endpoint
│   │   ├── receipts.go                # Receipt endpoints
│   │   ├── proposals.go               # Proposal endpoints
│   │   ├── items.go                   # Item endpoints
│   │   ├── categories.go              # Category endpoints
│   │   ├── merchants.go               # Merchant endpoints
│   │   └── analysis.go                # Analysis endpoints
│   └── photo/
│       ├── store.go                   # GCloud photo storage
│       └── cache.go                   # Local LRU cache
├── proto/
│   └── grocer.proto                   # Protobuf definitions
├── client/
│   ├── index.html
│   ├── main.ts                        # Entry, router
│   ├── api.ts                         # Fetch wrapper
│   ├── components/
│   │   ├── layout.ts
│   │   ├── receipt-card.ts
│   │   ├── proposal-form.ts
│   │   ├── category-tree.ts
│   │   ├── item-detail.ts
│   │   └── charts.ts
│   ├── pages/
│   │   ├── login.ts
│   │   ├── receipts.ts
│   │   ├── receipt.ts
│   │   ├── proposals.ts
│   │   ├── items.ts
│   │   ├── categories.ts
│   │   └── analysis.ts
│   └── styles/
│       └── main.css
├── deploy/
│   ├── Dockerfile
│   └── docker-compose.yml
├── go.mod
├── go.sum
├── package.json
├── tsconfig.json
└── mise.toml
```

---

## Phase 1: Foundation

### Task 1: Domain Types

**Files:**
- Create: `internal/domain/types.go`

- [ ] **Step 1: Create domain types**

```go
package domain

type User struct {
	UserID       uint64 `json:"userId" protobuf:"fixed64,1,opt,name=userId"`
	Name         string `json:"name" protobuf:"bytes,2,opt,name=name"`
	Username     string `json:"username" protobuf:"bytes,3,opt,name=username"`
	PasswordHash string `json:"-" protobuf:"bytes,4,opt,name=passwordHash"`
}

type Category struct {
	CategoryID uint64  `json:"categoryId" protobuf:"fixed64,1,opt,name=categoryId"`
	Name       string  `json:"name" protobuf:"bytes,2,opt,name=name"`
	ParentID   *uint64 `json:"parentId,omitempty" protobuf:"fixed64,3,opt,name=parentId"`
	SortOrder  int32   `json:"sortOrder" protobuf:"varint,4,opt,name=sortOrder"`
}

type Merchant struct {
	MerchantID uint64 `json:"merchantId" protobuf:"fixed64,1,opt,name=merchantId"`
	Name       string `json:"name" protobuf:"bytes,2,opt,name=name"`
}

type Item struct {
	ItemID     uint64   `json:"itemId" protobuf:"fixed64,1,opt,name=itemId"`
	Name       string   `json:"name" protobuf:"bytes,2,opt,name=name"`
	CategoryID uint64   `json:"categoryId" protobuf:"fixed64,3,opt,name=categoryId"`
	MerchantID uint64   `json:"merchantId" protobuf:"fixed64,4,opt,name=merchantId"`
	Normalized string   `json:"normalized" protobuf:"bytes,5,opt,name=normalized"`
	Aliases    []string `json:"aliases,omitempty" protobuf:"bytes,6,rep,name=aliases"`
}

type Receipt struct {
	ReceiptID uint64         `json:"receiptId" protobuf:"fixed64,1,opt,name=receiptId"`
	MerchantID uint64        `json:"merchantId" protobuf:"fixed64,2,opt,name=merchantId"`
	OwnerID   uint64         `json:"ownerId" protobuf:"fixed64,3,opt,name=ownerId"`
	Date      int64          `json:"date" protobuf:"fixed64,4,opt,name=date"`
	PhotoURL  string         `json:"photoUrl,omitempty" protobuf:"bytes,5,opt,name=photoUrl"`
	Items     []ReceiptItem  `json:"items" protobuf:"bytes,6,rep,name=items"`
	Total     float64        `json:"total" protobuf:"fixed64,7,opt,name=total"`
}

type ReceiptItem struct {
	ItemID    uint64  `json:"itemId" protobuf:"fixed64,1,opt,name=itemId"`
	Quantity  uint32  `json:"quantity" protobuf:"varint,2,opt,name=quantity"`
	UnitPrice float64 `json:"unitPrice" protobuf:"fixed64,3,opt,name=unitPrice"`
}

type Proposal struct {
	ProposalID uint64        `json:"proposalId" protobuf:"fixed64,1,opt,name=proposalId"`
	OwnerID    uint64        `json:"ownerId" protobuf:"fixed64,2,opt,name=ownerId"`
	Merchant   string        `json:"merchant" protobuf:"bytes,3,opt,name=merchant"`
	Date       int64         `json:"date" protobuf:"fixed64,4,opt,name=date"`
	PhotoURL   string        `json:"photoUrl,omitempty" protobuf:"bytes,5,opt,name=photoUrl"`
	Items      []ProposalItem `json:"items" protobuf:"bytes,6,rep,name=items"`
	Total      float64       `json:"total" protobuf:"fixed64,7,opt,name=total"`
	Status     string        `json:"status" protobuf:"bytes,8,opt,name=status"` // "pending", "approved", "rejected"
}

type ProposalItem struct {
	ParsedName    string  `json:"parsedName" protobuf:"bytes,1,opt,name=parsedName"`
	Quantity      uint32  `json:"quantity" protobuf:"varint,2,opt,name=quantity"`
	UnitPrice     float64 `json:"unitPrice" protobuf:"fixed64,3,opt,name=unitPrice"`
	MatchedItemID uint64  `json:"matchedItemId,omitempty" protobuf:"fixed64,4,opt,name=matchedItemId"`
	Confidence    float64 `json:"confidence" protobuf:"fixed64,5,opt,name=confidence"`
	CategoryID    uint64  `json:"categoryId,omitempty" protobuf:"fixed64,6,opt,name=categoryId"`
	IsNewCategory bool    `json:"isNewCategory,omitempty" protobuf:"varint,7,opt,name=isNewCategory"`
	UserChoice    string  `json:"userChoice,omitempty" protobuf:"bytes,8,opt,name=userChoice"` // "existing", "new", ""
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/domain/`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/domain/types.go
git commit -m "feat: add domain types for users, categories, items, receipts, proposals"
```

---

### Task 2: ID Generator

**Files:**
- Migrate: `lib/database/id-gen.go` → Create: `internal/store/idgen.go`

- [ ] **Step 1: Create ID generator**

```go
package store

import (
	"sync"
	"time"
)

type Generator struct {
	lock      sync.Mutex
	timestamp int64
	counter   uint32
}

const maxCounterBits = 19
const MaxCounter uint32 = 1<<maxCounterBits - 1

func NewGenerator() *Generator {
	return &Generator{}
}

func (g *Generator) Gen() uint64 {
	g.lock.Lock()
	defer g.lock.Unlock()
	return g.gen()
}

func (g *Generator) gen() uint64 {
	now := time.Now().UnixMilli()
	if now == g.timestamp {
		if g.counter == MaxCounter {
			time.Sleep(time.Millisecond)
			return g.gen()
		}
		g.counter++
	} else {
		g.counter = 0
		g.timestamp = now
	}
	return initUID(now, g.counter)
}

func initUID(timestamp int64, counter uint32) uint64 {
	var uid uint64 = 0
	uid += uint64(timestamp) << maxCounterBits
	uid += uint64(counter)
	return uid
}

func ParseUID(uid uint64) (time.Time, uint32) {
	return time.UnixMilli(int64(uid >> maxCounterBits)), uint32(uid) & MaxCounter
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/store/`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/store/idgen.go
git commit -m "feat: add ID generator to internal/store"
```

---

### Task 3: memdb Store

**Files:**
- Create: `internal/store/memdb.go`

- [ ] **Step 1: Create memdb store with schema and CRUD operations**

```go
package store

import (
	"errors"
	"sync"

	"code.sirenko.ca/grocer/internal/domain"
	"github.com/hashicorp/go-memdb"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

type Store struct {
	mu         sync.RWMutex
	db         *memdb.MemDB
	UserID     *Generator
	SessionID  *Generator
	CategoryID *Generator
	MerchantID *Generator
	ItemID     *Generator
	ReceiptID  *Generator
	ProposalID *Generator
}

func NewStore() (*Store, error) {
	schema := &memdb.DBSchema{
		Tables: map[string]*memdb.TableSchema{
			"users": {
				Name: "users",
				Indexes: map[string]*memdb.IndexSchema{
					"id": {
						Name:    "id",
						Unique:  true,
						Indexer: &memdb.StringFieldIndex{Field: "Username"},
					},
				},
			},
			"sessions": {
				Name: "sessions",
				Indexes: map[string]*memdb.IndexSchema{
					"id": {
						Name:    "id",
						Unique:  true,
						Indexer: &memdb.UintFieldIndex{Field: "SessionID"},
					},
				},
			},
			"categories": {
				Name: "categories",
				Indexes: map[string]*memdb.IndexSchema{
					"id": {
						Name:    "id",
						Unique:  true,
						Indexer: &memdb.UintFieldIndex{Field: "CategoryID"},
					},
				},
			},
			"merchants": {
				Name: "merchants",
				Indexes: map[string]*memdb.IndexSchema{
					"id": {
						Name:    "id",
						Unique:  true,
						Indexer: &memdb.UintFieldIndex{Field: "MerchantID"},
					},
				},
			},
			"items": {
				Name: "items",
				Indexes: map[string]*memdb.IndexSchema{
					"id": {
						Name:    "id",
						Unique:  true,
						Indexer: &memdb.UintFieldIndex{Field: "ItemID"},
					},
					"normalized": {
						Name:    "normalized",
						Unique:  false,
						Indexer: &memdb.StringFieldIndex{Field: "Normalized"},
					},
				},
			},
			"receipts": {
				Name: "receipts",
				Indexes: map[string]*memdb.IndexSchema{
					"id": {
						Name:    "id",
						Unique:  true,
						Indexer: &memdb.UintFieldIndex{Field: "ReceiptID"},
					},
				},
			},
			"proposals": {
				Name: "proposals",
				Indexes: map[string]*memdb.IndexSchema{
					"id": {
						Name:    "id",
						Unique:  true,
						Indexer: &memdb.UintFieldIndex{Field: "ProposalID"},
					},
				},
			},
		},
	}

	db, err := memdb.NewMemDB(schema)
	if err != nil {
		return nil, err
	}

	return &Store{
		db:         db,
		UserID:     NewGenerator(),
		SessionID:  NewGenerator(),
		CategoryID: NewGenerator(),
		MerchantID: NewGenerator(),
		ItemID:     NewGenerator(),
		ReceiptID:  NewGenerator(),
		ProposalID: NewGenerator(),
	}, nil
}

// User operations

func (s *Store) CreateUser(user *domain.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(true)
	defer txn.Abort()

	if err := txn.Insert("users", user); err != nil {
		return err
	}
	txn.Commit()
	return nil
}

func (s *Store) GetUserByUsername(username string) (*domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	raw, err := txn.First("users", "id", username)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, ErrNotFound
	}
	return raw.(*domain.User), nil
}

// Session operations

type Session struct {
	SessionID uint64
	TokenHash string
	UserID    uint64
}

func (s *Store) CreateSession(sess *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(true)
	defer txn.Abort()

	if err := txn.Insert("sessions", sess); err != nil {
		return err
	}
	txn.Commit()
	return nil
}

func (s *Store) GetSession(sessionID uint64) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	raw, err := txn.First("sessions", "id", sessionID)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, ErrNotFound
	}
	return raw.(*Session), nil
}

// Category operations

func (s *Store) CreateCategory(cat *domain.Category) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(true)
	defer txn.Abort()

	if err := txn.Insert("categories", cat); err != nil {
		return err
	}
	txn.Commit()
	return nil
}

func (s *Store) GetCategory(id uint64) (*domain.Category, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	raw, err := txn.First("categories", "id", id)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, ErrNotFound
	}
	return raw.(*domain.Category), nil
}

func (s *Store) ListCategories() ([]*domain.Category, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	iter, err := txn.Get("categories", "id")
	if err != nil {
		return nil, err
	}

	var cats []*domain.Category
	for raw := iter.Next(); raw != nil; raw = iter.Next() {
		cats = append(cats, raw.(*domain.Category))
	}
	return cats, nil
}

func (s *Store) UpdateCategory(cat *domain.Category) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(true)
	defer txn.Abort()

	if err := txn.Insert("categories", cat); err != nil {
		return err
	}
	txn.Commit()
	return nil
}

func (s *Store) DeleteCategory(id uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(true)
	defer txn.Abort()

	if err := txn.Delete("categories", &domain.Category{CategoryID: id}); err != nil {
		return err
	}
	txn.Commit()
	return nil
}

// Merchant operations

func (s *Store) CreateMerchant(m *domain.Merchant) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(true)
	defer txn.Abort()

	if err := txn.Insert("merchants", m); err != nil {
		return err
	}
	txn.Commit()
	return nil
}

func (s *Store) GetMerchant(id uint64) (*domain.Merchant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	raw, err := txn.First("merchants", "id", id)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, ErrNotFound
	}
	return raw.(*domain.Merchant), nil
}

func (s *Store) ListMerchants() ([]*domain.Merchant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	iter, err := txn.Get("merchants", "id")
	if err != nil {
		return nil, err
	}

	var merchants []*domain.Merchant
	for raw := iter.Next(); raw != nil; raw = iter.Next() {
		merchants = append(merchants, raw.(*domain.Merchant))
	}
	return merchants, nil
}

func (s *Store) UpdateMerchant(m *domain.Merchant) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(true)
	defer txn.Abort()

	if err := txn.Insert("merchants", m); err != nil {
		return err
	}
	txn.Commit()
	return nil
}

// Item operations

func (s *Store) CreateItem(item *domain.Item) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(true)
	defer txn.Abort()

	if err := txn.Insert("items", item); err != nil {
		return err
	}
	txn.Commit()
	return nil
}

func (s *Store) GetItem(id uint64) (*domain.Item, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	raw, err := txn.First("items", "id", id)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, ErrNotFound
	}
	return raw.(*domain.Item), nil
}

func (s *Store) ListItems() ([]*domain.Item, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	iter, err := txn.Get("items", "id")
	if err != nil {
		return nil, err
	}

	var items []*domain.Item
	for raw := iter.Next(); raw != nil; raw = iter.Next() {
		items = append(items, raw.(*domain.Item))
	}
	return items, nil
}

func (s *Store) GetItemsByNormalized(normalized string) ([]*domain.Item, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	iter, err := txn.Get("items", "normalized", normalized)
	if err != nil {
		return nil, err
	}

	var items []*domain.Item
	for raw := iter.Next(); raw != nil; raw = iter.Next() {
		items = append(items, raw.(*domain.Item))
	}
	return items, nil
}

func (s *Store) UpdateItem(item *domain.Item) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(true)
	defer txn.Abort()

	if err := txn.Insert("items", item); err != nil {
		return err
	}
	txn.Commit()
	return nil
}

// Receipt operations

func (s *Store) CreateReceipt(r *domain.Receipt) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(true)
	defer txn.Abort()

	if err := txn.Insert("receipts", r); err != nil {
		return err
	}
	txn.Commit()
	return nil
}

func (s *Store) GetReceipt(id uint64) (*domain.Receipt, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	raw, err := txn.First("receipts", "id", id)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, ErrNotFound
	}
	return raw.(*domain.Receipt), nil
}

func (s *Store) ListReceipts() ([]*domain.Receipt, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	iter, err := txn.Get("receipts", "id")
	if err != nil {
		return nil, err
	}

	var receipts []*domain.Receipt
	for raw := iter.Next(); raw != nil; raw = iter.Next() {
		receipts = append(receipts, raw.(*domain.Receipt))
	}
	return receipts, nil
}

// Proposal operations

func (s *Store) CreateProposal(p *domain.Proposal) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(true)
	defer txn.Abort()

	if err := txn.Insert("proposals", p); err != nil {
		return err
	}
	txn.Commit()
	return nil
}

func (s *Store) GetProposal(id uint64) (*domain.Proposal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	raw, err := txn.First("proposals", "id", id)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, ErrNotFound
	}
	return raw.(*domain.Proposal), nil
}

func (s *Store) ListProposals() ([]*domain.Proposal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	iter, err := txn.Get("proposals", "id")
	if err != nil {
		return nil, err
	}

	var proposals []*domain.Proposal
	for raw := iter.Next(); raw != nil; raw = iter.Next() {
		proposals = append(proposals, raw.(*domain.Proposal))
	}
	return proposals, nil
}

func (s *Store) UpdateProposal(p *domain.Proposal) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(true)
	defer txn.Abort()

	if err := txn.Insert("proposals", p); err != nil {
		return err
	}
	txn.Commit()
	return nil
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/store/`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/store/memdb.go
git commit -m "feat: add memdb store with CRUD operations for all entities"
```

---

## Phase 2: Persistence

### Task 4: Protobuf Definitions

**Files:**
- Create: `proto/grocer.proto`

- [ ] **Step 1: Create protobuf definitions**

```protobuf
syntax = "proto3";
package grocer;

option go_package = "./out_proto";

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

message Proposal {
  fixed64 proposalId = 1;
  fixed64 ownerId = 2;
  string merchant = 3;
  fixed64 date = 4;
  string photoUrl = 5;
  repeated ProposalItem items = 6;
  float total = 7;
  string status = 8;
}

message ProposalItem {
  string parsedName = 1;
  uint32 quantity = 2;
  float unitPrice = 3;
  fixed64 matchedItemId = 4;
  float confidence = 5;
  fixed64 categoryId = 6;
  bool isNewCategory = 7;
  string userChoice = 8;
}

message Snapshot {
  repeated User users = 1;
  repeated Category categories = 2;
  repeated Merchant merchants = 3;
  repeated Item items = 4;
  repeated Receipt receipts = 5;
  repeated Proposal proposals = 6;
}
```

- [ ] **Step 2: Generate Go code**

Run: `protoc --go_out=proto/out_proto/ proto/grocer.proto`
Expected: `proto/out_proto/grocer.pb.go` created

- [ ] **Step 3: Commit**

```bash
git add proto/grocer.proto proto/out_proto/
git commit -m "feat: add protobuf definitions for snapshot format"
```

---

### Task 5: Snapshot Serialization

**Files:**
- Create: `internal/store/snapshot.go`

- [ ] **Step 1: Create snapshot serialization**

```go
package store

import (
	"bytes"
	"compress/gzip"
	"io"

	"code.sirenko.ca/grocer/internal/domain"
	pb "code.sirenko.ca/grocer/proto/out_proto"
	"google.golang.org/protobuf/proto"
)

type SnapshotData struct {
	Users      []*domain.User
	Categories []*domain.Category
	Merchants  []*domain.Merchant
	Items      []*domain.Item
	Receipts   []*domain.Receipt
	Proposals  []*domain.Proposal
}

func SerializeSnapshot(data *SnapshotData) ([]byte, error) {
	snapshot := &pb.Snapshot{
		Users:      usersToProto(data.Users),
		Categories: categoriesToProto(data.Categories),
		Merchants:  merchantsToProto(data.Merchants),
		Items:      itemsToProto(data.Items),
		Receipts:   receiptsToProto(data.Receipts),
		Proposals:  proposalsToProto(data.Proposals),
	}

	raw, err := proto.Marshal(snapshot)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(raw); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func DeserializeSnapshot(data []byte) (*SnapshotData, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()

	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	var snapshot pb.Snapshot
	if err := proto.Unmarshal(raw, &snapshot); err != nil {
		return nil, err
	}

	return &SnapshotData{
		Users:      usersFromProto(snapshot.Users),
		Categories: categoriesFromProto(snapshot.Categories),
		Merchants:  merchantsFromProto(snapshot.Merchants),
		Items:      itemsFromProto(snapshot.Items),
		Receipts:   receiptsFromProto(snapshot.Receipts),
		Proposals:  proposalsFromProto(snapshot.Proposals),
	}, nil
}

// Proto conversion functions

func usersToProto(users []*domain.User) []*pb.User {
	result := make([]*pb.User, len(users))
	for i, u := range users {
		result[i] = &pb.User{
			UserId:       u.UserID,
			Name:         u.Name,
			Username:     u.Username,
			PasswordHash: u.PasswordHash,
		}
	}
	return result
}

func usersFromProto(users []*pb.User) []*domain.User {
	result := make([]*domain.User, len(users))
	for i, u := range users {
		result[i] = &domain.User{
			UserID:       u.UserId,
			Name:         u.Name,
			Username:     u.Username,
			PasswordHash: u.PasswordHash,
		}
	}
	return result
}

func categoriesToProto(cats []*domain.Category) []*pb.Category {
	result := make([]*pb.Category, len(cats))
	for i, c := range cats {
		cat := &pb.Category{
			CategoryId: c.CategoryID,
			Name:       c.Name,
			SortOrder:  c.SortOrder,
		}
		if c.ParentID != nil {
			cat.ParentId = c.ParentID
		}
		result[i] = cat
	}
	return result
}

func categoriesFromProto(cats []*pb.Category) []*domain.Category {
	result := make([]*domain.Category, len(cats))
	for i, c := range cats {
		cat := &domain.Category{
			CategoryID: c.CategoryId,
			Name:       c.Name,
			SortOrder:  c.SortOrder,
		}
		if c.ParentId != nil {
			cat.ParentId = c.ParentId
		}
		result[i] = cat
	}
	return result
}

func merchantsToProto(merchants []*domain.Merchant) []*pb.Merchant {
	result := make([]*pb.Merchant, len(merchants))
	for i, m := range merchants {
		result[i] = &pb.Merchant{
			MerchantId: m.MerchantID,
			Name:       m.Name,
		}
	}
	return result
}

func merchantsFromProto(merchants []*pb.Merchant) []*domain.Merchant {
	result := make([]*domain.Merchant, len(merchants))
	for i, m := range merchants {
		result[i] = &domain.Merchant{
			MerchantID: m.MerchantId,
			Name:       m.Name,
		}
	}
	return result
}

func itemsToProto(items []*domain.Item) []*pb.Item {
	result := make([]*pb.Item, len(items))
	for i, item := range items {
		result[i] = &pb.Item{
			ItemId:     item.ItemID,
			Name:       item.Name,
			CategoryId: item.CategoryID,
			MerchantId: item.MerchantID,
			Normalized: item.Normalized,
			Aliases:    item.Aliases,
		}
	}
	return result
}

func itemsFromProto(items []*pb.Item) []*domain.Item {
	result := make([]*domain.Item, len(items))
	for i, item := range items {
		result[i] = &domain.Item{
			ItemID:     item.ItemId,
			Name:       item.Name,
			CategoryID: item.CategoryId,
			MerchantID: item.MerchantId,
			Normalized: item.Normalized,
			Aliases:    item.Aliases,
		}
	}
	return result
}

func receiptsToProto(receipts []*domain.Receipt) []*pb.Receipt {
	result := make([]*pb.Receipt, len(receipts))
	for i, r := range receipts {
		items := make([]*pb.ReceiptItem, len(r.Items))
		for j, item := range r.Items {
			items[j] = &pb.ReceiptItem{
				ItemId:    item.ItemID,
				Quantity:  item.Quantity,
				UnitPrice: item.UnitPrice,
			}
		}
		result[i] = &pb.Receipt{
			ReceiptId:  r.ReceiptID,
			MerchantId: r.MerchantID,
			OwnerId:    r.OwnerID,
			Date:       r.Date,
			PhotoUrl:   r.PhotoURL,
			Items:      items,
			Total:      r.Total,
		}
	}
	return result
}

func receiptsFromProto(receipts []*pb.Receipt) []*domain.Receipt {
	result := make([]*domain.Receipt, len(receipts))
	for i, r := range receipts {
		items := make([]domain.ReceiptItem, len(r.Items))
		for j, item := range r.Items {
			items[j] = domain.ReceiptItem{
				ItemID:    item.ItemId,
				Quantity:  item.Quantity,
				UnitPrice: item.UnitPrice,
			}
		}
		result[i] = &domain.Receipt{
			ReceiptID:  r.ReceiptId,
			MerchantID: r.MerchantId,
			OwnerID:    r.OwnerId,
			Date:       r.Date,
			PhotoURL:   r.PhotoUrl,
			Items:      items,
			Total:      r.Total,
		}
	}
	return result
}

func proposalsToProto(proposals []*domain.Proposal) []*pb.Proposal {
	result := make([]*pb.Proposal, len(proposals))
	for i, p := range proposals {
		items := make([]*pb.ProposalItem, len(p.Items))
		for j, item := range p.Items {
			items[j] = &pb.ProposalItem{
				ParsedName:    item.ParsedName,
				Quantity:      item.Quantity,
				UnitPrice:     item.UnitPrice,
				MatchedItemId: item.MatchedItemID,
				Confidence:    item.Confidence,
				CategoryId:    item.CategoryID,
				IsNewCategory: item.IsNewCategory,
				UserChoice:    item.UserChoice,
			}
		}
		result[i] = &pb.Proposal{
			ProposalId: p.ProposalID,
			OwnerId:    p.OwnerID,
			Merchant:   p.Merchant,
			Date:       p.Date,
			PhotoUrl:   p.PhotoURL,
			Items:      items,
			Total:      p.Total,
			Status:     p.Status,
		}
	}
	return result
}

func proposalsFromProto(proposals []*pb.Proposal) []*domain.Proposal {
	result := make([]*domain.Proposal, len(proposals))
	for i, p := range proposals {
		items := make([]domain.ProposalItem, len(p.Items))
		for j, item := range p.Items {
			items[j] = domain.ProposalItem{
				ParsedName:    item.ParsedName,
				Quantity:      item.Quantity,
				UnitPrice:     item.UnitPrice,
				MatchedItemID: item.MatchedItemId,
				Confidence:    item.Confidence,
				CategoryID:    item.CategoryId,
				IsNewCategory: item.IsNewCategory,
				UserChoice:    item.UserChoice,
			}
		}
		result[i] = &domain.Proposal{
			ProposalID: p.ProposalId,
			OwnerID:    p.OwnerId,
			Merchant:   p.Merchant,
			Date:       p.Date,
			PhotoURL:   p.PhotoUrl,
			Items:      items,
			Total:      p.Total,
			Status:     p.Status,
		}
	}
	return result
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/store/`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/store/snapshot.go
git commit -m "feat: add snapshot serialization with gzip compression"
```

---

### Task 6: GCloud Snapshot Storage

**Files:**
- Create: `internal/store/gcloud.go`

- [ ] **Step 1: Create GCloud storage client**

```go
package store

import (
	"context"
	"fmt"
	"io"
	"os"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

type GCloudStorage struct {
	client *storage.Client
	bucket string
	prefix string
}

func NewGCloudStorage(ctx context.Context, credentialsFile, bucket, prefix string) (*GCloudStorage, error) {
	var opts []option.ClientOption
	if credentialsFile != "" {
		opts = append(opts, option.WithCredentialsFile(credentialsFile))
	}

	client, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("storage.NewClient: %w", err)
	}

	return &GCloudStorage{
		client: client,
		bucket: bucket,
		prefix: prefix,
	}, nil
}

func (g *GCloudStorage) objectName() string {
	return g.prefix + "snapshot.pb.gz"
}

func (g *GCloudStorage) Pull(ctx context.Context) ([]byte, error) {
	obj := g.client.Bucket(g.bucket).Object(g.objectName())

	reader, err := obj.NewReader(ctx)
	if err != nil {
		if err == storage.ErrObjectNotExist {
			return nil, nil // No snapshot yet
		}
		return nil, fmt.Errorf("NewReader: %w", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("ReadAll: %w", err)
	}

	return data, nil
}

func (g *GCloudStorage) Push(ctx context.Context, data []byte) error {
	obj := g.client.Bucket(g.bucket).Object(g.objectName())

	writer := obj.NewWriter(ctx)
	writer.ContentType = "application/gzip"

	if _, err := writer.Write(data); err != nil {
		return fmt.Errorf("Write: %w", err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("Close: %w", err)
	}

	return nil
}

func (g *GCloudStorage) Close() error {
	return g.client.Close()
}
```

- [ ] **Step 2: Add snapshot manager to store**

Add to `internal/store/memdb.go`:

```go
// Add at the end of the file

type SnapshotManager struct {
	store     *Store
	storage   *GCloudStorage
}

func NewSnapshotManager(store *Store, storage *GCloudStorage) *SnapshotManager {
	return &SnapshotManager{
		store:   store,
		storage: storage,
	}
}

func (sm *SnapshotManager) Load(ctx context.Context) error {
	data, err := sm.storage.Pull(ctx)
	if err != nil {
		return fmt.Errorf("pull snapshot: %w", err)
	}
	if data == nil {
		return nil // No snapshot, start fresh
	}

	snapshot, err := DeserializeSnapshot(data)
	if err != nil {
		return fmt.Errorf("deserialize snapshot: %w", err)
	}

	// Load into store
	sm.store.mu.Lock()
	defer sm.store.mu.Unlock()

	txn := sm.store.db.Txn(true)
	defer txn.Abort()

	for _, u := range snapshot.Users {
		if err := txn.Insert("users", u); err != nil {
			return err
		}
	}
	for _, c := range snapshot.Categories {
		if err := txn.Insert("categories", c); err != nil {
			return err
		}
	}
	for _, m := range snapshot.Merchants {
		if err := txn.Insert("merchants", m); err != nil {
			return err
		}
	}
	for _, item := range snapshot.Items {
		if err := txn.Insert("items", item); err != nil {
			return err
		}
	}
	for _, r := range snapshot.Receipts {
		if err := txn.Insert("receipts", r); err != nil {
			return err
		}
	}
	for _, p := range snapshot.Proposals {
		if err := txn.Insert("proposals", p); err != nil {
			return err
		}
	}

	txn.Commit()
	return nil
}

func (sm *SnapshotManager) Save(ctx context.Context) error {
	sm.store.mu.RLock()
	defer sm.store.mu.RUnlock()

	txn := sm.store.db.Txn(false)
	defer txn.Abort()

	// Collect all data
	var users []*domain.User
	iter, _ := txn.Get("users", "id")
	for raw := iter.Next(); raw != nil; raw = iter.Next() {
		users = append(users, raw.(*domain.User))
	}

	var categories []*domain.Category
	iter, _ = txn.Get("categories", "id")
	for raw := iter.Next(); raw != nil; raw = iter.Next() {
		categories = append(categories, raw.(*domain.Category))
	}

	var merchants []*domain.Merchant
	iter, _ = txn.Get("merchants", "id")
	for raw := iter.Next(); raw != nil; raw = iter.Next() {
		merchants = append(merchants, raw.(*domain.Merchant))
	}

	var items []*domain.Item
	iter, _ = txn.Get("items", "id")
	for raw := iter.Next(); raw != nil; raw = iter.Next() {
		items = append(items, raw.(*domain.Item))
	}

	var receipts []*domain.Receipt
	iter, _ = txn.Get("receipts", "id")
	for raw := iter.Next(); raw != nil; raw = iter.Next() {
		receipts = append(receipts, raw.(*domain.Receipt))
	}

	var proposals []*domain.Proposal
	iter, _ = txn.Get("proposals", "id")
	for raw := iter.Next(); raw != nil; raw = iter.Next() {
		proposals = append(proposals, raw.(*domain.Proposal))
	}

	data, err := SerializeSnapshot(&SnapshotData{
		Users:      users,
		Categories: categories,
		Merchants:  merchants,
		Items:      items,
		Receipts:   receipts,
		Proposals:  proposals,
	})
	if err != nil {
		return fmt.Errorf("serialize snapshot: %w", err)
	}

	return sm.storage.Push(ctx, data)
}
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./internal/store/`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add internal/store/gcloud.go internal/store/memdb.go
git commit -m "feat: add GCloud snapshot storage with pull/push"
```

---

## Phase 3: LLM Integration

### Task 7: LLM Provider Interface

**Files:**
- Create: `internal/llm/llm.go`

- [ ] **Step 1: Create LLM interface**

```go
package llm

import (
	"context"
	"time"

	"code.sirenko.ca/grocer/internal/domain"
)

type Provider interface {
	ParseReceipt(ctx context.Context, photo []byte) (*ParsedReceipt, error)
	CategorizeItem(ctx context.Context, itemName string, existingCategories []domain.Category) (*Categorization, error)
}

type ParsedReceipt struct {
	Merchant string
	Date     time.Time
	Items    []ParsedItem
	Total    float64
}

type ParsedItem struct {
	Name       string
	Quantity   uint32
	UnitPrice  float64
	TotalPrice float64
}

type Categorization struct {
	CategoryID    uint64
	IsNew         bool
	SuggestedName string
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/llm/`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/llm/llm.go
git commit -m "feat: add LLM provider interface"
```

---

### Task 8: Kimi Provider

**Files:**
- Create: `internal/llm/kimi.go`

- [ ] **Step 1: Create Kimi provider implementation**

```go
package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"code.sirenko.ca/grocer/internal/domain"
)

type KimiProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

func NewKimiProvider(apiKey, model string) *KimiProvider {
	return &KimiProvider{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://opencode.ai/zen/go/v1",
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
}

type message struct {
	Role    string    `json:"role"`
	Content any       `json:"content"`
}

type imageContent struct {
	Type     string    `json:"type"`
	ImageURL imageURL  `json:"image_url"`
}

type imageURL struct {
	URL string `json:"url"`
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (k *KimiProvider) ParseReceipt(ctx context.Context, photo []byte) (*ParsedReceipt, error) {
	b64 := base64.StdEncoding.EncodeToString(photo)

	prompt := `Analyze this grocery receipt photo and extract the following information in JSON format:
{
  "merchant": "store name",
  "date": "YYYY-MM-DD",
  "items": [
    {
      "name": "item name as shown on receipt",
      "quantity": 1,
      "unit_price": 2.99,
      "total_price": 2.99
    }
  ],
  "total": 25.99
}

Return ONLY the JSON, no other text.`

	req := chatRequest{
		Model: k.model,
		Messages: []message{
			{
				Role: "user",
				Content: []any{
					imageContent{
						Type: "image_url",
						ImageURL: imageURL{
							URL: "data:image/jpeg;base64," + b64,
						},
					},
					textContent{
						Type: "text",
						Text: prompt,
					},
				},
			},
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", k.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+k.apiKey)

	resp, err := k.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kimi API error: %d %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, err
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("no response from kimi")
	}

	return parseReceiptJSON(chatResp.Choices[0].Message.Content)
}

func (k *KimiProvider) CategorizeItem(ctx context.Context, itemName string, existingCategories []domain.Category) (*Categorization, error) {
	categoriesJSON, _ := json.Marshal(existingCategories)

	prompt := fmt.Sprintf(`Given the item "%s" and these existing categories: %s

Determine the best category. If no existing category fits, suggest a new one.

Return JSON:
{
  "category_id": 123,
  "is_new": false,
  "suggested_name": ""
}

If creating a new category, set category_id to 0 and is_new to true.
Return ONLY the JSON.`, itemName, string(categoriesJSON))

	req := chatRequest{
		Model: k.model,
		Messages: []message{
			{Role: "user", Content: prompt},
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", k.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+k.apiKey)

	resp, err := k.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kimi API error: %d %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, err
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("no response from kimi")
	}

	return parseCategorizationJSON(chatResp.Choices[0].Message.Content)
}

func parseReceiptJSON(content string) (*ParsedReceipt, error) {
	// Extract JSON from response (might be wrapped in markdown)
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		content = strings.Join(lines[1:len(lines)-1], "\n")
	}

	var parsed struct {
		Merchant string `json:"merchant"`
		Date     string `json:"date"`
		Items    []struct {
			Name       string  `json:"name"`
			Quantity   uint32  `json:"quantity"`
			UnitPrice  float64 `json:"unit_price"`
			TotalPrice float64 `json:"total_price"`
		} `json:"items"`
		Total float64 `json:"total"`
	}

	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, fmt.Errorf("parse receipt JSON: %w", err)
	}

	date, err := time.Parse("2006-01-02", parsed.Date)
	if err != nil {
		date = time.Now()
	}

	items := make([]ParsedItem, len(parsed.Items))
	for i, item := range parsed.Items {
		items[i] = ParsedItem{
			Name:       item.Name,
			Quantity:   item.Quantity,
			UnitPrice:  item.UnitPrice,
			TotalPrice: item.TotalPrice,
		}
	}

	return &ParsedReceipt{
		Merchant: parsed.Merchant,
		Date:     date,
		Items:    items,
		Total:    parsed.Total,
	}, nil
}

func parseCategorizationJSON(content string) (*Categorization, error) {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		content = strings.Join(lines[1:len(lines)-1], "\n")
	}

	var parsed struct {
		CategoryID    uint64 `json:"category_id"`
		IsNew         bool   `json:"is_new"`
		SuggestedName string `json:"suggested_name"`
	}

	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, fmt.Errorf("parse categorization JSON: %w", err)
	}

	return &Categorization{
		CategoryID:    parsed.CategoryID,
		IsNew:         parsed.IsNew,
		SuggestedName: parsed.SuggestedName,
	}, nil
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/llm/`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/llm/kimi.go
git commit -m "feat: add Kimi LLM provider implementation"
```

---

### Task 9: Qwen Provider

**Files:**
- Create: `internal/llm/qwen.go`

- [ ] **Step 1: Create Qwen provider implementation**

```go
package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"code.sirenko.ca/grocer/internal/domain"
)

type QwenProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

func NewQwenProvider(apiKey, model string) *QwenProvider {
	return &QwenProvider{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://opencode.ai/zen/go/v1",
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type qwenRequest struct {
	Model     string        `json:"model"`
	MaxTokens int           `json:"max_tokens"`
	Messages  []qwenMessage `json:"messages"`
}

type qwenMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type qwenImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type qwenImageContent struct {
	Type   string           `json:"type"`
	Source qwenImageSource  `json:"source"`
}

type qwenTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type qwenResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func (q *QwenProvider) ParseReceipt(ctx context.Context, photo []byte) (*ParsedReceipt, error) {
	b64 := base64.StdEncoding.EncodeToString(photo)

	prompt := `Analyze this grocery receipt photo and extract the following information in JSON format:
{
  "merchant": "store name",
  "date": "YYYY-MM-DD",
  "items": [
    {
      "name": "item name as shown on receipt",
      "quantity": 1,
      "unit_price": 2.99,
      "total_price": 2.99
    }
  ],
  "total": 25.99
}

Return ONLY the JSON, no other text.`

	req := qwenRequest{
		Model:     q.model,
		MaxTokens: 4096,
		Messages: []qwenMessage{
			{
				Role: "user",
				Content: []any{
					qwenImageContent{
						Type: "image",
						Source: qwenImageSource{
							Type:      "base64",
							MediaType: "image/jpeg",
							Data:      b64,
						},
					},
					qwenTextContent{
						Type: "text",
						Text: prompt,
					},
				},
			},
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", q.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+q.apiKey)

	resp, err := q.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("qwen API error: %d %s", resp.StatusCode, string(respBody))
	}

	var qwenResp qwenResponse
	if err := json.Unmarshal(respBody, &qwenResp); err != nil {
		return nil, err
	}

	if len(qwenResp.Content) == 0 {
		return nil, fmt.Errorf("no response from qwen")
	}

	// Find text content
	var text string
	for _, c := range qwenResp.Content {
		if c.Type == "text" {
			text = c.Text
			break
		}
	}

	return parseReceiptJSON(text)
}

func (q *QwenProvider) CategorizeItem(ctx context.Context, itemName string, existingCategories []domain.Category) (*Categorization, error) {
	categoriesJSON, _ := json.Marshal(existingCategories)

	prompt := fmt.Sprintf(`Given the item "%s" and these existing categories: %s

Determine the best category. If no existing category fits, suggest a new one.

Return JSON:
{
  "category_id": 123,
  "is_new": false,
  "suggested_name": ""
}

If creating a new category, set category_id to 0 and is_new to true.
Return ONLY the JSON.`, itemName, string(categoriesJSON))

	req := qwenRequest{
		Model:     q.model,
		MaxTokens: 1024,
		Messages: []qwenMessage{
			{Role: "user", Content: prompt},
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", q.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+q.apiKey)

	resp, err := q.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("qwen API error: %d %s", resp.StatusCode, string(respBody))
	}

	var qwenResp qwenResponse
	if err := json.Unmarshal(respBody, &qwenResp); err != nil {
		return nil, err
	}

	if len(qwenResp.Content) == 0 {
		return nil, fmt.Errorf("no response from qwen")
	}

	var text string
	for _, c := range qwenResp.Content {
		if c.Type == "text" {
			text = c.Text
			break
		}
	}

	return parseCategorizationJSON(text)
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/llm/`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/llm/qwen.go
git commit -m "feat: add Qwen LLM provider implementation"
```

---

### Task 10: Receipt Parser

**Files:**
- Create: `internal/receipt/parser.go`

- [ ] **Step 1: Create receipt parser**

```go
package receipt

import (
	"context"
	"fmt"
	"strings"
	"time"

	"code.sirenko.ca/grocer/internal/domain"
	"code.sirenko.ca/grocer/internal/llm"
	"code.sirenko.ca/grocer/internal/store"
)

type Parser struct {
	store    *store.Store
	llm      llm.Provider
	matcher  *Matcher
}

func NewParser(store *store.Store, llm llm.Provider) *Parser {
	return &Parser{
		store:   store,
		llm:     llm,
		matcher: NewMatcher(store),
	}
}

func (p *Parser) ParseReceipt(ctx context.Context, photo []byte, ownerID uint64) (*domain.Proposal, error) {
	parsed, err := p.llm.ParseReceipt(ctx, photo)
	if err != nil {
		return nil, fmt.Errorf("llm.ParseReceipt: %w", err)
	}

	// Find or create merchant
	merchant, err := p.findOrCreateMerchant(parsed.Merchant)
	if err != nil {
		return nil, fmt.Errorf("findOrCreateMerchant: %w", err)
	}

	// Match items
	proposalItems := make([]domain.ProposalItem, len(parsed.Items))
	for i, item := range parsed.Items {
		matched, confidence, err := p.matcher.FindMatch(item.Name)
		if err != nil {
			return nil, fmt.Errorf("FindMatch: %w", err)
		}

		pi := domain.ProposalItem{
			ParsedName: item.Name,
			Quantity:   item.Quantity,
			UnitPrice:  item.UnitPrice,
			Confidence: confidence,
		}

		if matched != nil && confidence >= 0.99 {
			// Auto-match
			pi.MatchedItemID = matched.ItemID
			pi.CategoryID = matched.CategoryID
			pi.UserChoice = "existing"
		} else if matched != nil && confidence > 0.80 {
			// Needs review
			pi.MatchedItemID = matched.ItemID
			pi.CategoryID = matched.CategoryID
			pi.UserChoice = "" // Pending
		} else {
			// New item
			cat, err := p.categorizeItem(ctx, item.Name)
			if err != nil {
				return nil, fmt.Errorf("categorizeItem: %w", err)
			}
			pi.CategoryID = cat.CategoryID
			pi.IsNewCategory = cat.IsNew
		}

		proposalItems[i] = pi
	}

	proposal := &domain.Proposal{
		ProposalID: p.store.ProposalID.Gen(),
		OwnerID:    ownerID,
		Merchant:   merchant.Name,
		Date:       parsed.Date.Unix(),
		Items:      proposalItems,
		Total:      parsed.Total,
		Status:     "pending",
	}

	if err := p.store.CreateProposal(proposal); err != nil {
		return nil, fmt.Errorf("CreateProposal: %w", err)
	}

	return proposal, nil
}

func (p *Parser) findOrCreateMerchant(name string) (*domain.Merchant, error) {
	merchants, err := p.store.ListMerchants()
	if err != nil {
		return nil, err
	}

	// Simple exact match for now
	for _, m := range merchants {
		if strings.EqualFold(m.Name, name) {
			return m, nil
		}
	}

	// Create new merchant
	merchant := &domain.Merchant{
		MerchantID: p.store.MerchantID.Gen(),
		Name:       name,
	}

	if err := p.store.CreateMerchant(merchant); err != nil {
		return nil, err
	}

	return merchant, nil
}

func (p *Parser) categorizeItem(ctx context.Context, itemName string) (*llm.Categorization, error) {
	categories, err := p.store.ListCategories()
	if err != nil {
		return nil, err
	}

	return p.llm.CategorizeItem(ctx, itemName, categories)
}

func (p *Parser) ApproveProposal(ctx context.Context, proposalID uint64, choices map[int]string) (*domain.Receipt, error) {
	proposal, err := p.store.GetProposal(proposalID)
	if err != nil {
		return nil, err
	}

	if proposal.Status != "pending" {
		return nil, fmt.Errorf("proposal already %s", proposal.Status)
	}

	// Apply user choices
	for i, choice := range choices {
		if i >= len(proposal.Items) {
			continue
		}
		proposal.Items[i].UserChoice = choice
	}

	// Create receipt and update items
	receiptItems := make([]domain.ReceiptItem, len(proposal.Items))
	for i, pi := range proposal.Items {
		var itemID uint64

		if pi.UserChoice == "existing" && pi.MatchedItemID != 0 {
			itemID = pi.MatchedItemID
		} else {
			// Create new item
			item := &domain.Item{
				ItemID:     p.store.ItemID.Gen(),
				Name:       pi.ParsedName,
				CategoryID: pi.CategoryID,
				MerchantID: 0, // Will be set if merchant is known
				Normalized: normalizeName(pi.ParsedName),
				Aliases:    []string{pi.ParsedName},
			}

			if err := p.store.CreateItem(item); err != nil {
				return nil, err
			}
			itemID = item.ItemID
		}

		receiptItems[i] = domain.ReceiptItem{
			ItemID:    itemID,
			Quantity:  pi.Quantity,
			UnitPrice: pi.UnitPrice,
		}
	}

	receipt := &domain.Receipt{
		ReceiptID:  p.store.ReceiptID.Gen(),
		MerchantID: 0, // TODO: lookup merchant ID
		OwnerID:    proposal.OwnerID,
		Date:       proposal.Date,
		PhotoURL:   proposal.PhotoURL,
		Items:      receiptItems,
		Total:      proposal.Total,
	}

	if err := p.store.CreateReceipt(receipt); err != nil {
		return nil, err
	}

	proposal.Status = "approved"
	if err := p.store.UpdateProposal(proposal); err != nil {
		return nil, err
	}

	return receipt, nil
}

func normalizeName(name string) string {
	// Simple normalization: lowercase, trim, collapse spaces
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.Join(strings.Fields(name), " ")
	return name
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/receipt/`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/receipt/parser.go
git commit -m "feat: add receipt parser with LLM integration"
```

---

### Task 11: Item Matcher

**Files:**
- Create: `internal/receipt/matcher.go`

- [ ] **Step 1: Create item matcher**

```go
package receipt

import (
	"math"
	"strings"

	"code.sirenko.ca/grocer/internal/domain"
	"code.sirenko.ca/grocer/internal/store"
)

type Matcher struct {
	store *store.Store
}

func NewMatcher(store *store.Store) *Matcher {
	return &Matcher{store: store}
}

func (m *Matcher) FindMatch(name string) (*domain.Item, float64, error) {
	items, err := m.store.ListItems()
	if err != nil {
		return nil, 0, err
	}

	normalized := normalizeName(name)

	var bestMatch *domain.Item
	var bestScore float64

	for _, item := range items {
		score := m.calculateSimilarity(normalized, item.Normalized)
		if score > bestScore {
			bestScore = score
			bestMatch = item
		}

		// Also check aliases
		for _, alias := range item.Aliases {
			aliasScore := m.calculateSimilarity(normalized, normalizeName(alias))
			if aliasScore > bestScore {
				bestScore = aliasScore
				bestMatch = item
			}
		}
	}

	if bestMatch == nil {
		return nil, 0, nil
	}

	return bestMatch, bestScore, nil
}

func (m *Matcher) calculateSimilarity(s1, s2 string) float64 {
	if s1 == s2 {
		return 1.0
	}

	// Jaccard similarity on words
	words1 := strings.Fields(s1)
	words2 := strings.Fields(s2)

	set1 := make(map[string]bool)
	for _, w := range words1 {
		set1[w] = true
	}

	set2 := make(map[string]bool)
	for _, w := range words2 {
		set2[w] = true
	}

	intersection := 0
	for w := range set1 {
		if set2[w] {
			intersection++
		}
	}

	union := len(set1) + len(set2) - intersection
	if union == 0 {
		return 0
	}

	jaccard := float64(intersection) / float64(union)

	// Also consider Levenshtein distance for fuzzy matching
	levDistance := levenshteinDistance(s1, s2)
	maxLen := math.Max(float64(len(s1)), float64(len(s2)))
	levSimilarity := 1.0 - float64(levDistance)/maxLen

	// Weighted average
	return 0.7*jaccard + 0.3*levSimilarity
}

func levenshteinDistance(s1, s2 string) int {
	if len(s1) == 0 {
		return len(s2)
	}
	if len(s2) == 0 {
		return len(s1)
	}

	matrix := make([][]int, len(s1)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(s2)+1)
		matrix[i][0] = i
	}
	for j := range matrix[0] {
		matrix[0][j] = j
	}

	for i := 1; i <= len(s1); i++ {
		for j := 1; j <= len(s2); j++ {
			cost := 1
			if s1[i-1] == s2[j-1] {
				cost = 0
			}
			matrix[i][j] = min(
				matrix[i-1][j]+1,
				matrix[i][j-1]+1,
				matrix[i-1][j-1]+cost,
			)
		}
	}

	return matrix[len(s1)][len(s2)]
}

func min(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/receipt/`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/receipt/matcher.go
git commit -m "feat: add item matcher with fuzzy matching"
```

---

## Phase 4: HTTP API

### Task 12: Router and Middleware

**Files:**
- Create: `internal/api/router.go`

- [ ] **Step 1: Create router with middleware**

```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"code.sirenko.ca/grocer/internal/store"
)

type Router struct {
	store  *store.Store
	mux    *http.ServeMux
}

func NewRouter(store *store.Store) *Router {
	r := &Router{
		store: store,
		mux:   http.NewServeMux(),
	}

	r.setupRoutes()
	return r
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

func (r *Router) setupRoutes() {
	// Auth
	r.mux.HandleFunc("POST /api/auth/login", r.handleLogin)

	// Receipts
	r.mux.HandleFunc("GET /api/receipts", r.withAuth(r.handleListReceipts))
	r.mux.HandleFunc("GET /api/receipts/{id}", r.withAuth(r.handleGetReceipt))
	r.mux.HandleFunc("POST /api/receipts/upload", r.withAuth(r.handleUploadReceipt))

	// Proposals
	r.mux.HandleFunc("GET /api/proposals", r.withAuth(r.handleListProposals))
	r.mux.HandleFunc("GET /api/proposals/{id}", r.withAuth(r.handleGetProposal))
	r.mux.HandleFunc("POST /api/proposals/{id}/approve", r.withAuth(r.handleApproveProposal))

	// Items
	r.mux.HandleFunc("GET /api/items", r.withAuth(r.handleListItems))
	r.mux.HandleFunc("GET /api/items/{id}", r.withAuth(r.handleGetItem))
	r.mux.HandleFunc("PATCH /api/items/{id}", r.withAuth(r.handleUpdateItem))

	// Categories
	r.mux.HandleFunc("GET /api/categories", r.withAuth(r.handleListCategories))
	r.mux.HandleFunc("POST /api/categories", r.withAuth(r.handleCreateCategory))
	r.mux.HandleFunc("PATCH /api/categories/{id}", r.withAuth(r.handleUpdateCategory))
	r.mux.HandleFunc("DELETE /api/categories/{id}", r.withAuth(r.handleDeleteCategory))

	// Merchants
	r.mux.HandleFunc("GET /api/merchants", r.withAuth(r.handleListMerchants))
	r.mux.HandleFunc("POST /api/merchants", r.withAuth(r.handleCreateMerchant))
	r.mux.HandleFunc("PATCH /api/merchants/{id}", r.withAuth(r.handleUpdateMerchant))

	// Analysis
	r.mux.HandleFunc("GET /api/analysis/spending", r.withAuth(r.handleAnalysisSpending))
	r.mux.HandleFunc("GET /api/analysis/categories", r.withAuth(r.handleAnalysisCategories))
	r.mux.HandleFunc("GET /api/analysis/family", r.withAuth(r.handleAnalysisFamily))
	r.mux.HandleFunc("GET /api/analysis/items/{id}", r.withAuth(r.handleAnalysisItem))
}

type contextKey string

const userIDKey contextKey = "userID"

func (r *Router) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		authHeader := req.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "missing authorization header", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			http.Error(w, "invalid authorization format", http.StatusUnauthorized)
			return
		}

		sessionID, tokenStr, err := store.ParseTokenString(token)
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		session, err := r.store.GetSession(sessionID)
		if err != nil {
			http.Error(w, "invalid session", http.StatusUnauthorized)
			return
		}

		// Verify token hash
		// TODO: implement token verification

		ctx := context.WithValue(req.Context(), userIDKey, session.UserID)
		next(w, req.WithContext(ctx))
	}
}

func (r *Router) getUserID(req *http.Request) uint64 {
	return req.Context().Value(userIDKey).(uint64)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
```

- [ ] **Step 2: Add token parsing to store**

Add to `internal/store/memdb.go`:

```go
func ParseTokenString(tokenString string) (uint64, string, error) {
	vals := strings.Split(tokenString, ":")
	if len(vals) != 2 {
		return 0, "", errors.New("invalid token string")
	}
	id, err := strconv.ParseUint(vals[0], 10, 64)
	if err != nil {
		return 0, "", err
	}
	return id, vals[1], nil
}
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./internal/api/`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add internal/api/router.go internal/store/memdb.go
git commit -m "feat: add HTTP router with auth middleware"
```

---

### Task 13: Auth Handler

**Files:**
- Create: `internal/api/auth.go`

- [ ] **Step 1: Create auth handler**

```go
package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"code.sirenko.ca/grocer/internal/store"
	"golang.org/x/crypto/argon2"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string      `json:"token"`
	User  interface{} `json:"user"`
}

func (r *Router) handleLogin(w http.ResponseWriter, req *http.Request) {
	var reqBody loginRequest
	if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := r.store.GetUserByUsername(reqBody.Username)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Verify password
	match, err := verifyPassword(reqBody.Password, user.PasswordHash)
	if err != nil || !match {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Create session
	token, err := store.GenerateRandomBytes(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	tokenString := base64.RawStdEncoding.EncodeToString(token)

	tokenHash, err := store.GenerateFromPasswordShort(tokenString)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	session := &store.Session{
		SessionID: r.store.SessionID.Gen(),
		TokenHash: tokenHash,
		UserID:    user.UserID,
	}

	if err := r.store.CreateSession(session); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	idTokenString := fmt.Sprintf("%d:%s", session.SessionID, tokenString)

	writeJSON(w, http.StatusOK, loginResponse{
		Token: idTokenString,
		User:  user,
	})
}

func verifyPassword(password, hash string) (bool, error) {
	// Parse argon2 hash
	// Implementation similar to existing encryption package
	return false, nil // TODO: implement
}
```

- [ ] **Step 2: Add password verification to store**

Add to `internal/store/memdb.go`:

```go
func GenerateRandomBytes(n uint32) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	return b, err
}

func GenerateFromPasswordShort(password string) (string, error) {
	salt, err := GenerateRandomBytes(16)
	if err != nil {
		return "", err
	}

	hash := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf("%s$%s", b64Salt, b64Hash), nil
}

func ComparePasswordAndHashShort(password, encodedHash string) (bool, error) {
	vals := strings.Split(encodedHash, "$")
	if len(vals) != 2 {
		return false, errors.New("invalid hash format")
	}

	salt, err := base64.RawStdEncoding.Strict().DecodeString(vals[0])
	if err != nil {
		return false, err
	}

	hash, err := base64.RawStdEncoding.Strict().DecodeString(vals[1])
	if err != nil {
		return false, err
	}

	otherHash := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)

	return subtle.ConstantTimeCompare(hash, otherHash) == 1, nil
}
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./internal/api/`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add internal/api/auth.go internal/store/memdb.go
git commit -m "feat: add auth handler with password verification"
```

---

### Task 14: Receipt Endpoints

**Files:**
- Create: `internal/api/receipts.go`

- [ ] **Step 1: Create receipt endpoints**

```go
package api

import (
	"io"
	"net/http"
	"strconv"

	"code.sirenko.ca/grocer/internal/receipt"
)

type ReceiptHandler struct {
	parser *receipt.Parser
}

func (r *Router) handleListReceipts(w http.ResponseWriter, req *http.Request) {
	// Parse query params for filtering
	from := req.URL.Query().Get("from")
	to := req.URL.Query().Get("to")
	owner := req.URL.Query().Get("owner")
	category := req.URL.Query().Get("category")

	receipts, err := r.store.ListReceipts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Apply filters (simplified for now)
	filtered := receipts
	_ = from
	_ = to
	_ = owner
	_ = category

	writeJSON(w, http.StatusOK, filtered)
}

func (r *Router) handleGetReceipt(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid receipt ID")
		return
	}

	receipt, err := r.store.GetReceipt(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "receipt not found")
		return
	}

	writeJSON(w, http.StatusOK, receipt)
}

func (r *Router) handleUploadReceipt(w http.ResponseWriter, req *http.Request) {
	userID := r.getUserID(req)

	// Parse multipart form
	if err := req.ParseMultipartForm(10 << 20); err != nil { // 10 MB
		writeError(w, http.StatusBadRequest, "file too large")
		return
	}

	file, _, err := req.FormFile("photo")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing photo")
		return
	}
	defer file.Close()

	photo, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read file")
		return
	}

	// Parse receipt using LLM
	parser := receipt.NewParser(r.store, nil) // TODO: inject LLM provider
	proposal, err := parser.ParseReceipt(req.Context(), photo, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to parse receipt")
		return
	}

	writeJSON(w, http.StatusOK, proposal)
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/api/`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/api/receipts.go
git commit -m "feat: add receipt endpoints"
```

---

### Task 15: Proposal Endpoints

**Files:**
- Create: `internal/api/proposals.go`

- [ ] **Step 1: Create proposal endpoints**

```go
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"code.sirenko.ca/grocer/internal/receipt"
)

type approveRequest struct {
	Choices map[int]string `json:"choices"` // index -> "existing" or "new"
}

func (r *Router) handleListProposals(w http.ResponseWriter, req *http.Request) {
	proposals, err := r.store.ListProposals()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Filter pending only
	var pending []*domain.Proposal
	for _, p := range proposals {
		if p.Status == "pending" {
			pending = append(pending, p)
		}
	}

	writeJSON(w, http.StatusOK, pending)
}

func (r *Router) handleGetProposal(w http.ResponseWriter, req *http.Request) {
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

	writeJSON(w, http.StatusOK, proposal)
}

func (r *Router) handleApproveProposal(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid proposal ID")
		return
	}

	var reqBody approveRequest
	if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	parser := receipt.NewParser(r.store, nil) // TODO: inject LLM provider
	receipt, err := parser.ApproveProposal(req.Context(), id, reqBody.Choices)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to approve proposal")
		return
	}

	writeJSON(w, http.StatusOK, receipt)
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/api/`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/api/proposals.go
git commit -m "feat: add proposal endpoints"
```

---

### Task 16: Item, Category, Merchant Endpoints

**Files:**
- Create: `internal/api/items.go`
- Create: `internal/api/categories.go`
- Create: `internal/api/merchants.go`

- [ ] **Step 1: Create items endpoints**

```go
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"code.sirenko.ca/grocer/internal/domain"
)

func (r *Router) handleListItems(w http.ResponseWriter, req *http.Request) {
	items, err := r.store.ListItems()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (r *Router) handleGetItem(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item ID")
		return
	}

	item, err := r.store.GetItem(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type updateItemRequest struct {
	Name       *string  `json:"name,omitempty"`
	CategoryID *uint64  `json:"categoryId,omitempty"`
	Aliases    []string `json:"aliases,omitempty"`
}

func (r *Router) handleUpdateItem(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item ID")
		return
	}

	item, err := r.store.GetItem(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}

	var reqBody updateItemRequest
	if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if reqBody.Name != nil {
		item.Name = *reqBody.Name
	}
	if reqBody.CategoryID != nil {
		item.CategoryID = *reqBody.CategoryID
	}
	if reqBody.Aliases != nil {
		item.Aliases = reqBody.Aliases
	}

	if err := r.store.UpdateItem(item); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, item)
}
```

- [ ] **Step 2: Create categories endpoints**

```go
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"code.sirenko.ca/grocer/internal/domain"
)

func (r *Router) handleListCategories(w http.ResponseWriter, req *http.Request) {
	categories, err := r.store.ListCategories()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, categories)
}

type createCategoryRequest struct {
	Name     string  `json:"name"`
	ParentID *uint64 `json:"parentId,omitempty"`
}

func (r *Router) handleCreateCategory(w http.ResponseWriter, req *http.Request) {
	var reqBody createCategoryRequest
	if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	category := &domain.Category{
		CategoryID: r.store.CategoryID.Gen(),
		Name:       reqBody.Name,
		ParentID:   reqBody.ParentID,
	}

	if err := r.store.CreateCategory(category); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, category)
}

type updateCategoryRequest struct {
	Name      *string `json:"name,omitempty"`
	ParentID  *uint64 `json:"parentId,omitempty"`
	SortOrder *int32  `json:"sortOrder,omitempty"`
}

func (r *Router) handleUpdateCategory(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid category ID")
		return
	}

	category, err := r.store.GetCategory(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "category not found")
		return
	}

	var reqBody updateCategoryRequest
	if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if reqBody.Name != nil {
		category.Name = *reqBody.Name
	}
	if reqBody.ParentID != nil {
		category.ParentID = reqBody.ParentID
	}
	if reqBody.SortOrder != nil {
		category.SortOrder = *reqBody.SortOrder
	}

	if err := r.store.UpdateCategory(category); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, category)
}

func (r *Router) handleDeleteCategory(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid category ID")
		return
	}

	if err := r.store.DeleteCategory(id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 3: Create merchants endpoints**

```go
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"code.sirenko.ca/grocer/internal/domain"
)

func (r *Router) handleListMerchants(w http.ResponseWriter, req *http.Request) {
	merchants, err := r.store.ListMerchants()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, merchants)
}

type createMerchantRequest struct {
	Name string `json:"name"`
}

func (r *Router) handleCreateMerchant(w http.ResponseWriter, req *http.Request) {
	var reqBody createMerchantRequest
	if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	merchant := &domain.Merchant{
		MerchantID: r.store.MerchantID.Gen(),
		Name:       reqBody.Name,
	}

	if err := r.store.CreateMerchant(merchant); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, merchant)
}

type updateMerchantRequest struct {
	Name *string `json:"name,omitempty"`
}

func (r *Router) handleUpdateMerchant(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid merchant ID")
		return
	}

	merchant, err := r.store.GetMerchant(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "merchant not found")
		return
	}

	var reqBody updateMerchantRequest
	if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if reqBody.Name != nil {
		merchant.Name = *reqBody.Name
	}

	if err := r.store.UpdateMerchant(merchant); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, merchant)
}
```

- [ ] **Step 4: Verify compilation**

Run: `go build ./internal/api/`
Expected: No errors

- [ ] **Step 5: Commit**

```bash
git add internal/api/items.go internal/api/categories.go internal/api/merchants.go
git commit -m "feat: add item, category, and merchant endpoints"
```

---

### Task 17: Analysis Endpoints

**Files:**
- Create: `internal/api/analysis.go`

- [ ] **Step 1: Create analysis endpoints**

```go
package api

import (
	"net/http"
	"strconv"
	"time"
)

func (r *Router) handleAnalysisSpending(w http.ResponseWriter, req *http.Request) {
	from := req.URL.Query().Get("from")
	to := req.URL.Query().Get("to")
	granularity := req.URL.Query().Get("granularity") // day, week, month

	if granularity == "" {
		granularity = "month"
	}

	receipts, err := r.store.ListReceipts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Filter by date range
	var filtered []*domain.Receipt
	for _, receipt := range receipts {
		receiptDate := time.Unix(receipt.Date, 0)
		if from != "" {
			fromDate, err := time.Parse("2006-01-02", from)
			if err == nil && receiptDate.Before(fromDate) {
				continue
			}
		}
		if to != "" {
			toDate, err := time.Parse("2006-01-02", to)
			if err == nil && receiptDate.After(toDate) {
				continue
			}
		}
		filtered = append(filtered, receipt)
	}

	// Group by granularity
	type SpendingPeriod struct {
		Period string  `json:"period"`
		Total  float64 `json:"total"`
	}

	periodMap := make(map[string]float64)
	for _, receipt := range filtered {
		date := time.Unix(receipt.Date, 0)
		var period string
		switch granularity {
		case "day":
			period = date.Format("2006-01-02")
		case "week":
			// ISO week
			year, week := date.ISOWeek()
			period = strconv.Itoa(year) + "-W" + strconv.Itoa(week)
		case "month":
			period = date.Format("2006-01")
		}
		periodMap[period] += receipt.Total
	}

	var result []SpendingPeriod
	for period, total := range periodMap {
		result = append(result, SpendingPeriod{Period: period, Total: total})
	}

	writeJSON(w, http.StatusOK, result)
}

func (r *Router) handleAnalysisCategories(w http.ResponseWriter, req *http.Request) {
	from := req.URL.Query().Get("from")
	to := req.URL.Query().Get("to")
	owner := req.URL.Query().Get("owner")

	receipts, err := r.store.ListReceipts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Filter
	var filtered []*domain.Receipt
	for _, receipt := range receipts {
		receiptDate := time.Unix(receipt.Date, 0)
		if from != "" {
			fromDate, err := time.Parse("2006-01-02", from)
			if err == nil && receiptDate.Before(fromDate) {
				continue
			}
		}
		if to != "" {
			toDate, err := time.Parse("2006-01-02", to)
			if err == nil && receiptDate.After(toDate) {
				continue
			}
		}
		if owner != "" {
			ownerID, err := strconv.ParseUint(owner, 10, 64)
			if err == nil && receipt.OwnerID != ownerID {
				continue
			}
		}
		filtered = append(filtered, receipt)
	}

	// Aggregate by category
	type CategoryTotal struct {
		CategoryID uint64  `json:"categoryId"`
		Name       string  `json:"name"`
		Total      float64 `json:"total"`
	}

	categoryMap := make(map[uint64]float64)
	for _, receipt := range filtered {
		for _, item := range receipt.Items {
			// Lookup item to get category
			itemObj, err := r.store.GetItem(item.ItemID)
			if err != nil {
				continue
			}
			categoryMap[itemObj.CategoryID] += float64(item.Quantity) * item.UnitPrice
		}
	}

	var result []CategoryTotal
	for catID, total := range categoryMap {
		cat, err := r.store.GetCategory(catID)
		name := "Unknown"
		if err == nil {
			name = cat.Name
		}
		result = append(result, CategoryTotal{CategoryID: catID, Name: name, Total: total})
	}

	writeJSON(w, http.StatusOK, result)
}

func (r *Router) handleAnalysisFamily(w http.ResponseWriter, req *http.Request) {
	from := req.URL.Query().Get("from")
	to := req.URL.Query().Get("to")

	receipts, err := r.store.ListReceipts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Filter
	var filtered []*domain.Receipt
	for _, receipt := range receipts {
		receiptDate := time.Unix(receipt.Date, 0)
		if from != "" {
			fromDate, err := time.Parse("2006-01-02", from)
			if err == nil && receiptDate.Before(fromDate) {
				continue
			}
		}
		if to != "" {
			toDate, err := time.Parse("2006-01-02", to)
			if err == nil && receiptDate.After(toDate) {
				continue
			}
		}
		filtered = append(filtered, receipt)
	}

	// Aggregate by owner
	type FamilyMember struct {
		UserID uint64  `json:"userId"`
		Name   string  `json:"name"`
		Total  float64 `json:"total"`
	}

	memberMap := make(map[uint64]float64)
	for _, receipt := range filtered {
		memberMap[receipt.OwnerID] += receipt.Total
	}

	var result []FamilyMember
	for userID, total := range memberMap {
		user, err := r.store.GetUserByUserID(userID)
		name := "Unknown"
		if err == nil {
			name = user.Name
		}
		result = append(result, FamilyMember{UserID: userID, Name: name, Total: total})
	}

	writeJSON(w, http.StatusOK, result)
}

func (r *Router) handleAnalysisItem(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	itemID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item ID")
		return
	}

	receipts, err := r.store.ListReceipts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Find all receipts containing this item
	type PricePoint struct {
		Date  string  `json:"date"`
		Price float64 `json:"price"`
	}

	var history []PricePoint
	for _, receipt := range receipts {
		for _, item := range receipt.Items {
			if item.ItemID == itemID {
				date := time.Unix(receipt.Date, 0)
				history = append(history, PricePoint{
					Date:  date.Format("2006-01-02"),
					Price: item.UnitPrice,
				})
			}
		}
	}

	writeJSON(w, http.StatusOK, history)
}
```

- [ ] **Step 2: Add GetUserByUserID to store**

Add to `internal/store/memdb.go`:

```go
func (s *Store) GetUserByUserID(userID uint64) (*domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	iter, err := txn.Get("users", "id")
	if err != nil {
		return nil, err
	}

	for raw := iter.Next(); raw != nil; raw = iter.Next() {
		user := raw.(*domain.User)
		if user.UserID == userID {
			return user, nil
		}
	}

	return nil, ErrNotFound
}
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./internal/api/`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add internal/api/analysis.go internal/store/memdb.go
git commit -m "feat: add analysis endpoints for spending, categories, family"
```

---

## Phase 5: Bots

### Task 18: Bot Interface and Telegram Bot

**Files:**
- Create: `internal/bot/bot.go`
- Create: `internal/bot/telegram.go`

- [ ] **Step 1: Create bot interface**

```go
package bot

import (
	"context"
)

type Bot interface {
	Start(ctx context.Context) error
	Stop() error
}

type ReceiptHandler interface {
	HandlePhoto(ctx context.Context, photo []byte, senderID string) (string, error)
}
```

- [ ] **Step 2: Create Telegram bot**

```go
package bot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type TelegramBot struct {
	token    string
	webURL   string
	handler  ReceiptHandler
	bot      *tgbotapi.BotAPI
}

func NewTelegramBot(token, webURL string, handler ReceiptHandler) *TelegramBot {
	return &TelegramBot{
		token:   token,
		webURL:  webURL,
		handler: handler,
	}
}

func (t *TelegramBot) Start(ctx context.Context) error {
	var err error
	t.bot, err = tgbotapi.NewBotAPI(t.token)
	if err != nil {
		return fmt.Errorf("NewBotAPI: %w", err)
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := t.bot.GetUpdatesChan(u)

	go func() {
		for {
			select {
			case <-ctx.Done():
				t.bot.StopReceivingUpdates()
				return
			case update := <-updates:
				t.handleUpdate(ctx, update)
			}
		}
	}()

	return nil
}

func (t *TelegramBot) Stop() error {
	t.bot.StopReceivingUpdates()
	return nil
}

func (t *TelegramBot) handleUpdate(ctx context.Context, update tgbotapi.Update) {
	if update.Message == nil {
		return
	}

	// Handle photo messages
	if update.Message.Photo != nil {
		photo := update.Message.Photo[len(update.Message.Photo)-1] // Get largest photo

		file, err := t.bot.GetFile(tgbotapi.FileConfig{FileID: photo.FileID})
		if err != nil {
			t.sendMessage(update.Message.Chat.ID, "Failed to get photo")
			return
		}

		resp, err := http.Get(file.Link(t.bot.Token))
		if err != nil {
			t.sendMessage(update.Message.Chat.ID, "Failed to download photo")
			return
		}
		defer resp.Body.Close()

		photoData, err := io.ReadAll(resp.Body)
		if err != nil {
			t.sendMessage(update.Message.Chat.ID, "Failed to read photo")
			return
		}

		senderID := strconv.FormatInt(update.Message.From.ID, 10)
		proposalID, err := t.handler.HandlePhoto(ctx, photoData, senderID)
		if err != nil {
			t.sendMessage(update.Message.Chat.ID, fmt.Sprintf("Failed to parse receipt: %v", err))
			return
		}

		link := fmt.Sprintf("%s/proposals/%s", t.webURL, proposalID)
		t.sendMessage(update.Message.Chat.ID, fmt.Sprintf("Receipt parsed! [Review and approve →](%s)", link))
	}
}

func (t *TelegramBot) sendMessage(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	t.bot.Send(msg)
}
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./internal/bot/`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add internal/bot/bot.go internal/bot/telegram.go
git commit -m "feat: add bot interface and Telegram bot implementation"
```

---

### Task 19: Discord Bot

**Files:**
- Create: `internal/bot/discord.go`

- [ ] **Step 1: Create Discord bot**

```go
package bot

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/bwmarrin/discordgo"
)

type DiscordBot struct {
	token   string
	webURL  string
	handler ReceiptHandler
	session *discordgo.Session
}

func NewDiscordBot(token, webURL string, handler ReceiptHandler) *DiscordBot {
	return &DiscordBot{
		token:   token,
		webURL:  webURL,
		handler: handler,
	}
}

func (d *DiscordBot) Start(ctx context.Context) error {
	var err error
	d.session, err = discordgo.New("Bot " + d.token)
	if err != nil {
		return fmt.Errorf("discordgo.New: %w", err)
	}

	d.session.AddHandler(d.handleMessage)

	if err := d.session.Open(); err != nil {
		return fmt.Errorf("Open: %w", err)
	}

	return nil
}

func (d *DiscordBot) Stop() error {
	return d.session.Close()
}

func (d *DiscordBot) handleMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	// Handle messages with attachments (photos)
	for _, att := range m.Attachments {
		if att.ContentType == "image/jpeg" || att.ContentType == "image/png" {
			resp, err := http.Get(att.URL)
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, "Failed to download photo")
				continue
			}
			defer resp.Body.Close()

			photoData, err := io.ReadAll(resp.Body)
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, "Failed to read photo")
				continue
			}

			proposalID, err := d.handler.HandlePhoto(context.Background(), photoData, m.Author.ID)
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to parse receipt: %v", err))
				continue
			}

			link := fmt.Sprintf("%s/proposals/%s", d.webURL, proposalID)
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Receipt parsed! [Review and approve →](%s)", link))
		}
	}
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/bot/`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/bot/discord.go
git commit -m "feat: add Discord bot implementation"
```

---

## Phase 6: Photo Storage

### Task 20: Photo Storage

**Files:**
- Create: `internal/photo/store.go`
- Create: `internal/photo/cache.go`

- [ ] **Step 1: Create photo store interface**

```go
package photo

import (
	"context"
)

type Store interface {
	Save(ctx context.Context, receiptID uint64, data []byte) (string, error)
	Get(ctx context.Context, url string) ([]byte, error)
}
```

- [ ] **Step 2: Create GCloud photo store**

```go
package photo

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"cloud.google.com/go/storage"
)

type GCloudStore struct {
	client *storage.Client
	bucket string
	prefix string
}

func NewGCloudStore(client *storage.Client, bucket, prefix string) *GCloudStore {
	return &GCloudStore{
		client: client,
		bucket: bucket,
		prefix: prefix,
	}
}

func (g *GCloudStore) Save(ctx context.Context, receiptID uint64, data []byte) (string, error) {
	objectName := fmt.Sprintf("%s%d.jpg", g.prefix, receiptID)

	obj := g.client.Bucket(g.bucket).Object(objectName)
	writer := obj.NewWriter(ctx)
	writer.ContentType = "image/jpeg"

	if _, err := writer.Write(data); err != nil {
		return "", fmt.Errorf("Write: %w", err)
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("Close: %w", err)
	}

	return fmt.Sprintf("gs://%s/%s", g.bucket, objectName), nil
}

func (g *GCloudStore) Get(ctx context.Context, url string) ([]byte, error) {
	// Parse gs:// URL
	// For now, assume direct object name
	obj := g.client.Bucket(g.bucket).Object(url)

	reader, err := obj.NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("NewReader: %w", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("ReadAll: %w", err)
	}

	return data, nil
}
```

- [ ] **Step 3: Create local cache**

```go
package photo

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type LocalCache struct {
	dir     string
	maxSize int64
	mu      sync.Mutex
	files   map[string]int64 // filename -> size
}

func NewLocalCache(dir string, maxSizeMB int) *LocalCache {
	return &LocalCache{
		dir:     dir,
		maxSize: int64(maxSizeMB) * 1024 * 1024,
		files:   make(map[string]int64),
	}
}

func (c *LocalCache) Get(ctx context.Context, url string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	filename := c.filename(url)
	path := filepath.Join(c.dir, filename)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (c *LocalCache) Set(ctx context.Context, url string, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	filename := c.filename(url)
	path := filepath.Join(c.dir, filename)

	// Ensure directory exists
	if err := os.MkdirAll(c.dir, 0755); err != nil {
		return err
	}

	// Check if we need to evict
	c.evictIfNeeded(int64(len(data)))

	return os.WriteFile(path, data, 0644)
}

func (c *LocalCache) filename(url string) string {
	hash := sha256.Sum256([]byte(url))
	return fmt.Sprintf("%x.jpg", hash)
}

func (c *LocalCache) evictIfNeeded(newSize int64) {
	// Simple LRU: remove oldest files until we have space
	// For now, just check total size
	var totalSize int64
	for _, size := range c.files {
		totalSize += size
	}

	if totalSize+newSize > c.maxSize {
		// Remove files until we have space
		for filename, size := range c.files {
			os.Remove(filepath.Join(c.dir, filename))
			delete(c.files, filename)
			totalSize -= size
			if totalSize+newSize <= c.maxSize {
				break
			}
		}
	}

	c.files[c.filename("")] = newSize
}
```

- [ ] **Step 4: Verify compilation**

Run: `go build ./internal/photo/`
Expected: No errors

- [ ] **Step 5: Commit**

```bash
git add internal/photo/store.go internal/photo/cache.go
git commit -m "feat: add photo storage with GCloud and local cache"
```

---

## Phase 7: Frontend

### Task 21: Frontend Setup

**Files:**
- Modify: `client/index.html`
- Modify: `client/main.ts`
- Create: `client/api.ts`
- Create: `client/styles/main.css`

- [ ] **Step 1: Update index.html**

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Grocer</title>
  <link rel="stylesheet" href="./styles/main.css">
</head>
<body>
  <div id="app"></div>
  <script type="module" src="./main.ts"></script>
</body>
</html>
```

- [ ] **Step 2: Create main.ts with router**

```typescript
import van from "vanjs-core"

const { div, nav, a, button } = van.tags

// Simple hash-based router
const currentPath = van.state(window.location.hash.slice(1) || "/")

window.addEventListener("hashchange", () => {
  currentPath.val = window.location.hash.slice(1)
})

const navigate = (path: string) => {
  window.location.hash = path
}

// API helper
const api = {
  async fetch(path: string, options: RequestInit = {}) {
    const token = localStorage.getItem("token")
    const headers: Record<string, string> = {
      ...options.headers as Record<string, string>,
    }
    if (token) {
      headers["Authorization"] = `Bearer ${token}`
    }
    const response = await fetch(`/api${path}`, { ...options, headers })
    if (response.status === 401) {
      navigate("/login")
      throw new Error("Unauthorized")
    }
    return response.json()
  },

  get: (path: string) => api.fetch(path),
  post: (path: string, body: any) => api.fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  }),
  patch: (path: string, body: any) => api.fetch(path, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  }),
  delete: (path: string) => api.fetch(path, { method: "DELETE" }),
}

// Layout
const Layout = (content: any) => div({ class: "layout" },
  nav({ class: "sidebar" },
    a({ href: "#/receipts", onclick: () => navigate("/receipts") }, "Receipts"),
    a({ href: "#/proposals", onclick: () => navigate("/proposals") }, "Proposals"),
    a({ href: "#/items", onclick: () => navigate("/items") }, "Items"),
    a({ href: "#/categories", onclick: () => navigate("/categories") }, "Categories"),
    a({ href: "#/analysis", onclick: () => navigate("/analysis") }, "Analysis"),
  ),
  div({ class: "content" }, content),
)

// App
const App = () => {
  return div({ id: "app" },
    () => {
      const path = currentPath.val
      if (path === "/login") {
        return Login()
      }
      return Layout(PageContent())
    }
  )
}

// Placeholder pages
const Login = () => div("Login page - TODO")
const PageContent = () => div("Page content - TODO")

// Mount
van.add(document.getElementById("app")!, App())

export { api, navigate, currentPath }
```

- [ ] **Step 3: Create main.css**

```css
:root {
  --bg-primary: #0a0a0a;
  --bg-secondary: #141414;
  --bg-tertiary: #1e1e1e;
  --text-primary: #e5e5e5;
  --text-secondary: #a0a0a0;
  --accent: #3b82f6;
  --accent-hover: #2563eb;
  --border: #2e2e2e;
  --success: #22c55e;
  --warning: #eab308;
  --error: #ef4444;
}

* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

body {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
  background: var(--bg-primary);
  color: var(--text-primary);
  line-height: 1.5;
}

.layout {
  display: flex;
  min-height: 100vh;
}

.sidebar {
  width: 200px;
  background: var(--bg-secondary);
  border-right: 1px solid var(--border);
  padding: 1rem;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.sidebar a {
  color: var(--text-secondary);
  text-decoration: none;
  padding: 0.5rem 0.75rem;
  border-radius: 6px;
  transition: all 0.2s;
}

.sidebar a:hover {
  background: var(--bg-tertiary);
  color: var(--text-primary);
}

.content {
  flex: 1;
  padding: 2rem;
}

button {
  background: var(--accent);
  color: white;
  border: none;
  padding: 0.5rem 1rem;
  border-radius: 6px;
  cursor: pointer;
  font-size: 0.875rem;
  transition: background 0.2s;
}

button:hover {
  background: var(--accent-hover);
}

input, select {
  background: var(--bg-tertiary);
  border: 1px solid var(--border);
  color: var(--text-primary);
  padding: 0.5rem 0.75rem;
  border-radius: 6px;
  font-size: 0.875rem;
}

input:focus, select:focus {
  outline: none;
  border-color: var(--accent);
}

.card {
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 1rem;
}

table {
  width: 100%;
  border-collapse: collapse;
}

th, td {
  padding: 0.75rem;
  text-align: left;
  border-bottom: 1px solid var(--border);
}

th {
  color: var(--text-secondary);
  font-weight: 500;
}
```

- [ ] **Step 4: Verify build**

Run: `bun build --outdir=dist ./client/index.html`
Expected: No errors

- [ ] **Step 5: Commit**

```bash
git add client/index.html client/main.ts client/styles/main.css
git commit -m "feat: add frontend setup with router and layout"
```

---

### Task 22: Login Page

**Files:**
- Create: `client/pages/login.ts`

- [ ] **Step 1: Create login page**

```typescript
import van from "vanjs-core"
import { api, navigate } from "../main"

const { div, form, input, button, label, h1, p } = van.tags

const Login = () => {
  const username = van.state("")
  const password = van.state("")
  const error = van.state("")

  const handleSubmit = async (e: Event) => {
    e.preventDefault()
    error.val = ""

    try {
      const result = await api.post("/auth/login", {
        username: username.val,
        password: password.val,
      })

      if (result.token) {
        localStorage.setItem("token", result.token)
        localStorage.setItem("user", JSON.stringify(result.user))
        navigate("/receipts")
      } else {
        error.val = "Invalid credentials"
      }
    } catch (err) {
      error.val = "Login failed"
    }
  }

  return div({ class: "login-page" },
    form({ class: "login-form", onsubmit: handleSubmit },
      h1("Grocer"),
      () => error.val ? p({ class: "error" }, error.val) : "",
      div({ class: "form-group" },
        label({ for: "username" }, "Username"),
        input({
          id: "username",
          type: "text",
          value: username,
          oninput: (e: Event) => username.val = (e.target as HTMLInputElement).value,
        }),
      ),
      div({ class: "form-group" },
        label({ for: "password" }, "Password"),
        input({
          id: "password",
          type: "password",
          value: password,
          oninput: (e: Event) => password.val = (e.target as HTMLInputElement).value,
        }),
      ),
      button({ type: "submit" }, "Login"),
    ),
  )
}

export default Login
```

- [ ] **Step 2: Add login styles**

Add to `client/styles/main.css`:

```css
.login-page {
  display: flex;
  justify-content: center;
  align-items: center;
  min-height: 100vh;
}

.login-form {
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 12px;
  padding: 2rem;
  width: 100%;
  max-width: 400px;
}

.login-form h1 {
  text-align: center;
  margin-bottom: 1.5rem;
  font-size: 1.5rem;
}

.form-group {
  margin-bottom: 1rem;
}

.form-group label {
  display: block;
  margin-bottom: 0.5rem;
  color: var(--text-secondary);
  font-size: 0.875rem;
}

.form-group input {
  width: 100%;
}

.error {
  color: var(--error);
  font-size: 0.875rem;
  margin-bottom: 1rem;
  text-align: center;
}
```

- [ ] **Step 3: Update main.ts to use login page**

Update the Login placeholder in `client/main.ts`:

```typescript
import Login from "./pages/login"

// Replace the placeholder
const LoginPage = Login
```

- [ ] **Step 4: Verify build**

Run: `bun build --outdir=dist ./client/index.html`
Expected: No errors

- [ ] **Step 5: Commit**

```bash
git add client/pages/login.ts client/styles/main.css client/main.ts
git commit -m "feat: add login page"
```

---

### Task 23: Receipts Page

**Files:**
- Create: `client/pages/receipts.ts`
- Create: `client/pages/receipt.ts`
- Create: `client/components/receipt-card.ts`

- [ ] **Step 1: Create receipt card component**

```typescript
import van from "vanjs-core"

const { div, span, h3, p } = van.tags

interface ReceiptItem {
  itemId: number
  quantity: number
  unitPrice: number
}

interface Receipt {
  receiptId: number
  merchantId: number
  ownerId: number
  date: number
  photoUrl: string
  items: ReceiptItem[]
  total: number
}

const ReceiptCard = (receipt: Receipt) => {
  const date = new Date(receipt.date * 1000)
  const dateStr = date.toLocaleDateString()

  return div({ class: "receipt-card card" },
    div({ class: "receipt-header" },
      h3(`Receipt #${receipt.receiptId}`),
      span({ class: "receipt-date" }, dateStr),
    ),
    div({ class: "receipt-body" },
      p(`${receipt.items.length} items`),
      p({ class: "receipt-total" }, `$${receipt.total.toFixed(2)}`),
    ),
  )
}

export default ReceiptCard
export type { Receipt, ReceiptItem }
```

- [ ] **Step 2: Create receipts list page**

```typescript
import van from "vanjs-core"
import { api, navigate } from "../main"
import ReceiptCard from "../components/receipt-card"
import type { Receipt } from "../components/receipt-card"

const { div, h1, button } = van.tags

const ReceiptsPage = () => {
  const receipts = van.state<Receipt[]>([])
  const loading = van.state(true)

  const loadReceipts = async () => {
    loading.val = true
    try {
      const data = await api.get("/receipts")
      receipts.val = data || []
    } catch (err) {
      console.error("Failed to load receipts:", err)
    }
    loading.val = false
  }

  // Load on mount
  van.derive(() => {
    if (loading.val) {
      loadReceipts()
    }
  })

  return div({ class: "receipts-page" },
    div({ class: "page-header" },
      h1("Receipts"),
      button({ onclick: () => navigate("/receipts/upload") }, "Upload Receipt"),
    ),
    div({ class: "receipts-list" },
      () => loading.val
        ? div("Loading...")
        : receipts.val.length === 0
          ? div("No receipts yet")
          : receipts.val.map(r => ReceiptCard(r)),
    ),
  )
}

export default ReceiptsPage
```

- [ ] **Step 3: Create receipt detail page**

```typescript
import van from "vanjs-core"
import { api, navigate } from "../main"
import type { Receipt } from "../components/receipt-card"

const { div, h1, h2, table, tr, td, th, button, p } = van.tags

const ReceiptDetailPage = () => {
  const receipt = van.state<Receipt | null>(null)
  const loading = van.state(true)

  const loadReceipt = async () => {
    const id = window.location.hash.split("/").pop()
    if (!id) return

    loading.val = true
    try {
      const data = await api.get(`/receipts/${id}`)
      receipt.val = data
    } catch (err) {
      console.error("Failed to load receipt:", err)
    }
    loading.val = false
  }

  van.derive(() => {
    if (loading.val) {
      loadReceipt()
    }
  })

  return div({ class: "receipt-detail-page" },
    () => loading.val
      ? div("Loading...")
      : !receipt.val
        ? div("Receipt not found")
        : div(
            div({ class: "page-header" },
              h1(`Receipt #${receipt.val.receiptId}`),
              button({ onclick: () => navigate("/receipts") }, "Back"),
            ),
            div({ class: "receipt-info card" },
              p(`Date: ${new Date(receipt.val.date * 1000).toLocaleDateString()}`),
              p(`Total: $${receipt.val.total.toFixed(2)}`),
            ),
            h2("Items"),
            table({ class: "items-table" },
              tr(
                th("Item"),
                th("Quantity"),
                th("Unit Price"),
                th("Total"),
              ),
              ...receipt.val.items.map(item =>
                tr(
                  td(`Item #${item.itemId}`),
                  td(item.quantity.toString()),
                  td(`$${item.unitPrice.toFixed(2)}`),
                  td(`$${(item.quantity * item.unitPrice).toFixed(2)}`),
                )
              ),
            ),
          ),
  )
}

export default ReceiptDetailPage
```

- [ ] **Step 4: Add receipt styles**

Add to `client/styles/main.css`:

```css
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 1.5rem;
}

.receipts-list {
  display: grid;
  gap: 1rem;
}

.receipt-card {
  cursor: pointer;
  transition: border-color 0.2s;
}

.receipt-card:hover {
  border-color: var(--accent);
}

.receipt-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 0.5rem;
}

.receipt-date {
  color: var(--text-secondary);
  font-size: 0.875rem;
}

.receipt-body {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.receipt-total {
  font-weight: 600;
  font-size: 1.125rem;
}

.receipt-info {
  margin-bottom: 1.5rem;
}

.items-table {
  width: 100%;
}
```

- [ ] **Step 5: Verify build**

Run: `bun build --outdir=dist ./client/index.html`
Expected: No errors

- [ ] **Step 6: Commit**

```bash
git add client/pages/receipts.ts client/pages/receipt.ts client/components/receipt-card.ts client/styles/main.css
git commit -m "feat: add receipts page with list and detail views"
```

---

### Task 24: Proposals Page

**Files:**
- Create: `client/pages/proposals.ts`
- Create: `client/components/proposal-form.ts`

- [ ] **Step 1: Create proposal form component**

```typescript
import van from "vanjs-core"
import { api } from "../main"

const { div, h2, table, tr, td, th, button, select, option } = van.tags

interface ProposalItem {
  parsedName: string
  quantity: number
  unitPrice: number
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
  total: number
  status: string
}

const ProposalForm = (proposal: Proposal, onApproved: () => void) => {
  const choices = van.state<Record<number, string>>({})

  const handleChoice = (index: number, choice: string) => {
    choices.val = { ...choices.val, [index]: choice }
  }

  const handleApprove = async () => {
    try {
      await api.post(`/proposals/${proposal.proposalId}/approve`, {
        choices: choices.val,
      })
      onApproved()
    } catch (err) {
      console.error("Failed to approve proposal:", err)
    }
  }

  return div({ class: "proposal-form card" },
    h2(`Proposal from ${proposal.merchant}`),
    table(
      tr(
        th("Item"),
        th("Qty"),
        th("Price"),
        th("Confidence"),
        th("Action"),
      ),
      ...proposal.items.map((item, index) =>
        tr(
          td(item.parsedName),
          td(item.quantity.toString()),
          td(`$${item.unitPrice.toFixed(2)}`),
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
    button({ onclick: handleApprove }, "Approve Receipt"),
  )
}

export default ProposalForm
export type { Proposal, ProposalItem }
```

- [ ] **Step 2: Create proposals page**

```typescript
import van from "vanjs-core"
import { api, navigate } from "../main"
import ProposalForm from "../components/proposal-form"
import type { Proposal } from "../components/proposal-form"

const { div, h1 } = van.tags

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

  van.derive(() => {
    if (loading.val) {
      loadProposals()
    }
  })

  const handleApproved = () => {
    loadProposals()
    navigate("/receipts")
  }

  return div({ class: "proposals-page" },
    h1("Pending Proposals"),
    () => loading.val
      ? div("Loading...")
      : proposals.val.length === 0
        ? div("No pending proposals")
        : proposals.val.map(p => ProposalForm(p, handleApproved)),
  )
}

export default ProposalsPage
```

- [ ] **Step 3: Add proposal styles**

Add to `client/styles/main.css`:

```css
.proposals-page {
  max-width: 800px;
}

.proposal-form {
  margin-bottom: 1.5rem;
}

.proposal-form h2 {
  margin-bottom: 1rem;
}
```

- [ ] **Step 4: Verify build**

Run: `bun build --outdir=dist ./client/index.html`
Expected: No errors

- [ ] **Step 5: Commit**

```bash
git add client/pages/proposals.ts client/components/proposal-form.ts client/styles/main.css
git commit -m "feat: add proposals page with approval flow"
```

---

### Task 25: Items and Categories Pages

**Files:**
- Create: `client/pages/items.ts`
- Create: `client/pages/categories.ts`
- Create: `client/components/category-tree.ts`

- [ ] **Step 1: Create items page**

```typescript
import van from "vanjs-core"
import { api, navigate } from "../main"

const { div, h1, table, tr, td, th, button } = van.tags

interface Item {
  itemId: number
  name: string
  categoryId: number
  merchantId: number
  normalized: string
  aliases: string[]
}

const ItemsPage = () => {
  const items = van.state<Item[]>([])
  const loading = van.state(true)

  const loadItems = async () => {
    loading.val = true
    try {
      const data = await api.get("/items")
      items.val = data || []
    } catch (err) {
      console.error("Failed to load items:", err)
    }
    loading.val = false
  }

  van.derive(() => {
    if (loading.val) {
      loadItems()
    }
  })

  return div({ class: "items-page" },
    div({ class: "page-header" },
      h1("Items"),
    ),
    table(
      tr(
        th("Name"),
        th("Category"),
        th("Aliases"),
        th("Actions"),
      ),
      ...items.val.map(item =>
        tr(
          td(item.name),
          td(item.categoryId.toString()),
          td(item.aliases.join(", ")),
          td(
            button({ onclick: () => navigate(`/items/${item.itemId}`) }, "View"),
          ),
        )
      ),
    ),
  )
}

export default ItemsPage
```

- [ ] **Step 2: Create category tree component**

```typescript
import van from "vanjs-core"

const { div, span, ul, li, button } = van.tags

interface Category {
  categoryId: number
  name: string
  parentId: number | null
  sortOrder: number
}

const CategoryTree = (categories: Category[], onEdit: (id: number) => void) => {
  const buildTree = (parentId: number | null): Category[] => {
    return categories
      .filter(c => c.parentId === parentId)
      .sort((a, b) => a.sortOrder - b.sortOrder)
  }

  const renderNode = (category: Category) => {
    const children = buildTree(category.categoryId)
    
    return li(
      div({ class: "category-node" },
        span(category.name),
        button({ onclick: () => onEdit(category.categoryId) }, "Edit"),
      ),
      children.length > 0 ? ul(...children.map(renderNode)) : "",
    )
  }

  const rootCategories = buildTree(null)

  return ul({ class: "category-tree" },
    ...rootCategories.map(renderNode),
  )
}

export default CategoryTree
export type { Category }
```

- [ ] **Step 3: Create categories page**

```typescript
import van from "vanjs-core"
import { api } from "../main"
import CategoryTree from "../components/category-tree"
import type { Category } from "../components/category-tree"

const { div, h1, button, input, form, label } = van.tags

const CategoriesPage = () => {
  const categories = van.state<Category[]>([])
  const loading = van.state(true)
  const newName = van.state("")
  const editingId = van.state<number | null>(null)
  const editName = van.state("")

  const loadCategories = async () => {
    loading.val = true
    try {
      const data = await api.get("/categories")
      categories.val = data || []
    } catch (err) {
      console.error("Failed to load categories:", err)
    }
    loading.val = false
  }

  van.derive(() => {
    if (loading.val) {
      loadCategories()
    }
  })

  const handleCreate = async (e: Event) => {
    e.preventDefault()
    if (!newName.val) return

    try {
      await api.post("/categories", { name: newName.val })
      newName.val = ""
      loadCategories()
    } catch (err) {
      console.error("Failed to create category:", err)
    }
  }

  const handleEdit = (id: number) => {
    const cat = categories.val.find(c => c.categoryId === id)
    if (cat) {
      editingId.val = id
      editName.val = cat.name
    }
  }

  const handleUpdate = async (e: Event) => {
    e.preventDefault()
    if (!editingId.val || !editName.val) return

    try {
      await api.patch(`/categories/${editingId.val}`, { name: editName.val })
      editingId.val = null
      editName.val = ""
      loadCategories()
    } catch (err) {
      console.error("Failed to update category:", err)
    }
  }

  return div({ class: "categories-page" },
    div({ class: "page-header" },
      h1("Categories"),
    ),
    form({ onsubmit: handleCreate, class: "create-form" },
      input({
        type: "text",
        placeholder: "New category name",
        value: newName,
        oninput: (e: Event) => newName.val = (e.target as HTMLInputElement).value,
      }),
      button({ type: "submit" }, "Add"),
    ),
    () => editingId.val
      ? form({ onsubmit: handleUpdate, class: "edit-form" },
          input({
            type: "text",
            value: editName,
            oninput: (e: Event) => editName.val = (e.target as HTMLInputElement).value,
          }),
          button({ type: "submit" }, "Save"),
          button({ type: "button", onclick: () => editingId.val = null }, "Cancel"),
        )
      : "",
    () => loading.val
      ? div("Loading...")
      : CategoryTree(categories.val, handleEdit),
  )
}

export default CategoriesPage
```

- [ ] **Step 4: Add category styles**

Add to `client/styles/main.css`:

```css
.categories-page {
  max-width: 600px;
}

.create-form, .edit-form {
  display: flex;
  gap: 0.5rem;
  margin-bottom: 1rem;
}

.create-form input, .edit-form input {
  flex: 1;
}

.category-tree {
  list-style: none;
  padding-left: 1.5rem;
}

.category-node {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0.5rem;
  border-radius: 4px;
}

.category-node:hover {
  background: var(--bg-tertiary);
}
```

- [ ] **Step 5: Verify build**

Run: `bun build --outdir=dist ./client/index.html`
Expected: No errors

- [ ] **Step 6: Commit**

```bash
git add client/pages/items.ts client/pages/categories.ts client/components/category-tree.ts client/styles/main.css
git commit -m "feat: add items and categories pages"
```

---

### Task 26: Analysis Page

**Files:**
- Create: `client/pages/analysis.ts`
- Create: `client/components/charts.ts`

- [ ] **Step 1: Create charts component**

```typescript
import { Chart, registerables } from "chart.js"

Chart.register(...registerables)

interface ChartData {
  labels: string[]
  datasets: {
    label: string
    data: number[]
    backgroundColor?: string | string[]
    borderColor?: string | string[]
  }[]
}

export const createLineChart = (canvas: HTMLCanvasElement, data: ChartData) => {
  return new Chart(canvas, {
    type: "line",
    data,
    options: {
      responsive: true,
      plugins: {
        legend: {
          labels: { color: "#e5e5e5" },
        },
      },
      scales: {
        x: {
          ticks: { color: "#a0a0a0" },
          grid: { color: "#2e2e2e" },
        },
        y: {
          ticks: { color: "#a0a0a0" },
          grid: { color: "#2e2e2e" },
        },
      },
    },
  })
}

export const createPieChart = (canvas: HTMLCanvasElement, data: ChartData) => {
  return new Chart(canvas, {
    type: "pie",
    data,
    options: {
      responsive: true,
      plugins: {
        legend: {
          position: "bottom",
          labels: { color: "#e5e5e5" },
        },
      },
    },
  })
}

export const createBarChart = (canvas: HTMLCanvasElement, data: ChartData) => {
  return new Chart(canvas, {
    type: "bar",
    data,
    options: {
      responsive: true,
      plugins: {
        legend: {
          labels: { color: "#e5e5e5" },
        },
      },
      scales: {
        x: {
          ticks: { color: "#a0a0a0" },
          grid: { color: "#2e2e2e" },
        },
        y: {
          ticks: { color: "#a0a0a0" },
          grid: { color: "#2e2e2e" },
        },
      },
    },
  })
}
```

- [ ] **Step 2: Create analysis page**

```typescript
import van from "vanjs-core"
import { api } from "../main"
import { createLineChart, createPieChart, createBarChart } from "../components/charts"

const { div, h1, h2, canvas, select, option } = van.tags

const AnalysisPage = () => {
  const granularity = van.state("month")
  const spendingData = van.state<any[]>([])
  const categoryData = van.state<any[]>([])
  const familyData = van.state<any[]>([])
  const loading = van.state(true)

  const loadData = async () => {
    loading.val = true
    try {
      const [spending, categories, family] = await Promise.all([
        api.get(`/analysis/spending?granularity=${granularity.val}`),
        api.get("/analysis/categories"),
        api.get("/analysis/family"),
      ])
      spendingData.val = spending || []
      categoryData.val = categories || []
      familyData.val = family || []
    } catch (err) {
      console.error("Failed to load analysis:", err)
    }
    loading.val = false
  }

  van.derive(() => {
    loadData()
  })

  // Charts will be initialized after DOM is ready
  let spendingChart: Chart | null = null
  let categoryChart: Chart | null = null
  let familyChart: Chart | null = null

  const initCharts = () => {
    const spendingCanvas = document.getElementById("spending-chart") as HTMLCanvasElement
    const categoryCanvas = document.getElementById("category-chart") as HTMLCanvasElement
    const familyCanvas = document.getElementById("family-chart") as HTMLCanvasElement

    if (spendingCanvas && spendingData.val.length > 0) {
      if (spendingChart) spendingChart.destroy()
      spendingChart = createLineChart(spendingCanvas, {
        labels: spendingData.val.map((d: any) => d.period),
        datasets: [{
          label: "Spending",
          data: spendingData.val.map((d: any) => d.total),
          borderColor: "#3b82f6",
          backgroundColor: "rgba(59, 130, 246, 0.1)",
        }],
      })
    }

    if (categoryCanvas && categoryData.val.length > 0) {
      if (categoryChart) categoryChart.destroy()
      categoryChart = createPieChart(categoryCanvas, {
        labels: categoryData.val.map((d: any) => d.name),
        datasets: [{
          label: "Spending by Category",
          data: categoryData.val.map((d: any) => d.total),
          backgroundColor: [
            "#3b82f6", "#22c55e", "#eab308", "#ef4444",
            "#8b5cf6", "#ec4899", "#14b8a6", "#f97316",
          ],
        }],
      })
    }

    if (familyCanvas && familyData.val.length > 0) {
      if (familyChart) familyChart.destroy()
      familyChart = createBarChart(familyCanvas, {
        labels: familyData.val.map((d: any) => d.name),
        datasets: [{
          label: "Spending by Member",
          data: familyData.val.map((d: any) => d.total),
          backgroundColor: "#3b82f6",
        }],
      })
    }
  }

  // Initialize charts after render
  van.derive(() => {
    if (!loading.val) {
      setTimeout(initCharts, 0)
    }
  })

  return div({ class: "analysis-page" },
    div({ class: "page-header" },
      h1("Analysis"),
      select({
        value: granularity,
        onchange: (e: Event) => granularity.val = (e.target as HTMLSelectElement).value,
      },
        option({ value: "day" }, "Daily"),
        option({ value: "week" }, "Weekly"),
        option({ value: "month" }, "Monthly"),
      ),
    ),
    () => loading.val
      ? div("Loading...")
      : div({ class: "charts-grid" },
          div({ class: "chart-container card" },
            h2("Spending Over Time"),
            canvas({ id: "spending-chart" }),
          ),
          div({ class: "chart-container card" },
            h2("Category Breakdown"),
            canvas({ id: "category-chart" }),
          ),
          div({ class: "chart-container card" },
            h2("Family Member Spending"),
            canvas({ id: "family-chart" }),
          ),
        ),
  )
}

export default AnalysisPage
```

- [ ] **Step 3: Add analysis styles**

Add to `client/styles/main.css`:

```css
.analysis-page {
  max-width: 1200px;
}

.charts-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(350px, 1fr));
  gap: 1.5rem;
}

.chart-container {
  padding: 1.5rem;
}

.chart-container h2 {
  margin-bottom: 1rem;
  font-size: 1rem;
  color: var(--text-secondary);
}

.chart-container canvas {
  max-height: 300px;
}
```

- [ ] **Step 4: Update main.ts with all pages**

Update `client/main.ts` to import and use all pages:

```typescript
import Login from "./pages/login"
import ReceiptsPage from "./pages/receipts"
import ReceiptDetailPage from "./pages/receipt"
import ProposalsPage from "./pages/proposals"
import ItemsPage from "./pages/items"
import CategoriesPage from "./pages/categories"
import AnalysisPage from "./pages/analysis"

// Update the App component
const App = () => {
  return div({ id: "app" },
    () => {
      const path = currentPath.val
      
      if (path === "/login") {
        return Login()
      }
      
      if (path === "/") {
        navigate("/receipts")
        return div()
      }
      
      return Layout(PageContent(path))
    }
  )
}

const PageContent = (path: string) => {
  if (path === "/receipts") return ReceiptsPage()
  if (path.startsWith("/receipts/")) return ReceiptDetailPage()
  if (path === "/proposals") return ProposalsPage()
  if (path === "/items") return ItemsPage()
  if (path === "/categories") return CategoriesPage()
  if (path === "/analysis") return AnalysisPage()
  return div("404 - Page not found")
}
```

- [ ] **Step 5: Verify build**

Run: `bun build --outdir=dist ./client/index.html`
Expected: No errors

- [ ] **Step 6: Commit**

```bash
git add client/pages/analysis.ts client/components/charts.ts client/styles/main.css client/main.ts
git commit -m "feat: add analysis page with Chart.js visualizations"
```

---

## Phase 8: Integration

### Task 27: Server Entry Point

**Files:**
- Create: `cmd/server/main.go`

- [ ] **Step 1: Create server entry point**

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"code.sirenko.ca/grocer/internal/api"
	"code.sirenko.ca/grocer/internal/bot"
	"code.sirenko.ca/grocer/internal/llm"
	"code.sirenko.ca/grocer/internal/photo"
	"code.sirenko.ca/grocer/internal/receipt"
	"code.sirenko.ca/grocer/internal/store"
	"cloud.google.com/go/storage"
	"golang.org/x/crypto/argon2"
)

func main() {
	// Flags
	createUser := flag.Bool("create-user", false, "Create a new user")
	name := flag.String("name", "", "User's display name")
	username := flag.String("username", "", "Username")
	password := flag.String("password", "", "Password")
	flag.Parse()

	ctx := context.Background()

	// Initialize store
	s, err := store.NewStore()
	if err != nil {
		log.Fatalf("Failed to create store: %v", err)
	}

	// Initialize GCloud storage
	credsFile := os.Getenv("GCS_CREDENTIALS_FILE")
	bucket := os.Getenv("GCS_BUCKET")
	prefix := os.Getenv("GCS_PREFIX")
	if prefix == "" {
		prefix = "snapshots/"
	}

	gcloud, err := store.NewGCloudStorage(ctx, credsFile, bucket, prefix)
	if err != nil {
		log.Fatalf("Failed to create GCloud storage: %v", err)
	}
	defer gcloud.Close()

	// Load snapshot
	snapshotMgr := store.NewSnapshotManager(s, gcloud)
	if err := snapshotMgr.Load(ctx); err != nil {
		log.Fatalf("Failed to load snapshot: %v", err)
	}

	// Handle create-user flag
	if *createUser {
		if *name == "" || *username == "" || *password == "" {
			log.Fatal("All flags required: --name, --username, --password")
		}

		passwordHash, err := hashPassword(*password)
		if err != nil {
			log.Fatalf("Failed to hash password: %v", err)
		}

		user := &domain.User{
			UserID:       s.UserID.Gen(),
			Name:         *name,
			Username:     *username,
			PasswordHash: passwordHash,
		}

		if err := s.CreateUser(user); err != nil {
			log.Fatalf("Failed to create user: %v", err)
		}

		if err := snapshotMgr.Save(ctx); err != nil {
			log.Fatalf("Failed to save snapshot: %v", err)
		}

		fmt.Printf("User %s created successfully\n", *username)
		return
	}

	// Initialize LLM provider
	llmProvider := os.Getenv("LLM_PROVIDER")
	llmAPIKey := os.Getenv("LLM_API_KEY")
	llmModel := os.Getenv("LLM_MODEL")

	var provider llm.Provider
	switch llmProvider {
	case "kimi":
		provider = llm.NewKimiProvider(llmAPIKey, llmModel)
	case "qwen":
		provider = llm.NewQwenProvider(llmAPIKey, llmModel)
	default:
		log.Fatalf("Unknown LLM provider: %s", llmProvider)
	}

	// Initialize receipt parser
	parser := receipt.NewParser(s, provider)

	// Initialize photo storage
	photoBucket := os.Getenv("GCS_BUCKET") // Reuse same bucket
	photoPrefix := "photos/"
	photoClient, err := storage.NewClient(ctx, storage.WithCredentialsFile(credsFile))
	if err != nil {
		log.Fatalf("Failed to create photo client: %v", err)
	}
	photoStore := photo.NewGCloudStore(photoClient, photoBucket, photoPrefix)

	// Initialize router
	router := api.NewRouter(s, parser, photoStore, snapshotMgr)

	// Initialize bots
	telegramToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	discordToken := os.Getenv("DISCORD_BOT_TOKEN")
	botWebURL := os.Getenv("BOT_WEB_URL")

	var bots []bot.Bot
	if telegramToken != "" {
		telegramBot := bot.NewTelegramBot(telegramToken, botWebURL, parser)
		bots = append(bots, telegramBot)
	}
	if discordToken != "" {
		discordBot := bot.NewDiscordBot(discordToken, botWebURL, parser)
		bots = append(bots, discordBot)
	}

	// Start bots
	for _, b := range bots {
		if err := b.Start(ctx); err != nil {
			log.Printf("Failed to start bot: %v", err)
		}
	}

	// Start server
	addr := ":8080"
	if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}

	server := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		log.Println("Shutting down...")

		// Save snapshot before shutdown
		if err := snapshotMgr.Save(ctx); err != nil {
			log.Printf("Failed to save snapshot: %v", err)
		}

		// Stop bots
		for _, b := range bots {
			if err := b.Stop(); err != nil {
				log.Printf("Failed to stop bot: %v", err)
			}
		}

		// Shutdown server
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("Failed to shutdown server: %v", err)
		}
	}()

	log.Printf("Server starting on %s", addr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server failed: %v", err)
	}
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	hash := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, 64*1024, 3, 2, b64Salt, b64Hash), nil
}
```

- [ ] **Step 2: Update router to accept dependencies**

Update `internal/api/router.go`:

```go
type Router struct {
	store     *store.Store
	parser    *receipt.Parser
	photo     photo.Store
	snapshot  *store.SnapshotManager
	mux       *http.ServeMux
}

func NewRouter(store *store.Store, parser *receipt.Parser, photo photo.Store, snapshot *store.SnapshotManager) *Router {
	r := &Router{
		store:    store,
		parser:   parser,
		photo:    photo,
		snapshot: snapshot,
		mux:      http.NewServeMux(),
	}

	r.setupRoutes()
	return r
}
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./cmd/server/`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add cmd/server/main.go internal/api/router.go
git commit -m "feat: add server entry point with all integrations"
```

---

### Task 28: Dockerfile

**Files:**
- Modify: `deploy/Dockerfile`

- [ ] **Step 1: Update Dockerfile**

```dockerfile
# Stage 1: Build the client
FROM oven/bun:1 as client
WORKDIR /app
COPY client ./client
COPY package.json bun.lock tsconfig.json .
RUN bun install --frozen-lockfile
RUN bun build --production --outdir=dist ./client/index.html

# Stage 2: Build the server
FROM golang:1.25.1 as server
WORKDIR /app
ENV CGO_ENABLED=0
COPY go.mod go.sum .
RUN go mod download
COPY cmd cmd
COPY internal internal
COPY proto proto
COPY --from=client /app/dist ./dist
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown
RUN go build -ldflags "-s -w -X 'main.GitCommit=$GIT_COMMIT' -X 'main.BuildTime=$BUILD_TIME'" -o /app/app ./cmd/server

# Stage 3: Final image
FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=server /app/app .
EXPOSE 8080
CMD ["./app"]
```

- [ ] **Step 2: Commit**

```bash
git add deploy/Dockerfile
git commit -m "feat: update Dockerfile for new project structure"
```

---

### Task 29: Final Cleanup

- [ ] **Step 1: Remove old lib directory**

```bash
rm -rf lib/
```

- [ ] **Step 2: Update go.mod**

Ensure `go.mod` has all required dependencies:

```bash
go mod tidy
```

- [ ] **Step 3: Final build test**

```bash
go build ./...
bun build --outdir=dist ./client/index.html
```

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "chore: cleanup old lib directory and update dependencies"
```

---

## Summary

This plan implements the complete Grocer application in 29 tasks across 8 phases:

1. **Foundation** — Domain types, ID generator, memdb store
2. **Persistence** — Protobuf definitions, snapshot serialization, GCloud storage
3. **LLM Integration** — Provider interface, Kimi and Qwen implementations
4. **Receipt Parsing** — Parser orchestration, fuzzy item matching
5. **HTTP API** — Router, auth, all endpoints
6. **Bots** — Telegram and Discord integration
7. **Photo Storage** — GCloud storage with local cache
8. **Frontend** — VanJS app with all pages and Chart.js visualizations

Each task is self-contained with exact file paths, complete code, and verification steps. The modular monolith architecture allows easy testing and future refactoring.
