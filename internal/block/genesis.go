package block

import "strings"

// GenesisPrevHash is the fixed, well-known "nothing came before this" value: 64
// zero hex digits, the same width as a real SHA-256 digest.
var GenesisPrevHash = strings.Repeat("0", 64)

// GenesisTimestamp is hard-coded (2026-01-01T00:00:00Z) rather than read from
// the clock. The genesis block must be byte-for-byte identical on every machine,
// otherwise two nodes would disagree about the chain from block 0 onwards.
const GenesisTimestamp int64 = 1767225600

// Genesis builds the first block of the chain.
//
// It carries no transactions and is mined at difficulty 0, so MeetsDifficulty
// accepts it without proof-of-work while every later block is checked normally.
// Nonce stays 0; its hash is simply computed and stored.
func Genesis() Block {
	b := New(0, GenesisPrevHash, nil, 0, GenesisTimestamp)
	b.Hash = b.ComputeHash()
	return b
}
