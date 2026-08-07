// Package ledger defines the value that moves through the chain: transactions
// and the account balances derived from them.
//
// It deliberately knows nothing about blocks or mining, so it can never import
// them. The dependency arrows only ever point this way:
//
//	chain -> block -> ledger
package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// Coinbase is the sender of a transaction that mints new coins out of nothing:
// the block reward, and the faucet used to fund accounts in a demo. Real chains
// allow exactly one such transaction per block; this toy allows a faucet too and
// says so plainly in the README.
const Coinbase = ""

// maxAddressLen keeps addresses printable and bounded. This toy uses plain
// human-readable names ("alice") instead of public keys; see the report for how
// key-derived addresses would change this.
const maxAddressLen = 64

// Transaction moves Amount from From to To. It is a value type: once inside a
// block it must never be mutated, because its hash is baked into the block.
type Transaction struct {
	From   string `json:"from"`   // empty means Coinbase (newly minted coins)
	To     string `json:"to"`     // recipient account
	Amount int64  `json:"amount"` // must be > 0
	// Timestamp is Unix nanoseconds. It exists to make otherwise identical
	// transfers (alice -> bob 10, twice) hash differently, so each has its own
	// ID. A production chain uses a signed per-account nonce instead.
	Timestamp int64 `json:"timestamp"`
}

// NewTransfer builds a normal account-to-account transaction.
func NewTransfer(from, to string, amount, timestamp int64) Transaction {
	return Transaction{From: from, To: to, Amount: amount, Timestamp: timestamp}
}

// NewCoinbase builds a minting transaction: no sender, coins appear from thin
// air. Used for the block reward and the faucet.
func NewCoinbase(to string, amount, timestamp int64) Transaction {
	return Transaction{From: Coinbase, To: to, Amount: amount, Timestamp: timestamp}
}

// IsCoinbase reports whether this transaction mints new coins.
func (t Transaction) IsCoinbase() bool { return t.From == Coinbase }

// Validate checks everything that can be decided by looking at the transaction
// alone. It does NOT check that the sender can afford it: that needs the chain
// state and lives in the ledger's State (see state.go).
func (t Transaction) Validate() error {
	if t.Amount <= 0 {
		return fmt.Errorf("amount must be positive, got %d", t.Amount)
	}
	if err := validAddress("recipient", t.To); err != nil {
		return err
	}
	if t.IsCoinbase() {
		return nil // a coinbase legitimately has no sender
	}
	if err := validAddress("sender", t.From); err != nil {
		return err
	}
	if t.From == t.To {
		return fmt.Errorf("sender and recipient are the same account %q", t.From)
	}
	return nil
}

// canonicalBytes is the exact byte sequence that gets hashed:
//
//	tx|<len(from)>:<from>|<len(to)>:<to>|<amount>|<timestamp>
//
// Each address is length-prefixed so no address content can ever be mistaken for
// a field separator: "a|b" and "a" + "b" cannot collide. Field order is fixed
// here and nowhere else, which is what makes the hash reproducible.
func (t Transaction) canonicalBytes() []byte {
	var b strings.Builder
	b.WriteString("tx|")
	writeLenPrefixed(&b, t.From)
	b.WriteByte('|')
	writeLenPrefixed(&b, t.To)
	b.WriteByte('|')
	b.WriteString(strconv.FormatInt(t.Amount, 10))
	b.WriteByte('|')
	b.WriteString(strconv.FormatInt(t.Timestamp, 10))
	return []byte(b.String())
}

// Hash is the raw SHA-256 of the canonical encoding. Used to build Merkle trees.
func (t Transaction) Hash() [32]byte { return sha256.Sum256(t.canonicalBytes()) }

// ID is the hex form of Hash: the transaction's identity, shown by the CLI.
func (t Transaction) ID() string {
	h := t.Hash()
	return hex.EncodeToString(h[:])
}

// String renders a transaction for the CLI, e.g. "alice -> bob : 10".
func (t Transaction) String() string {
	from := t.From
	if t.IsCoinbase() {
		from = "(coinbase)"
	}
	return fmt.Sprintf("%s -> %s : %d", from, t.To, t.Amount)
}

func writeLenPrefixed(b *strings.Builder, s string) {
	b.WriteString(strconv.Itoa(len(s)))
	b.WriteByte(':')
	b.WriteString(s)
}

// validAddress keeps addresses to a small, printable character set. Besides
// catching typos, it guarantees an address can never contain the characters the
// canonical encoding uses as separators.
func validAddress(role, addr string) error {
	if addr == "" {
		return fmt.Errorf("%s address must not be empty", role)
	}
	if len(addr) > maxAddressLen {
		return fmt.Errorf("%s address is longer than %d characters", role, maxAddressLen)
	}
	for _, r := range addr {
		isAllowed := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' || r == '-' || r == '_'
		if !isAllowed {
			return fmt.Errorf("%s address %q contains an unsupported character %q", role, addr, r)
		}
	}
	return nil
}
