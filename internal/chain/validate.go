package chain

import (
	"fmt"

	"toychain/internal/block"
	"toychain/internal/ledger"
)

// ValidationError reports the first block that fails validation and the exact
// check it failed. Naming the offending block is a requirement: "the chain is
// broken" is not an answer anyone can act on.
type ValidationError struct {
	Height uint64 // height of the offending block
	Hash   string // its stored hash, so it can be found in the file
	Check  string // short name of the rule that failed
	Detail string // what specifically went wrong
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("block %d (%s) failed the %s check: %s",
		e.Height, shortHash(e.Hash), e.Check, e.Detail)
}

// Validate re-derives everything the chain claims and compares it to what is
// stored. It walks from genesis forwards and stops at the first failure.
//
// The seven rules, in the order they are applied to each block:
//
//  1. genesis     - block 0 is byte-for-byte the canonical genesis block
//  2. height      - each block sits exactly one above its predecessor
//  3. timestamp   - time never runs backwards
//  4. link        - prev_hash equals the previous block's stored hash
//  5. merkle root - the root still summarises the transactions actually stored
//  6. hash        - the stored hash equals a fresh recomputation
//  7. difficulty  - that hash really does meet the target the block claims
//
// and finally the transactions are replayed into a ledger, which catches a
// block that is perfectly hashed but spends coins nobody had.
//
// Why both rule 5 and rule 6? The block hash is computed over the Merkle root,
// not over the raw transaction list. Editing a transaction therefore leaves the
// stored hash internally consistent: it still matches its own (now stale)
// preimage. Rule 5 is the check that actually catches the edit; rule 6 catches
// an attacker who repaired the root; rule 7 makes repairing the hash expensive;
// and rule 4 means the next block's link breaks as well. Each rule closes the
// escape route opened by fixing the previous one.
func Validate(blocks []block.Block) error {
	if len(blocks) == 0 {
		return &ValidationError{Check: "genesis", Detail: "the chain is empty"}
	}

	genesis := block.Genesis()
	if blocks[0].Hash != genesis.Hash || blocks[0].PrevHash != block.GenesisPrevHash {
		return fail(blocks[0], "genesis", fmt.Sprintf(
			"block 0 is not the canonical genesis block (want hash %s)", shortHash(genesis.Hash)))
	}

	state := ledger.NewState()
	for i, b := range blocks {
		if i > 0 {
			prev := blocks[i-1]
			if b.Height != prev.Height+1 {
				return fail(b, "height", fmt.Sprintf(
					"height %d does not follow %d", b.Height, prev.Height))
			}
			if b.Timestamp < prev.Timestamp {
				return fail(b, "timestamp", fmt.Sprintf(
					"timestamp %d is older than the previous block's %d", b.Timestamp, prev.Timestamp))
			}
			if b.PrevHash != prev.Hash {
				return fail(b, "link", fmt.Sprintf(
					"prev_hash %s does not match block %d's hash %s",
					shortHash(b.PrevHash), prev.Height, shortHash(prev.Hash)))
			}
		}
		if !b.HasValidMerkleRoot() {
			return fail(b, "merkle root", "the transactions stored in this block no longer "+
				"produce its Merkle root, so at least one of them has been altered")
		}
		if !b.HasValidHash() {
			return fail(b, "hash", fmt.Sprintf(
				"stored hash %s but the fields hash to %s", shortHash(b.Hash), shortHash(b.ComputeHash())))
		}
		if !block.MeetsDifficulty(b.Hash, b.Difficulty) {
			return fail(b, "difficulty", fmt.Sprintf(
				"hash %s does not start with %d zeros", shortHash(b.Hash), b.Difficulty))
		}
		if err := state.ApplyAll(b.Transactions); err != nil {
			return fail(b, "ledger", err.Error())
		}
	}
	return nil
}

func fail(b block.Block, check, detail string) *ValidationError {
	return &ValidationError{Height: b.Height, Hash: b.Hash, Check: check, Detail: detail}
}

// shortHash abbreviates a digest for human-readable output: full 64-character
// hashes make error messages unreadable, and the first 12 are plenty to identify
// a block in a toy chain.
func shortHash(h string) string {
	if len(h) <= 12 {
		return h
	}
	return h[:12] + "..."
}
