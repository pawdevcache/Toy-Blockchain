package ledger

import "testing"

// actor is a test account: a key pair with a shorthand for signing transfers.
type actor struct{ KeyPair }

func newActor(t *testing.T) actor {
	t.Helper()
	pair, err := GenerateKey()
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	return actor{pair}
}

// pay builds a transfer signed by this actor. Sign fills in From from the key
// itself, so the sender can never be someone else.
func (a actor) pay(to string, amount, timestamp int64) Transaction {
	return a.Sign(NewTransfer("", to, amount, timestamp))
}

func TestValidateRejectsMalformedTransactions(t *testing.T) {
	alice, bob := newActor(t), newActor(t)

	// A coinbase carrying a signature: minted coins nobody needs to authorise,
	// with credentials attached anyway. Built by blanking the sender of a signed
	// transfer, since Sign always fills the sender in from the key.
	signedCoinbase := alice.pay(bob.Address(), 5, 1)
	signedCoinbase.From = Coinbase

	bad := map[string]Transaction{
		"zero amount":      alice.pay(bob.Address(), 0, 1),
		"negative amount":  alice.pay(bob.Address(), -5, 1),
		"no recipient":     alice.pay("", 5, 1),
		"self transfer":    alice.pay(alice.Address(), 5, 1),
		"illegal char":     alice.pay("bo|b", 5, 1),
		"coinbase to void": NewCoinbase("", 5, 1),
		"unsigned":         NewTransfer(alice.Address(), bob.Address(), 5, 1),
		"signed coinbase":  signedCoinbase,
	}
	for name, tx := range bad {
		if err := tx.Validate(); err == nil {
			t.Errorf("%s should be rejected", name)
		}
	}

	good := map[string]Transaction{
		"signed transfer": alice.pay(bob.Address(), 5, 1),
		"coinbase":        NewCoinbase("miner", 50, 1),
	}
	for name, tx := range good {
		if err := tx.Validate(); err != nil {
			t.Errorf("%s should be accepted: %v", name, err)
		}
	}
}

// The three things a signature check must catch. Any one of them missing would
// let somebody spend coins they do not own.
func TestVerifySignature(t *testing.T) {
	alice, bob, mallory := newActor(t), newActor(t), newActor(t)

	t.Run("a valid signature verifies", func(t *testing.T) {
		if err := alice.pay(bob.Address(), 10, 1).VerifySignature(); err != nil {
			t.Errorf("an honestly signed transfer must verify: %v", err)
		}
	})

	t.Run("someone else's key cannot spend an account", func(t *testing.T) {
		tx := mallory.pay(bob.Address(), 10, 1)
		tx.From = alice.Address() // claim alice's coins with mallory's signature
		if err := tx.VerifySignature(); err == nil {
			t.Error("a signature from another key must not authorise this account")
		}
	})

	t.Run("fields cannot be altered after signing", func(t *testing.T) {
		for name, tamper := range map[string]func(*Transaction){
			"amount":    func(tx *Transaction) { tx.Amount = 1_000_000 },
			"recipient": func(tx *Transaction) { tx.To = mallory.Address() },
			"timestamp": func(tx *Transaction) { tx.Timestamp++ },
		} {
			tx := alice.pay(bob.Address(), 10, 1)
			tamper(&tx)
			if err := tx.VerifySignature(); err == nil {
				t.Errorf("editing the %s after signing must invalidate the signature", name)
			}
		}
	})

	t.Run("malformed keys and signatures are rejected", func(t *testing.T) {
		tx := alice.pay(bob.Address(), 10, 1)
		tx.Signature = "not hex"
		if err := tx.VerifySignature(); err == nil {
			t.Error("a signature that is not hex must be rejected")
		}
		tx = alice.pay(bob.Address(), 10, 1)
		tx.PubKey = "abcd"
		if err := tx.VerifySignature(); err == nil {
			t.Error("a public key of the wrong length must be rejected")
		}
	})
}

func TestAddressIsDerivedFromTheKey(t *testing.T) {
	alice := newActor(t)
	if got := len(alice.Address()); got != AddressLength {
		t.Errorf("address is %d characters, want %d", got, AddressLength)
	}
	if alice.Address() != AddressOf(alice.Public) {
		t.Error("KeyPair.Address must agree with AddressOf")
	}
	if newActor(t).Address() == alice.Address() {
		t.Error("two independently generated keys must not share an address")
	}

	// A key restored from disk must control the same account.
	restored, err := KeyPairFromHex(alice.PrivateHex())
	if err != nil {
		t.Fatal(err)
	}
	if restored.Address() != alice.Address() {
		t.Error("a restored key must control the same address")
	}
	if _, err := KeyPairFromHex("nonsense"); err == nil {
		t.Error("a corrupt private key must be reported")
	}
}

func TestIDIsDeterministicAndUnambiguous(t *testing.T) {
	alice, bob := newActor(t), newActor(t)
	tx := alice.pay(bob.Address(), 10, 1_700_000_000)

	if tx.ID() != tx.ID() {
		t.Fatal("the same transaction must always produce the same ID")
	}
	if len(tx.ID()) != 64 {
		t.Fatalf("ID = %q, want 64 hex digits", tx.ID())
	}
	// ed25519 signatures are deterministic, so signing twice is identical: the
	// transaction has one identity, not one per signing attempt.
	if alice.pay(bob.Address(), 10, 1_700_000_000).ID() != tx.ID() {
		t.Error("signing the same transaction twice must produce the same ID")
	}

	// Length prefixing means field boundaries cannot be faked: ("ab","c") and
	// ("a","bc") share the concatenation "abc" but must not share an ID.
	if NewTransfer("ab", "c", 1, 0).ID() == NewTransfer("a", "bc", 1, 0).ID() {
		t.Error("addresses must not be able to bleed across field boundaries")
	}
	if tx.ID() == alice.pay(bob.Address(), 11, 1_700_000_000).ID() {
		t.Error("a different amount must produce a different ID")
	}

	// The ID covers the signature, so a block's Merkle root commits to it and a
	// stripped signature cannot pass unnoticed.
	stripped := tx
	stripped.Signature = ""
	if stripped.ID() == tx.ID() {
		t.Error("removing the signature must change the transaction ID")
	}
}

func TestIsCoinbase(t *testing.T) {
	if !NewCoinbase("miner", 50, 1).IsCoinbase() {
		t.Error("a transaction with no sender is a coinbase")
	}
	if newActor(t).pay("miner", 1, 1).IsCoinbase() {
		t.Error("a transfer is not a coinbase")
	}
}
