package store

import (
	"context"
	"fmt"

	"code.sirenko.ca/grocer/internal/domain"
	"github.com/hashicorp/go-memdb"
)

// Transaction wraps memdb transactions and provides atomic operations.
//
// The wrapper takes the store write lock in BeginTransaction and releases
// it in Commit or Abort. Callers typically pair a successful Commit with
// a deferred Abort; the finished flag makes Abort a no-op when Commit
// has already run, preventing the double-unlock panic.
type Transaction struct {
	store    *Store
	txn      *memdb.Txn
	finished bool
}

// BeginTransaction starts a new transaction
func (s *Store) BeginTransaction() *Transaction {
	s.mu.Lock()
	return &Transaction{
		store: s,
		txn:   s.db.Txn(true),
	}
}

// Commit commits the transaction and releases the store lock. Calling
// Commit more than once on the same Transaction is an error.
func (t *Transaction) Commit() error {
	if t.finished {
		return fmt.Errorf("transaction already finished")
	}
	t.finished = true
	t.txn.Commit()
	t.store.mu.Unlock()

	// Save snapshot after successful transaction
	t.store.SaveSnapshotAsync(context.Background())
	return nil
}

// Abort rolls back the transaction and releases the store lock. Safe to
// call after Commit; in that case it's a no-op.
func (t *Transaction) Abort() {
	if t.finished {
		return
	}
	t.finished = true
	t.txn.Abort()
	t.store.mu.Unlock()
}

// CreateUser creates a user within a transaction
func (t *Transaction) CreateUser(user *domain.User) error {
	return t.txn.Insert("users", user)
}

// CreateSession creates a session within a transaction
func (t *Transaction) CreateSession(sess *Session) error {
	return t.txn.Insert("sessions", sess)
}

// CreateCategory creates a category within a transaction
func (t *Transaction) CreateCategory(cat *domain.Category) error {
	return t.txn.Insert("categories", cat)
}

// UpdateCategory updates a category within a transaction
func (t *Transaction) UpdateCategory(cat *domain.Category) error {
	return t.txn.Insert("categories", cat)
}

// DeleteCategory deletes a category within a transaction
func (t *Transaction) DeleteCategory(id uint64) error {
	return t.txn.Delete("categories", &domain.Category{CategoryID: id})
}

// CreateMerchant creates a merchant within a transaction
func (t *Transaction) CreateMerchant(m *domain.Merchant) error {
	return t.txn.Insert("merchants", m)
}

// UpdateMerchant updates a merchant within a transaction
func (t *Transaction) UpdateMerchant(m *domain.Merchant) error {
	return t.txn.Insert("merchants", m)
}

// DeleteMerchant deletes a merchant within a transaction
func (t *Transaction) DeleteMerchant(id uint64) error {
	return t.txn.Delete("merchants", &domain.Merchant{MerchantID: id})
}

// CreateItem creates an item within a transaction
func (t *Transaction) CreateItem(item *domain.Item) error {
	return t.txn.Insert("items", item)
}

// UpdateItem updates an item within a transaction
func (t *Transaction) UpdateItem(item *domain.Item) error {
	return t.txn.Insert("items", item)
}

// DeleteItem deletes an item within a transaction
func (t *Transaction) DeleteItem(id uint64) error {
	return t.txn.Delete("items", &domain.Item{ItemID: id})
}

// CreateReceipt creates a receipt within a transaction
func (t *Transaction) CreateReceipt(r *domain.Receipt) error {
	return t.txn.Insert("receipts", r)
}

// DeleteReceipt deletes a receipt within a transaction
func (t *Transaction) DeleteReceipt(id uint64) error {
	return t.txn.Delete("receipts", &domain.Receipt{ReceiptID: id})
}

// CreateProposal creates a proposal within a transaction
func (t *Transaction) CreateProposal(p *domain.Proposal) error {
	return t.txn.Insert("proposals", p)
}

// UpdateProposal updates a proposal within a transaction
func (t *Transaction) UpdateProposal(p *domain.Proposal) error {
	return t.txn.Insert("proposals", p)
}

// DeleteProposal deletes a proposal within a transaction
func (t *Transaction) DeleteProposal(id uint64) error {
	return t.txn.Delete("proposals", &domain.Proposal{ProposalID: id})
}

// CreateBotUser creates a bot user within a transaction
func (t *Transaction) CreateBotUser(bu *BotUser) error {
	return t.txn.Insert("bot_users", bu)
}

// DeleteBotUser deletes a bot user within a transaction
func (t *Transaction) DeleteBotUser(externalID string) error {
	return t.txn.Delete("bot_users", &BotUser{ExternalID: externalID})
}

// DeleteSession deletes a session within a transaction
func (t *Transaction) DeleteSession(sessionID uint64) error {
	return t.txn.Delete("sessions", &Session{SessionID: sessionID})
}

// GetItem retrieves an item within a transaction
func (t *Transaction) GetItem(id uint64) (*domain.Item, error) {
	raw, err := t.txn.First("items", "id", id)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, ErrNotFound
	}
	return raw.(*domain.Item), nil
}

// GetCategory retrieves a category within a transaction
func (t *Transaction) GetCategory(id uint64) (*domain.Category, error) {
	raw, err := t.txn.First("categories", "id", id)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, ErrNotFound
	}
	return raw.(*domain.Category), nil
}

// GetMerchant retrieves a merchant within a transaction
func (t *Transaction) GetMerchant(id uint64) (*domain.Merchant, error) {
	raw, err := t.txn.First("merchants", "id", id)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, ErrNotFound
	}
	return raw.(*domain.Merchant), nil
}

// GetReceipt retrieves a receipt within a transaction
func (t *Transaction) GetReceipt(id uint64) (*domain.Receipt, error) {
	raw, err := t.txn.First("receipts", "id", id)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, ErrNotFound
	}
	return raw.(*domain.Receipt), nil
}

// GetProposal retrieves a proposal within a transaction
func (t *Transaction) GetProposal(id uint64) (*domain.Proposal, error) {
	raw, err := t.txn.First("proposals", "id", id)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, ErrNotFound
	}
	return raw.(*domain.Proposal), nil
}

// ApproveProposal atomically approves a proposal and creates receipt and items
func (s *Store) ApproveProposalWithTransaction(proposalID uint64, items []*domain.Item, receipt *domain.Receipt) error {
	txn := s.BeginTransaction()
	defer txn.Abort()

	// Get the proposal
	proposal, err := txn.GetProposal(proposalID)
	if err != nil {
		return fmt.Errorf("get proposal: %w", err)
	}

	if proposal.Status != "pending" {
		return fmt.Errorf("proposal already %s", proposal.Status)
	}

	// Create all items
	for _, item := range items {
		if err := txn.CreateItem(item); err != nil {
			return fmt.Errorf("create item: %w", err)
		}
	}

	// Create receipt
	if err := txn.CreateReceipt(receipt); err != nil {
		return fmt.Errorf("create receipt: %w", err)
	}

	// Update proposal status
	proposal.Status = "approved"
	if err := txn.UpdateProposal(proposal); err != nil {
		return fmt.Errorf("update proposal: %w", err)
	}

	return txn.Commit()
}
