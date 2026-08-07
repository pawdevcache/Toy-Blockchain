package block

import (
	"testing"

	"toychain/internal/ledger"
)

func txs(n int) []ledger.Transaction {
	out := make([]ledger.Transaction, n)
	for i := range out {
		out[i] = ledger.NewTransfer("alice", "bob", int64(i+1), int64(i))
	}
	return out
}

func TestMerkleRootEmptyAndSingle(t *testing.T) {
	if got := MerkleRoot(nil); got != EmptyMerkleRoot {
		t.Errorf("empty root = %q, want %q", got, EmptyMerkleRoot)
	}
	one := txs(1)
	if got := MerkleRoot(one); got == EmptyMerkleRoot || len(got) != 64 {
		t.Errorf("single-transaction root = %q, want a real 64-digit digest", got)
	}
}

// Odd levels pair the last node with itself, so the tree must still collapse to
// exactly one root for any count.
func TestMerkleRootHandlesOddAndEvenCounts(t *testing.T) {
	for n := 1; n <= 9; n++ {
		if got := MerkleRoot(txs(n)); len(got) != 64 {
			t.Errorf("root for %d transactions = %q, want 64 hex digits", n, got)
		}
	}
}

func TestMerkleRootIsOrderSensitiveAndDeterministic(t *testing.T) {
	list := txs(4)
	root := MerkleRoot(list)
	if root != MerkleRoot(txs(4)) {
		t.Error("the same transactions must always produce the same root")
	}
	list[0], list[3] = list[3], list[0]
	if MerkleRoot(list) == root {
		t.Error("reordering transactions must change the root")
	}
}

func TestMerkleRootChangesWhenATransactionChanges(t *testing.T) {
	list := txs(5)
	root := MerkleRoot(list)
	list[2].Amount++
	if MerkleRoot(list) == root {
		t.Error("editing one transaction must change the root")
	}
}
