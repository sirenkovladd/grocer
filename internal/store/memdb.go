package store

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

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
	snapshot   *GCloudStorage

	// Snapshot debouncing
	snapshotMu       sync.Mutex
	snapshotTimer    *time.Timer
	snapshotDebounce time.Duration
}

type Session struct {
	SessionID uint64 `json:"sessionId,string"`
	TokenHash string `json:"tokenHash"`
	UserID    uint64 `json:"userId,string"`
}

type BotUser struct {
	ExternalID string `json:"externalId"`
	UserID     uint64 `json:"userId,string"`
}

func NewStore() (*Store, error) {
	// Parse snapshot debounce duration from environment (default 5 seconds)
	debounceStr := os.Getenv("SNAPSHOT_DEBOUNCE_SECONDS")
	debounceSeconds := 5
	if debounceStr != "" {
		if d, err := strconv.Atoi(debounceStr); err == nil && d > 0 {
			debounceSeconds = d
		}
	}

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
					"user_id": {
						Name:    "user_id",
						Unique:  true,
						Indexer: &memdb.UintFieldIndex{Field: "UserID"},
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
					"user_id": {
						Name:    "user_id",
						Unique:  false,
						Indexer: &memdb.UintFieldIndex{Field: "UserID"},
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
					"category_id": {
						Name:    "category_id",
						Unique:  false,
						Indexer: &memdb.UintFieldIndex{Field: "CategoryID"},
					},
					"merchant_id": {
						Name:    "merchant_id",
						Unique:  false,
						Indexer: &memdb.UintFieldIndex{Field: "MerchantID"},
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
					"owner_id": {
						Name:    "owner_id",
						Unique:  false,
						Indexer: &memdb.UintFieldIndex{Field: "OwnerID"},
					},
					"merchant_id": {
						Name:    "merchant_id",
						Unique:  false,
						Indexer: &memdb.UintFieldIndex{Field: "MerchantID"},
					},
					"date": {
						Name:    "date",
						Unique:  false,
						Indexer: &memdb.IntFieldIndex{Field: "Date"},
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
					"owner_id": {
						Name:    "owner_id",
						Unique:  false,
						Indexer: &memdb.UintFieldIndex{Field: "OwnerID"},
					},
					"status": {
						Name:    "status",
						Unique:  false,
						Indexer: &memdb.StringFieldIndex{Field: "Status"},
					},
				},
			},
			"bot_users": {
				Name: "bot_users",
				Indexes: map[string]*memdb.IndexSchema{
					"id": {
						Name:    "id",
						Unique:  true,
						Indexer: &memdb.StringFieldIndex{Field: "ExternalID"},
					},
					"user_id": {
						Name:    "user_id",
						Unique:  false,
						Indexer: &memdb.UintFieldIndex{Field: "UserID"},
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
		db:               db,
		UserID:           NewGenerator(),
		SessionID:        NewGenerator(),
		CategoryID:       NewGenerator(),
		MerchantID:       NewGenerator(),
		ItemID:           NewGenerator(),
		ReceiptID:        NewGenerator(),
		ProposalID:       NewGenerator(),
		snapshotDebounce: time.Duration(debounceSeconds) * time.Second,
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
	s.SaveSnapshotAsync(context.Background())
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

func (s *Store) GetUserByUserID(userID uint64) (*domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	raw, err := txn.First("users", "user_id", userID)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, ErrNotFound
	}
	return raw.(*domain.User), nil
}

func (s *Store) ListUsers() ([]*domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	iter, err := txn.Get("users", "id")
	if err != nil {
		return nil, err
	}

	var users []*domain.User
	for raw := iter.Next(); raw != nil; raw = iter.Next() {
		users = append(users, raw.(*domain.User))
	}
	return users, nil
}

func (s *Store) DeleteUser(username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(true)
	defer txn.Abort()

	if err := txn.Delete("users", &domain.User{Username: username}); err != nil {
		return err
	}
	txn.Commit()
	s.SaveSnapshotAsync(context.Background())
	return nil
}

// Session operations

func (s *Store) CreateSession(sess *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(true)
	defer txn.Abort()

	if err := txn.Insert("sessions", sess); err != nil {
		return err
	}
	txn.Commit()
	s.SaveSnapshotAsync(context.Background())
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

func (s *Store) ListSessions() ([]*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	iter, err := txn.Get("sessions", "id")
	if err != nil {
		return nil, err
	}

	var sessions []*Session
	for raw := iter.Next(); raw != nil; raw = iter.Next() {
		sessions = append(sessions, raw.(*Session))
	}
	return sessions, nil
}

func (s *Store) DeleteSession(sessionID uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(true)
	defer txn.Abort()

	if err := txn.Delete("sessions", &Session{SessionID: sessionID}); err != nil {
		return err
	}
	txn.Commit()
	s.SaveSnapshotAsync(context.Background())
	return nil
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
	s.SaveSnapshotAsync(context.Background())
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
	s.SaveSnapshotAsync(context.Background())
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
	s.SaveSnapshotAsync(context.Background())
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
	s.SaveSnapshotAsync(context.Background())
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

func (s *Store) GetMerchantByName(name string) (*domain.Merchant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	// Get all merchants and do case-insensitive comparison
	// Note: memdb doesn't support case-insensitive string index, so we iterate
	iter, err := txn.Get("merchants", "id")
	if err != nil {
		return nil, err
	}

	for raw := iter.Next(); raw != nil; raw = iter.Next() {
		merchant := raw.(*domain.Merchant)
		if strings.EqualFold(merchant.Name, name) {
			return merchant, nil
		}
	}

	return nil, ErrNotFound
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
	s.SaveSnapshotAsync(context.Background())
	return nil
}

func (s *Store) DeleteMerchant(id uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(true)
	defer txn.Abort()

	if err := txn.Delete("merchants", &domain.Merchant{MerchantID: id}); err != nil {
		return err
	}
	txn.Commit()
	s.SaveSnapshotAsync(context.Background())
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
	s.SaveSnapshotAsync(context.Background())
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
	s.SaveSnapshotAsync(context.Background())
	return nil
}

func (s *Store) DeleteItem(id uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(true)
	defer txn.Abort()

	if err := txn.Delete("items", &domain.Item{ItemID: id}); err != nil {
		return err
	}
	txn.Commit()
	s.SaveSnapshotAsync(context.Background())
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
	s.SaveSnapshotAsync(context.Background())
	return nil
}

// UpdateReceipt replaces an existing receipt record. memdb has no
// "update" operation; Insert with the same primary key overwrites.
// Used for inline edits on the receipt detail page (merchant, date,
// total, per-item price/quantity).
func (s *Store) UpdateReceipt(r *domain.Receipt) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(true)
	defer txn.Abort()

	if err := txn.Insert("receipts", r); err != nil {
		return err
	}
	txn.Commit()
	s.SaveSnapshotAsync(context.Background())
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

func (s *Store) DeleteReceipt(id uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(true)
	defer txn.Abort()

	if err := txn.Delete("receipts", &domain.Receipt{ReceiptID: id}); err != nil {
		return err
	}
	txn.Commit()
	s.SaveSnapshotAsync(context.Background())
	return nil
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
	s.SaveSnapshotAsync(context.Background())
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

// FindProposalByHash finds a proposal by its original image hash.
func (s *Store) FindProposalByHash(hash string) (*domain.Proposal, error) {
	if hash == "" {
		return nil, ErrNotFound
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	iter, err := txn.Get("proposals", "id")
	if err != nil {
		return nil, err
	}

	for raw := iter.Next(); raw != nil; raw = iter.Next() {
		p := raw.(*domain.Proposal)
		if p.OriginalHash == hash {
			return p, nil
		}
	}

	return nil, ErrNotFound
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
	s.SaveSnapshotAsync(context.Background())
	return nil
}

func (s *Store) DeleteProposal(id uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(true)
	defer txn.Abort()

	if err := txn.Delete("proposals", &domain.Proposal{ProposalID: id}); err != nil {
		return err
	}
	txn.Commit()
	s.SaveSnapshotAsync(context.Background())
	return nil
}

// BotUser operations

func (s *Store) CreateBotUser(bu *BotUser) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(true)
	defer txn.Abort()

	if err := txn.Insert("bot_users", bu); err != nil {
		return err
	}
	txn.Commit()
	s.SaveSnapshotAsync(context.Background())
	return nil
}

func (s *Store) GetBotUser(externalID string) (*BotUser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	raw, err := txn.First("bot_users", "id", externalID)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, ErrNotFound
	}
	return raw.(*BotUser), nil
}

func (s *Store) ListBotUsers() ([]*BotUser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	iter, err := txn.Get("bot_users", "id")
	if err != nil {
		return nil, err
	}

	var botUsers []*BotUser
	for raw := iter.Next(); raw != nil; raw = iter.Next() {
		botUsers = append(botUsers, raw.(*BotUser))
	}
	return botUsers, nil
}

func (s *Store) DeleteBotUser(externalID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(true)
	defer txn.Abort()

	if err := txn.Delete("bot_users", &BotUser{ExternalID: externalID}); err != nil {
		return err
	}
	txn.Commit()
	s.SaveSnapshotAsync(context.Background())
	return nil
}

func (s *Store) ListReceiptsByDateRange(fromDate, toDate int64) ([]*domain.Receipt, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	// Use the date index to get receipts in range
	iter, err := txn.LowerBound("receipts", "date", fromDate)
	if err != nil {
		return nil, err
	}

	var receipts []*domain.Receipt
	for raw := iter.Next(); raw != nil; raw = iter.Next() {
		receipt := raw.(*domain.Receipt)
		if receipt.Date <= toDate {
			receipts = append(receipts, receipt)
		} else {
			break // Past the end date
		}
	}
	return receipts, nil
}

func (s *Store) ListReceiptsByOwner(ownerID uint64) ([]*domain.Receipt, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

	iter, err := txn.Get("receipts", "owner_id", ownerID)
	if err != nil {
		return nil, err
	}

	var receipts []*domain.Receipt
	for raw := iter.Next(); raw != nil; raw = iter.Next() {
		receipts = append(receipts, raw.(*domain.Receipt))
	}
	return receipts, nil
}

// Token parsing utility

func ParseTokenString(tokenString string) (uint64, string, error) {
	vals := strings.Split(tokenString, ":")
	if len(vals) != 2 {
		return 0, "", errors.New("invalid token string")
	}
	// Simple uint64 parsing
	var id uint64
	for _, c := range vals[0] {
		if c < '0' || c > '9' {
			return 0, "", errors.New("invalid token id")
		}
		id = id*10 + uint64(c-'0')
	}
	return id, vals[1], nil
}

// Snapshot operations

func (s *Store) SetSnapshotStorage(storage *GCloudStorage) {
	s.snapshot = storage
}

func (s *Store) LoadSnapshot(ctx context.Context) error {
	if s.snapshot == nil {
		return nil
	}

	data, err := s.snapshot.Pull(ctx)
	if err != nil {
		return fmt.Errorf("pull snapshot: %w", err)
	}
	if data == nil {
		return nil
	}

	snapshot, err := DeserializeSnapshot(data)
	if err != nil {
		return fmt.Errorf("deserialize snapshot: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(true)
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
	for _, bu := range snapshot.BotUsers {
		if err := txn.Insert("bot_users", bu); err != nil {
			return err
		}
	}
	for _, sess := range snapshot.Sessions {
		if err := txn.Insert("sessions", sess); err != nil {
			return err
		}
	}

	txn.Commit()
	log.Printf("Loaded snapshot: %d users, %d items, %d receipts, %d bot_users, %d sessions",
		len(snapshot.Users), len(snapshot.Items), len(snapshot.Receipts),
		len(snapshot.BotUsers), len(snapshot.Sessions))

	return nil
}

func (s *Store) SaveSnapshot(ctx context.Context) error {
	if s.snapshot == nil {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	txn := s.db.Txn(false)
	defer txn.Abort()

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

	var botUsers []*BotUser
	iter, _ = txn.Get("bot_users", "id")
	for raw := iter.Next(); raw != nil; raw = iter.Next() {
		botUsers = append(botUsers, raw.(*BotUser))
	}

	var sessions []*Session
	iter, _ = txn.Get("sessions", "id")
	for raw := iter.Next(); raw != nil; raw = iter.Next() {
		sessions = append(sessions, raw.(*Session))
	}

	data, err := SerializeSnapshot(&SnapshotData{
		Users:      users,
		Categories: categories,
		Merchants:  merchants,
		Items:      items,
		Receipts:   receipts,
		Proposals:  proposals,
		BotUsers:   botUsers,
		Sessions:   sessions,
	})
	if err != nil {
		return fmt.Errorf("serialize snapshot: %w", err)
	}

	return s.snapshot.Push(ctx, data)
}

func (s *Store) DeleteSessionsByUserID(userID uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn := s.db.Txn(true)
	defer txn.Abort()

	iter, err := txn.Get("sessions", "user_id", userID)
	if err != nil {
		return err
	}

	var toDelete []*Session
	for raw := iter.Next(); raw != nil; raw = iter.Next() {
		toDelete = append(toDelete, raw.(*Session))
	}

	for _, sess := range toDelete {
		if err := txn.Delete("sessions", sess); err != nil {
			return err
		}
	}

	txn.Commit()
	return nil
}

// UpdateProposalStatus changes a proposal's status and optionally sets an error message.
func (s *Store) UpdateProposalStatus(id uint64, status string, errorMsg ...string) error {
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
	if len(errorMsg) > 0 {
		p.Error = errorMsg[0]
	}

	txn2 := s.db.Txn(true)
	defer txn2.Abort()
	if err := txn2.Insert("proposals", p); err != nil {
		return err
	}
	txn2.Commit()
	s.SaveSnapshotAsync(context.Background())
	return nil
}

// UpdateProposalOcrResult stores the OCR markdown and minimum confidence on
// a proposal and sets its status to StatusParsedOCR. Called after OCR
// completes successfully and before the LLM extraction step starts.
func (s *Store) UpdateProposalOcrResult(id uint64, markdown string, minConfidence float32) error {
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
	p.OcrMarkdown = markdown
	p.OcrMinConfidence = minConfidence
	p.Status = "parsed_ocr"

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

// ResetProposalForReparse clears items and OCR results and resets status to
// "uploaded" so the next ParseReceiptAsync runs the full OCR → LLM pipeline.
func (s *Store) ResetProposalForReparse(id uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.resetProposalLocked(id); err != nil {
		return err
	}
	s.SaveSnapshotAsync(context.Background())
	return nil
}

// ResetProposalForReparseKeepOCR is like ResetProposalForReparse but
// preserves OcrMarkdown and OcrMinConfidence. Used by the LLM-text
// reparse path, which reuses the existing OCR result instead of running
// OCR again.
func (s *Store) ResetProposalForReparseKeepOCR(id uint64) error {
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

	// Capture OCR fields before reset.
	p := raw.(*domain.Proposal)
	preservedMarkdown := p.OcrMarkdown
	preservedMinConf := p.OcrMinConfidence

	// Apply the standard reset (clears OCR too).
	if err := s.resetProposalLocked(id); err != nil {
		return err
	}

	// Restore OCR fields on the freshly-reset proposal.
	txn2 := s.db.Txn(true)
	defer txn2.Abort()
	raw2, err := txn2.First("proposals", "id", id)
	if err != nil {
		return err
	}
	p2 := raw2.(*domain.Proposal)
	p2.OcrMarkdown = preservedMarkdown
	p2.OcrMinConfidence = preservedMinConf
	if err := txn2.Insert("proposals", p2); err != nil {
		return err
	}
	txn2.Commit()
	s.SaveSnapshotAsync(context.Background())
	return nil
}

// resetProposalLocked is the shared body of the two reset variants.
// Caller must hold s.mu.
func (s *Store) resetProposalLocked(id uint64) error {
	txn := s.db.Txn(true)
	defer txn.Abort()

	raw, err := txn.First("proposals", "id", id)
	if err != nil {
		return err
	}
	if raw == nil {
		return ErrNotFound
	}

	p := raw.(*domain.Proposal)
	p.Status = "uploaded"
	p.Items = nil
	p.TotalCents = 0
	p.MerchantID = 0
	p.Merchant = ""
	p.Date = 0
	p.OcrMarkdown = ""
	p.OcrMinConfidence = 0
	p.Error = ""

	if err := txn.Insert("proposals", p); err != nil {
		return err
	}
	txn.Commit()
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

// UpdateProposalItems replaces all items on a proposal.
func (s *Store) UpdateProposalItems(id uint64, items []domain.ProposalItem) error {
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
	p.Items = items

	txn2 := s.db.Txn(true)
	defer txn2.Abort()
	if err := txn2.Insert("proposals", p); err != nil {
		return err
	}
	txn2.Commit()
	s.SaveSnapshotAsync(context.Background())
	return nil
}

func (s *Store) SaveSnapshotAsync(ctx context.Context) {
	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()

	// Cancel existing timer
	if s.snapshotTimer != nil {
		s.snapshotTimer.Stop()
	}

	// Create new debounced timer
	s.snapshotTimer = time.AfterFunc(s.snapshotDebounce, func() {
		if err := s.SaveSnapshot(ctx); err != nil {
			log.Printf("Failed to save snapshot: %v", err)
		}
	})
}
