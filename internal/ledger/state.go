package ledger

import (
	"fmt"
	"math"
	"sort"
)

// State is the set of account balances implied by the chain.
//
// It is derived data, never the source of truth: the blocks are. Anyone can
// rebuild an identical State by replaying every transaction from genesis, which
// is exactly what internal/chain does on load. Keeping it that way means a
// corrupted balance can never outlive a restart.
type State struct {
	balances map[string]int64
}

// Account is one address and its balance, used for reporting.
type Account struct {
	Address string `json:"address"`
	Balance int64  `json:"balance"`
}

// NewState returns an empty world: every account has a balance of zero.
func NewState() *State { return &State{balances: make(map[string]int64)} }

// Balance returns the balance of addr; unknown accounts are simply zero.
func (s *State) Balance(addr string) int64 { return s.balances[addr] }

// Accounts lists every account with a non-zero history, sorted by address so the
// CLI output is stable between runs.
func (s *State) Accounts() []Account {
	out := make([]Account, 0, len(s.balances))
	for addr, bal := range s.balances {
		out = append(out, Account{Address: addr, Balance: bal})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	return out
}

// Clone returns a deep copy, so callers can test "what if I applied these
// transactions?" without touching the confirmed state.
func (s *State) Clone() *State {
	c := &State{balances: make(map[string]int64, len(s.balances))}
	for addr, bal := range s.balances {
		c.balances[addr] = bal
	}
	return c
}

// CanApply reports whether tx would be accepted, without changing anything.
//
// Two kinds of failure live here:
//   - malformed (non-positive amount, bad address) - delegated to tx.Validate
//   - unaffordable (spending more than the sender holds) - needs this state
func (s *State) CanApply(tx Transaction) error {
	if err := tx.Validate(); err != nil {
		return err
	}
	if !tx.IsCoinbase() {
		if balance := s.balances[tx.From]; balance < tx.Amount {
			return fmt.Errorf("insufficient funds: %s holds %d, tried to send %d",
				tx.From, balance, tx.Amount)
		}
	}
	// Guard against a credit that would wrap int64 around into a negative
	// balance. Unreachable in normal use, catastrophic if it ever happened.
	if math.MaxInt64-s.balances[tx.To] < tx.Amount {
		return fmt.Errorf("balance of %s would overflow", tx.To)
	}
	return nil
}

// Apply validates tx and, only if it is acceptable, moves the money. On error
// the state is left exactly as it was: there is no half-applied transaction.
func (s *State) Apply(tx Transaction) error {
	if err := s.CanApply(tx); err != nil {
		return err
	}
	if !tx.IsCoinbase() {
		s.balances[tx.From] -= tx.Amount
		if s.balances[tx.From] == 0 {
			delete(s.balances, tx.From) // keep the account list tidy
		}
	}
	s.balances[tx.To] += tx.Amount
	return nil
}

// ApplyAll applies transactions in order and stops at the first failure,
// reporting which one broke. Order matters: alice can only pay bob with coins
// she received earlier in the same block.
//
// It works on a copy and only commits when every transaction succeeds, so a bad
// block can never leave the state half-updated.
func (s *State) ApplyAll(txs []Transaction) error {
	next := s.Clone()
	for i, tx := range txs {
		if err := next.Apply(tx); err != nil {
			return fmt.Errorf("transaction %d (%s): %w", i, tx, err)
		}
	}
	s.balances = next.balances
	return nil
}

// Supply is the total number of coins in existence: useful as a sanity check,
// since it must only ever grow by the coinbase amounts that were minted.
func (s *State) Supply() int64 {
	var total int64
	for _, bal := range s.balances {
		total += bal
	}
	return total
}
