// Package block defines a single link in the chain and the rules for hashing it.
//
// The one idea worth understanding here: a block's hash is computed from its own
// contents *and* from the hash of the block before it. Change anything, anywhere,
// and every hash from that point onwards stops matching. That is the whole basis
// of tamper detection (see internal/chain).
package block

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"toychain/internal/ledger"
)

// Block is one entry in the append-only chain.
//
// Fields are split into two groups: everything that feeds the hash, and the
// stored Hash itself, which obviously cannot hash itself.
type Block struct {
	Height       uint64               `json:"height"`        // 0 for genesis, +1 per block
	Timestamp    int64                `json:"timestamp"`     // Unix seconds, when mining started
	Transactions []ledger.Transaction `json:"transactions"`  // the payload
	MerkleRoot   string               `json:"merkle_root"`   // hex digest summarising Transactions
	PrevHash     string               `json:"prev_hash"`     // hex hash of block Height-1
	Difficulty   int                  `json:"difficulty"`    // leading zero hex digits required
	Nonce        uint64               `json:"nonce"`         // the number the miner searched for
	Hash         string               `json:"hash"`          // hex, NOT part of the preimage
}

// New assembles an unmined block: it fills in everything except Nonce and Hash,
// which are the miner's job (see internal/miner).
//
// The timestamp is passed in rather than read from the clock so that tests and
// the genesis block can be fully deterministic.
func New(height uint64, prevHash string, txs []ledger.Transaction, difficulty int, timestamp int64) Block {
	return Block{
		Height:       height,
		Timestamp:    timestamp,
		Transactions: txs,
		MerkleRoot:   MerkleRoot(txs),
		PrevHash:     prevHash,
		Difficulty:   difficulty,
	}
}

// hashPreimage is the exact byte sequence fed to SHA-256:
//
//	block|height|timestamp|prev_hash|merkle_root|difficulty|nonce
//
// Notes on the design, because this is the file a reviewer should read first:
//
//   - The stored Hash is excluded; including it would be circular.
//   - Transactions enter through MerkleRoot, not as raw text. One fixed-length
//     digest stands in for the whole payload, so the preimage stays small and
//     changing any transaction still changes the block hash.
//   - Difficulty is included so a block records the target it was mined against.
//     Without it, re-reading an old chain with a new difficulty setting would
//     either falsely fail validation or let an under-mined block slip through.
//   - Every field is either a number or a hex digest, so '|' can never appear
//     inside a value and the encoding is unambiguous.
//   - Order is defined here once and never anywhere else.
func (b Block) hashPreimage() []byte {
	var s strings.Builder
	s.WriteString("block|")
	s.WriteString(strconv.FormatUint(b.Height, 10))
	s.WriteByte('|')
	s.WriteString(strconv.FormatInt(b.Timestamp, 10))
	s.WriteByte('|')
	s.WriteString(b.PrevHash)
	s.WriteByte('|')
	s.WriteString(b.MerkleRoot)
	s.WriteByte('|')
	s.WriteString(strconv.Itoa(b.Difficulty))
	s.WriteByte('|')
	s.WriteString(strconv.FormatUint(b.Nonce, 10))
	return []byte(s.String())
}

// ComputeHash returns the SHA-256 of the preimage as lowercase hex. It is a pure
// function of the block's fields: same block in, same hash out, always.
func (b Block) ComputeHash() string {
	sum := sha256.Sum256(b.hashPreimage())
	return hex.EncodeToString(sum[:])
}

// HasValidHash reports whether the stored Hash still matches a recomputation.
// This is the check that catches a tampered transaction.
func (b Block) HasValidHash() bool { return b.Hash == b.ComputeHash() }

// HasValidMerkleRoot reports whether MerkleRoot still summarises Transactions.
// Recomputing it separately is what makes an edited transaction detectable even
// if an attacker also fixes up the block hash.
func (b Block) HasValidMerkleRoot() bool { return b.MerkleRoot == MerkleRoot(b.Transactions) }

// MeetsDifficulty reports whether a hex hash starts with the required number of
// zero digits. Difficulty 0 accepts anything, which is how the genesis block is
// exempt from proof-of-work without a special case anywhere in the code.
func MeetsDifficulty(hash string, difficulty int) bool {
	if difficulty > len(hash) {
		return false
	}
	return strings.HasPrefix(hash, strings.Repeat("0", difficulty))
}
