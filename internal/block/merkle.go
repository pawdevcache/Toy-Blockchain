package block

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"toychain/internal/ledger"
)

// EmptyMerkleRoot is the root used for a block with no transactions: 64 zeros.
// A real chain never has an empty block (there is always a coinbase), but the
// genesis block here carries nothing, so the case needs a defined answer.
var EmptyMerkleRoot = strings.Repeat("0", 64)

// MerkleRoot reduces a list of transactions to a single hex digest.
//
// How it works: hash every transaction, then repeatedly hash them together in
// pairs until one value is left.
//
//	   root            <- sha256(H(ab) || H(cd))
//	  /    \
//	H(ab)  H(cd)       <- sha256(H(a) || H(b))
//	 / \    / \
//	a   b  c   d       <- transaction hashes
//
// If a level has an odd number of nodes the last one is paired with itself, the
// same rule Bitcoin uses.
//
// Why bother instead of hashing the raw list? A Merkle root gives the block a
// fixed-size summary of any number of transactions, and it lets someone prove a
// single transaction is in a block by supplying only log2(n) hashes rather than
// the whole block. That proof (SPV) is not implemented here; the root is.
func MerkleRoot(txs []ledger.Transaction) string {
	if len(txs) == 0 {
		return EmptyMerkleRoot
	}

	level := make([][32]byte, len(txs))
	for i, tx := range txs {
		level[i] = tx.Hash()
	}

	for len(level) > 1 {
		next := make([][32]byte, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			right := level[i] // odd node out: pair it with itself
			if i+1 < len(level) {
				right = level[i+1]
			}
			var pair [64]byte
			copy(pair[:32], level[i][:])
			copy(pair[32:], right[:])
			next = append(next, sha256.Sum256(pair[:]))
		}
		level = next
	}
	return hex.EncodeToString(level[0][:])
}
