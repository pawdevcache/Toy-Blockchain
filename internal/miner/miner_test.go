package miner

import (
	"context"
	"strings"
	"testing"

	"toychain/internal/block"
	"toychain/internal/ledger"
)

func candidate(difficulty int) block.Block {
	txs := []ledger.Transaction{ledger.NewCoinbase("miner", 50, 1)}
	return block.New(1, strings.Repeat("0", 64), txs, difficulty, 1_700_000_000)
}

// FR-5 / acceptance: "a mined block satisfies the difficulty target, and the
// found nonce reproduces that exact hash".
func TestMinedBlockMeetsTargetAndIsReproducible(t *testing.T) {
	const difficulty = 3

	result, err := Mine(context.Background(), candidate(difficulty))
	if err != nil {
		t.Fatalf("mining: %v", err)
	}

	mined := result.Block
	if !strings.HasPrefix(mined.Hash, strings.Repeat("0", difficulty)) {
		t.Errorf("hash %q does not start with %d zeros", mined.Hash, difficulty)
	}
	if !mined.HasValidHash() {
		t.Error("recomputing the hash from the stored nonce must give the same value")
	}
	if result.Hashes == 0 || result.Elapsed <= 0 {
		t.Errorf("mining must report real effort: %+v", result)
	}
}

// One worker searches from zero upwards, so it must always land on the same
// smallest valid nonce. This is the property the report's difficulty table
// relies on to be comparable between runs.
func TestSingleWorkerIsDeterministic(t *testing.T) {
	first, err := MineWith(context.Background(), candidate(3), 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := MineWith(context.Background(), candidate(3), 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Block.Nonce != second.Block.Nonce || first.Hashes != second.Hashes {
		t.Errorf("single-worker mining is not reproducible: %d/%d vs %d/%d",
			first.Block.Nonce, first.Hashes, second.Block.Nonce, second.Hashes)
	}
	// The nonce is the smallest solution, so every nonce below it must fail.
	b := candidate(3)
	for n := uint64(0); n < first.Block.Nonce; n++ {
		b.Nonce = n
		if block.MeetsDifficulty(b.ComputeHash(), 3) {
			t.Fatalf("nonce %d also solves the block, so %d was not the first",
				n, first.Block.Nonce)
		}
	}
}

// Every worker count must produce a valid block: the parallel search must not
// skip nonces or corrupt the shared candidate.
func TestParallelSearchAgreesWithSerial(t *testing.T) {
	for _, workers := range []int{0, 1, 2, 8} {
		result, err := MineWith(context.Background(), candidate(2), workers)
		if err != nil {
			t.Fatalf("%d workers: %v", workers, err)
		}
		if !result.Block.HasValidHash() || !block.MeetsDifficulty(result.Block.Hash, 2) {
			t.Errorf("%d workers produced an invalid block: %+v", workers, result.Block)
		}
	}
}

// Difficulty 0 is the genesis case: any hash is acceptable, so nonce 0 wins.
func TestZeroDifficultySolvesImmediately(t *testing.T) {
	result, err := MineWith(context.Background(), candidate(0), 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Block.Nonce != 0 || result.Hashes != 1 {
		t.Errorf("difficulty 0 must be solved by the first hash, got %+v", result)
	}
}

// A cancelled context must stop the search instead of burning CPU forever.
func TestCancelledContextStopsMining(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := MineWith(ctx, candidate(8), 2); err == nil {
		t.Fatal("mining an impossible target with a cancelled context must return an error")
	} else if err != context.Canceled {
		t.Errorf("got %v, want context.Canceled", err)
	}
}

func TestHashRate(t *testing.T) {
	if got := (Result{}).HashRate(); got != 0 {
		t.Errorf("HashRate with no elapsed time = %v, want 0", got)
	}
}
