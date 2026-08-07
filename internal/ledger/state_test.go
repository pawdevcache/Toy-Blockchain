package ledger

import (
	"math"
	"testing"
)

// fund returns a state where addr already holds `amount` coins, minted by a
// coinbase. Coinbases need no signature: nobody authorises newly created coins.
func fund(t *testing.T, addr string, amount int64) *State {
	t.Helper()
	s := NewState()
	if err := s.Apply(NewCoinbase(addr, amount, 1)); err != nil {
		t.Fatalf("funding %s: %v", addr, err)
	}
	return s
}

func TestApplyMovesCoins(t *testing.T) {
	alice, bob := newActor(t), newActor(t)
	s := fund(t, alice.Address(), 100)

	if err := s.Apply(alice.pay(bob.Address(), 30, 2)); err != nil {
		t.Fatalf("a valid transfer must succeed: %v", err)
	}
	if got := s.Balance(alice.Address()); got != 70 {
		t.Errorf("alice = %d, want 70", got)
	}
	if got := s.Balance(bob.Address()); got != 30 {
		t.Errorf("bob = %d, want 30", got)
	}
	if got := s.Supply(); got != 100 {
		t.Errorf("a transfer must not change the total supply: got %d, want 100", got)
	}
}

// FR-4 / acceptance: "an overspending transaction is rejected and the balance is
// unchanged".
func TestApplyRejectsOverspendingWithoutChangingBalances(t *testing.T) {
	alice, bob := newActor(t), newActor(t)
	s := fund(t, alice.Address(), 100)

	if err := s.Apply(alice.pay(bob.Address(), 150, 2)); err == nil {
		t.Fatal("sending 150 from a balance of 100 must be rejected")
	}
	if got := s.Balance(alice.Address()); got != 100 {
		t.Errorf("alice = %d after a rejected transaction, want an untouched 100", got)
	}
	if got := s.Balance(bob.Address()); got != 0 {
		t.Errorf("bob = %d after a rejected transaction, want 0", got)
	}
}

func TestApplyRejectsMalformedAndOverflow(t *testing.T) {
	alice, bob, nobody := newActor(t), newActor(t), newActor(t)
	s := fund(t, alice.Address(), 100)

	if err := s.Apply(alice.pay(bob.Address(), -1, 2)); err == nil {
		t.Error("a negative amount must be rejected")
	}
	if err := s.Apply(nobody.pay(bob.Address(), 1, 2)); err == nil {
		t.Error("an account with no coins cannot send any")
	}
	if err := s.Apply(NewTransfer(alice.Address(), bob.Address(), 10, 2)); err == nil {
		t.Error("an unsigned transfer must be rejected even when it is affordable")
	}

	whale := newActor(t)
	rich := fund(t, whale.Address(), math.MaxInt64)
	if err := rich.Apply(NewCoinbase(whale.Address(), 1, 3)); err == nil {
		t.Error("a credit that would overflow int64 must be rejected")
	}
}

// A batch is atomic: if the third transaction fails, the first two must not have
// moved anything either.
func TestApplyAllIsAtomic(t *testing.T) {
	alice, bob, carol := newActor(t), newActor(t), newActor(t)
	s := fund(t, alice.Address(), 100)

	batch := []Transaction{
		alice.pay(bob.Address(), 40, 2),
		bob.pay(carol.Address(), 40, 3), // fine: bob was paid a line earlier
		alice.pay(carol.Address(), 999, 4),
	}
	if err := s.ApplyAll(batch); err == nil {
		t.Fatal("a batch containing an unaffordable transaction must fail")
	}
	if got := s.Balance(alice.Address()); got != 100 {
		t.Errorf("alice = %d after a failed batch, want an untouched 100", got)
	}
	if got := s.Balance(carol.Address()); got != 0 {
		t.Errorf("carol = %d after a failed batch, want 0", got)
	}

	// Drop the offending transaction and the same batch must go through,
	// including bob spending coins he only received within this batch.
	if err := s.ApplyAll(batch[:2]); err != nil {
		t.Fatalf("valid batch: %v", err)
	}
	if got := s.Balance(carol.Address()); got != 40 {
		t.Errorf("carol = %d, want 40", got)
	}
}

func TestAccountsAreSortedAndCloneIsIndependent(t *testing.T) {
	s := fund(t, "zoe", 10)
	if err := s.Apply(NewCoinbase("adam", 10, 2)); err != nil {
		t.Fatal(err)
	}
	accounts := s.Accounts()
	if len(accounts) != 2 || accounts[0].Address != "adam" {
		t.Errorf("accounts = %+v, want them sorted by address", accounts)
	}

	clone := s.Clone()
	if err := clone.Apply(NewCoinbase("zoe", 5, 3)); err != nil {
		t.Fatal(err)
	}
	if got := s.Balance("zoe"); got != 10 {
		t.Errorf("mutating a clone changed the original: zoe = %d, want 10", got)
	}
}
