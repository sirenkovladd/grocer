package store

import (
	"errors"
	"strings"
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

type Session struct {
	SessionID uint64
	TokenHash string
	UserID    uint64
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
