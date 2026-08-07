package block

import (
	"strings"
	"testing"

	"toychain/internal/ledger"
)

// sample returns a fixed, fully deterministic block used across these tests.
func sample() Block {
	txs := []ledger.Transaction{
		ledger.NewCoinbase("alice", 100, 1_000),
		ledger.NewTransfer("alice", "bob", 10, 2_000),
	}
	b := New(1, strings.Repeat("a", 64), txs, 3, 1_700_000_000)
	b.Nonce = 42
	b.Hash = b.ComputeHash()
	return b
}

// FR-3 / acceptance: "hashing the same block twice yields the same result".
func TestComputeHashIsDeterministic(t *testing.T) {
	b := sample()
	if b.ComputeHash() != b.ComputeHash() {
		t.Fatal("two hashes of the same block differ; the preimage is not stable")
	}
	if got := len(b.Hash); got != 64 {
		t.Errorf("hash length = %d hex digits, want 64", got)
	}
}

// Every field in the preimage must actually influence the hash. A field that is
// silently ignored would be a field an attacker could edit for free.
func TestEveryHashedFieldChangesTheHash(t *testing.T) {
	base := sample()
	mutations := map[string]func(*Block){
		"height":     func(b *Block) { b.Height++ },
		"timestamp":  func(b *Block) { b.Timestamp++ },
		"prev hash":  func(b *Block) { b.PrevHash = strings.Repeat("b", 64) },
		"difficulty": func(b *Block) { b.Difficulty++ },
		"nonce":      func(b *Block) { b.Nonce++ },
		"a transaction": func(b *Block) { // the tamper case, via the Merkle root
			b.Transactions[1].Amount = 999
			b.MerkleRoot = MerkleRoot(b.Transactions)
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			modified := sample()
			mutate(&modified)
			if modified.ComputeHash() == base.ComputeHash() {
				t.Errorf("changing %s left the hash untouched", name)
			}
		})
	}
}

func TestHasValidHashDetectsAnEditedBlock(t *testing.T) {
	b := sample()
	if !b.HasValidHash() {
		t.Fatal("a freshly hashed block must validate")
	}
	b.Transactions[1].Amount = 999 // tamper, leaving MerkleRoot and Hash stale
	if b.HasValidMerkleRoot() {
		t.Error("the Merkle root must no longer match the edited transactions")
	}
	b.MerkleRoot = MerkleRoot(b.Transactions) // attacker repairs the root...
	if b.HasValidHash() {
		t.Error("...but the block hash must still be stale")
	}
}

func TestMeetsDifficulty(t *testing.T) {
	tests := []struct {
		hash       string
		difficulty int
		want       bool
	}{
		{"000abc", 3, true},
		{"000abc", 4, false},
		{"abc", 0, true},  // difficulty 0 accepts anything (genesis)
		{"", 1, false},    // an empty hash never satisfies a target
		{"0000", 5, false}, // asking for more digits than the hash has
	}
	for _, tc := range tests {
		if got := MeetsDifficulty(tc.hash, tc.difficulty); got != tc.want {
			t.Errorf("MeetsDifficulty(%q, %d) = %v, want %v", tc.hash, tc.difficulty, got, tc.want)
		}
	}
}

// FR-2 / acceptance: "the chain starts from a deterministic genesis block".
func TestGenesisIsDeterministic(t *testing.T) {
	g := Genesis()
	if g.Height != 0 {
		t.Errorf("genesis height = %d, want 0", g.Height)
	}
	if g.PrevHash != GenesisPrevHash {
		t.Errorf("genesis prev-hash = %q, want the fixed value %q", g.PrevHash, GenesisPrevHash)
	}
	if !g.HasValidHash() {
		t.Error("genesis must carry its own correct hash")
	}
	if !MeetsDifficulty(g.Hash, g.Difficulty) {
		t.Error("genesis must satisfy its own (zero) difficulty")
	}
	if g.Hash != Genesis().Hash {
		t.Error("two genesis blocks must be identical; something non-deterministic leaked in")
	}
}
