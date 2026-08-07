package ledger

import "testing"

func TestMempoolAcceptsAffordableTransactions(t *testing.T) {
	alice, bob := newActor(t), newActor(t)
	state := fund(t, alice.Address(), 100)
	pool := NewMempool()

	if err := pool.Add(alice.pay(bob.Address(), 60, 2), state); err != nil {
		t.Fatalf("an affordable transfer must be accepted: %v", err)
	}
	if pool.Len() != 1 {
		t.Errorf("pool holds %d transactions, want 1", pool.Len())
	}
	if state.Balance(alice.Address()) != 100 {
		t.Error("queueing a transaction must not move coins; only mining does")
	}
}

// The double spend: two transfers that each fit on their own, but not together.
func TestMempoolRejectsOverspendAcrossQueuedTransactions(t *testing.T) {
	alice, bob, carol := newActor(t), newActor(t), newActor(t)
	state := fund(t, alice.Address(), 100)
	pool := NewMempool()

	if err := pool.Add(alice.pay(bob.Address(), 80, 2), state); err != nil {
		t.Fatalf("first transfer: %v", err)
	}
	if err := pool.Add(alice.pay(carol.Address(), 80, 3), state); err == nil {
		t.Fatal("the second transfer spends coins the first already committed")
	}
	if pool.Len() != 1 {
		t.Errorf("a rejected transaction must not be queued: pool holds %d", pool.Len())
	}
}

func TestMempoolRejectsUnsignedTransactions(t *testing.T) {
	alice, bob := newActor(t), newActor(t)
	state := fund(t, alice.Address(), 100)
	pool := NewMempool()

	unsigned := NewTransfer(alice.Address(), bob.Address(), 10, 1)
	if err := pool.Add(unsigned, state); err == nil {
		t.Error("an unsigned transfer must never reach the pool")
	}
}

func TestMempoolPeekAndRemove(t *testing.T) {
	alice, bob := newActor(t), newActor(t)
	state := fund(t, alice.Address(), 100)
	pool := NewMempool()
	for i := int64(1); i <= 3; i++ {
		if err := pool.Add(alice.pay(bob.Address(), 10, i), state); err != nil {
			t.Fatal(err)
		}
	}

	batch := pool.Peek(2)
	if len(batch) != 2 {
		t.Fatalf("Peek(2) returned %d transactions", len(batch))
	}
	if pool.Len() != 3 {
		t.Error("Peek must not remove anything: a failed mine should lose nothing")
	}

	pool.Remove(len(batch))
	if pool.Len() != 1 {
		t.Errorf("pool holds %d after removing a mined batch, want 1", pool.Len())
	}
	if pool.Peek(10)[0].Timestamp != 3 {
		t.Error("Remove must drop from the front, keeping the queue in order")
	}
	pool.Remove(99) // more than exists: must clamp, not panic
	if pool.Len() != 0 {
		t.Error("removing more than the pool holds must simply empty it")
	}
}

func TestMempoolRestoresPersistedTransactions(t *testing.T) {
	alice, bob := newActor(t), newActor(t)
	restored := NewMempool(alice.pay(bob.Address(), 5, 1))
	if restored.Len() != 1 {
		t.Errorf("a restored pool holds %d transactions, want 1", restored.Len())
	}
}
