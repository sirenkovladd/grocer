package store

import (
	"testing"

	"code.sirenko.ca/grocer/internal/domain"
)

// TestTransaction_CommitThenDeferredAbortDoesNotPanic exercises the
// "defer Abort after successful Commit" pattern used by every
// transactional store method. Before the fix this would panic with
// "sync: Unlock of unlocked RWMutex" because Commit and Abort each
// released the same store lock.
//
// If you see this test fail, the finished-flag in Transaction has
// regressed and approve / any future transactional write will crash
// the server.
func TestTransaction_CommitThenDeferredAbortDoesNotPanic(t *testing.T) {
	s, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Set up a pending proposal so ApproveProposalWithTransaction can run.
	proposalID := s.ProposalID.Gen()
	merchantID := s.MerchantID.Gen()
	if err := s.CreateProposal(&domain.Proposal{
		ProposalID: proposalID,
		OwnerID:    1,
		MerchantID: merchantID,
		Merchant:   "Test",
		Status:     "pending",
	}); err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	// Build the approve payload. One item, one receipt.
	items := []*domain.Item{{
		ItemID:     s.ItemID.Gen(),
		Name:       "Test Item",
		Normalized: "test item",
		Aliases:    []string{"Test Item"},
	}}
	receipt := &domain.Receipt{
		ReceiptID:  s.ReceiptID.Gen(),
		MerchantID: merchantID,
		OwnerID:    1,
		Items: []domain.ReceiptItem{{
			ItemID:         items[0].ItemID,
			Quantity:       1,
			UnitPriceCents: 100,
		}},
		TotalCents: 100,
	}

	// Run the approve. If the double-unlock bug is back, this will panic
	// and the test will fail.
	if err := s.ApproveProposalWithTransaction(proposalID, items, receipt); err != nil {
		t.Fatalf("ApproveProposalWithTransaction: %v", err)
	}

	// Verify the proposal is now approved (so we know the Commit actually
	// happened, not just Abort).
	got, err := s.GetProposal(proposalID)
	if err != nil {
		t.Fatalf("GetProposal: %v", err)
	}
	if got.Status != "approved" {
		t.Errorf("status: got %q, want approved", got.Status)
	}
}

// TestTransaction_AbortIsIdempotent confirms Abort can be called multiple
// times without panicking (useful for the common defer-then-explicit-error
// pattern in user code).
func TestTransaction_AbortIsIdempotent(t *testing.T) {
	s, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	txn := s.BeginTransaction()
	txn.Abort()
	txn.Abort() // must not panic
}

// TestTransaction_CommitAfterCommitErrors confirms Commit refuses to
// double-commit (otherwise it would Unlock the already-unlocked mutex
// through the Abort path).
func TestTransaction_CommitAfterCommitErrors(t *testing.T) {
	s, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	txn := s.BeginTransaction()
	if err := txn.Commit(); err != nil {
		t.Fatalf("first Commit: %v", err)
	}
	if err := txn.Commit(); err == nil {
		t.Fatal("second Commit should error, got nil")
	}
}
