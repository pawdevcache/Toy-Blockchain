package ledger

import "fmt"

// Mempool holds transactions that have been accepted but not yet mined into a
// block. Nothing in it is final: coins only really move when a block containing
// the transaction is added to the chain.
type Mempool struct {
	pending []Transaction
}

// NewMempool returns an empty pool, optionally pre-filled with transactions
// restored from disk.
func NewMempool(restored ...Transaction) *Mempool {
	return &Mempool{pending: append([]Transaction(nil), restored...)}
}

// Len is the number of transactions waiting to be mined.
func (m *Mempool) Len() int { return len(m.pending) }

// Pending returns a copy of the queue, so callers cannot mutate it by accident.
func (m *Mempool) Pending() []Transaction {
	return append([]Transaction(nil), m.pending...)
}

// Add validates tx against the confirmed state *plus everything already queued*
// and appends it on success.
//
// Checking against the queue as well is what stops the classic double spend:
// alice holds 100 and submits two transfers of 80. Each looks affordable on its
// own, but the second must be rejected, because the first will have spent those
// coins by the time the block is mined.
func (m *Mempool) Add(tx Transaction, confirmed *State) error {
	projected := confirmed.Clone()
	if err := projected.ApplyAll(m.pending); err != nil {
		// The queue no longer fits the confirmed state, meaning the chain moved
		// underneath us. Surfacing that beats quietly accepting more work.
		return fmt.Errorf("pending pool is stale: %w", err)
	}
	if err := projected.CanApply(tx); err != nil {
		return err
	}
	m.pending = append(m.pending, tx)
	return nil
}

// Peek returns up to n queued transactions without removing them. A block is
// built from these; they are only dropped once that block is safely stored.
func (m *Mempool) Peek(n int) []Transaction {
	if n > len(m.pending) {
		n = len(m.pending)
	}
	return append([]Transaction(nil), m.pending[:n]...)
}

// Remove drops the first n transactions, the ones Peek handed out. Calling it
// only after a successful mine means a failed mine loses nothing.
func (m *Mempool) Remove(n int) {
	if n > len(m.pending) {
		n = len(m.pending)
	}
	m.pending = append([]Transaction(nil), m.pending[n:]...)
}
