package ledger

import "testing"

func TestValidateRejectsMalformedTransactions(t *testing.T) {
	bad := map[string]Transaction{
		"zero amount":      NewTransfer("alice", "bob", 0, 1),
		"negative amount":  NewTransfer("alice", "bob", -5, 1),
		"no recipient":     NewTransfer("alice", "", 5, 1),
		"self transfer":    NewTransfer("alice", "alice", 5, 1),
		"illegal char":     NewTransfer("alice", "bo|b", 5, 1),
		"coinbase to void": NewCoinbase("", 5, 1),
	}
	for name, tx := range bad {
		if err := tx.Validate(); err == nil {
			t.Errorf("%s should be rejected", name)
		}
	}

	good := map[string]Transaction{
		"transfer": NewTransfer("alice", "bob", 5, 1),
		"coinbase": NewCoinbase("miner", 50, 1),
	}
	for name, tx := range good {
		if err := tx.Validate(); err != nil {
			t.Errorf("%s should be accepted: %v", name, err)
		}
	}
}

func TestIDIsDeterministicAndUnambiguous(t *testing.T) {
	tx := NewTransfer("alice", "bob", 10, 1_700_000_000)
	if tx.ID() != tx.ID() {
		t.Fatal("the same transaction must always produce the same ID")
	}
	if len(tx.ID()) != 64 {
		t.Fatalf("ID = %q, want 64 hex digits", tx.ID())
	}

	// Length prefixing means field boundaries cannot be faked: ("ab","c") and
	// ("a","bc") share the concatenation "abc" but must not share an ID.
	if NewTransfer("ab", "c", 1, 0).ID() == NewTransfer("a", "bc", 1, 0).ID() {
		t.Error("addresses must not be able to bleed across field boundaries")
	}
	if tx.ID() == NewTransfer("alice", "bob", 11, 1_700_000_000).ID() {
		t.Error("a different amount must produce a different ID")
	}
}

func TestIsCoinbase(t *testing.T) {
	if !NewCoinbase("miner", 50, 1).IsCoinbase() {
		t.Error("a transaction with no sender is a coinbase")
	}
	if NewTransfer("alice", "bob", 1, 1).IsCoinbase() {
		t.Error("a transfer is not a coinbase")
	}
}
