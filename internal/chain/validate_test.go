package chain

import (
	"context"
	"errors"
	"testing"

	"toychain/internal/block"
	"toychain/internal/ledger"
	"toychain/internal/miner"
)

// honestChain returns genesis plus two properly mined blocks:
//
//	block 1: coinbase reward + a faucet of 100 to alice
//	block 2: coinbase reward + alice pays bob 30
func honestChain(t *testing.T) []block.Block {
	t.Helper()
	c := mineFunded(t)
	if err := c.AddTransaction(ledger.NewTransfer("alice", "bob", 30, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Mine(context.Background()); err != nil {
		t.Fatal(err)
	}
	return c.Blocks()
}

// expectFailure asserts that validation fails on a specific block for a specific
// reason, which is the whole point of tamper detection: say which block, and why.
func expectFailure(t *testing.T, blocks []block.Block, height uint64, checks ...string) *ValidationError {
	t.Helper()
	err := Validate(blocks)
	if err == nil {
		t.Fatal("validation passed on a chain that should be rejected")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("got %T, want a *ValidationError naming the offending block", err)
	}
	if ve.Height != height {
		t.Errorf("blamed block %d, want block %d (%v)", ve.Height, height, ve)
	}
	for _, want := range checks {
		if ve.Check == want {
			return ve
		}
	}
	t.Errorf("failed the %q check, want one of %v (%v)", ve.Check, checks, ve)
	return ve
}

// FR-6 / acceptance: an honest chain validates.
func TestHonestChainValidates(t *testing.T) {
	if err := Validate(honestChain(t)); err != nil {
		t.Errorf("an untouched chain must validate: %v", err)
	}
}

// FR-6 / acceptance: tampering with a transaction in an earlier block is caught,
// and each attempt to cover it up runs into the next check.
//
// This is the experiment written up in docs/RESEARCH.md.
func TestTamperingIsDetectedAtEveryLevelOfCoverUp(t *testing.T) {
	t.Run("edit a transaction", func(t *testing.T) {
		blocks := honestChain(t)
		blocks[1].Transactions[1].Amount = 1_000_000 // alice's faucet, inflated
		expectFailure(t, blocks, 1, "merkle root")
	})

	t.Run("edit it and repair the Merkle root", func(t *testing.T) {
		blocks := honestChain(t)
		blocks[1].Transactions[1].Amount = 1_000_000
		blocks[1].MerkleRoot = block.MerkleRoot(blocks[1].Transactions)
		expectFailure(t, blocks, 1, "hash")
	})

	t.Run("edit it and repair both the root and the hash", func(t *testing.T) {
		blocks := honestChain(t)
		blocks[1].Transactions[1].Amount = 1_000_000
		blocks[1].MerkleRoot = block.MerkleRoot(blocks[1].Transactions)
		blocks[1].Hash = blocks[1].ComputeHash()
		// The recomputed hash almost certainly no longer meets the target; even
		// if it did, block 2 still links to the old hash. Without redoing the
		// proof of work for this block and every block after it, the forgery is
		// stuck. That is the property this toy shares with a real chain.
		expectFailure(t, blocks, 1, "difficulty")
	})

	t.Run("re-mine the block but leave the rest of the chain alone", func(t *testing.T) {
		blocks := honestChain(t)
		blocks[1].Transactions[1].Amount = 1_000_000
		blocks[1].MerkleRoot = block.MerkleRoot(blocks[1].Transactions)
		remined, err := miner.MineWith(context.Background(), blocks[1], 1)
		if err != nil {
			t.Fatal(err)
		}
		blocks[1] = remined.Block
		// Block 1 is now internally perfect, so the break shows up one block
		// later: block 2 still points at the hash block 1 used to have.
		expectFailure(t, blocks, 2, "link")
	})
}

func TestValidateCatchesStructuralDamage(t *testing.T) {
	t.Run("empty chain", func(t *testing.T) {
		if err := Validate(nil); err == nil {
			t.Error("an empty chain is not a valid chain")
		}
	})

	t.Run("substituted genesis", func(t *testing.T) {
		blocks := honestChain(t)
		blocks[0].Timestamp++ // any change makes it a different genesis
		blocks[0].Hash = blocks[0].ComputeHash()
		expectFailure(t, blocks, 0, "genesis")
	})

	t.Run("height gap", func(t *testing.T) {
		blocks := honestChain(t)
		blocks[2].Height = 7
		expectFailure(t, blocks, 7, "height")
	})

	t.Run("timestamp moving backwards", func(t *testing.T) {
		blocks := honestChain(t)
		blocks[2].Timestamp = blocks[1].Timestamp - 60
		expectFailure(t, blocks, 2, "timestamp")
	})

	t.Run("broken link", func(t *testing.T) {
		blocks := honestChain(t)
		blocks[2].PrevHash = block.GenesisPrevHash
		expectFailure(t, blocks, 2, "link")
	})
}

// A block can be flawlessly hashed and still be a lie: this one is fully mined
// but spends coins its sender never had. Only replaying the ledger catches it.
func TestValidateCatchesAFullyMinedButUnaffordableBlock(t *testing.T) {
	blocks := honestChain(t)
	tip := blocks[len(blocks)-1]

	forged := block.New(tip.Height+1, tip.Hash,
		[]ledger.Transaction{ledger.NewTransfer("bob", "mallory", 999_999, 1)},
		tip.Difficulty, tip.Timestamp+1)
	mined, err := miner.MineWith(context.Background(), forged, 1)
	if err != nil {
		t.Fatal(err)
	}

	blocks = append(blocks, mined.Block)
	ve := expectFailure(t, blocks, mined.Block.Height, "ledger")
	if ve != nil && ve.Detail == "" {
		t.Error("the ledger failure must explain which transaction was unaffordable")
	}
}
