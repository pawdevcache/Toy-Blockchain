package ledger

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// AddressLength is how many hex characters of the public key digest form an
// address. 16 hex characters is 64 bits: far too short for a real chain, where
// it invites collisions, but it keeps CLI output readable in a toy. The full
// public key is still what signatures are verified against.
const AddressLength = 16

// KeyPair is an ed25519 key and the address derived from it.
//
// ed25519 was chosen over ECDSA because the standard library implements it,
// signatures are deterministic (no risk of a repeated nonce leaking the private
// key, which has cost real chains real money), and there are no curve or hash
// parameters to get wrong.
type KeyPair struct {
	Private ed25519.PrivateKey
	Public  ed25519.PublicKey
}

// GenerateKey creates a new key pair from the system's secure random source.
func GenerateKey() (KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, fmt.Errorf("generating key: %w", err)
	}
	return KeyPair{Private: priv, Public: pub}, nil
}

// KeyPairFromHex restores a key pair from a stored private key.
func KeyPairFromHex(privHex string) (KeyPair, error) {
	raw, err := hex.DecodeString(privHex)
	if err != nil {
		return KeyPair{}, fmt.Errorf("private key is not valid hex: %w", err)
	}
	if len(raw) != ed25519.PrivateKeySize {
		return KeyPair{}, fmt.Errorf("private key is %d bytes, want %d", len(raw), ed25519.PrivateKeySize)
	}
	priv := ed25519.PrivateKey(raw)
	return KeyPair{Private: priv, Public: priv.Public().(ed25519.PublicKey)}, nil
}

// PrivateHex is the private key in the form the keystore writes to disk.
func (k KeyPair) PrivateHex() string { return hex.EncodeToString(k.Private) }

// PublicHex is the public key as it travels inside a transaction.
func (k KeyPair) PublicHex() string { return hex.EncodeToString(k.Public) }

// Address is the account this key controls.
func (k KeyPair) Address() string { return AddressOf(k.Public) }

// AddressOf derives an address from a public key: the first AddressLength hex
// characters of its SHA-256 digest.
//
// Hashing the key rather than using it directly keeps addresses short and means
// the public key is not revealed until the account first spends.
func AddressOf(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:])[:AddressLength]
}

// Sign returns a copy of t authorised by this key pair.
//
// It sets From to the key's own address: a key can only ever sign away its own
// coins, and building the transaction any other way would be a bug waiting for a
// test to find it.
func (k KeyPair) Sign(t Transaction) Transaction {
	t.From = k.Address()
	t.PubKey = k.PublicHex()
	t.Signature = hex.EncodeToString(ed25519.Sign(k.Private, t.signingBytes()))
	return t
}

// VerifySignature checks that a transfer really was authorised by the owner of
// the sending address. Three things must hold, and all three matter:
//
//  1. a public key and signature are present at all;
//  2. that public key hashes to the sender's address, so a valid signature from
//     some other key cannot be used to spend this account's coins;
//  3. the signature verifies against the transaction's signing bytes, so no
//     field can be altered after signing.
func (t Transaction) VerifySignature() error {
	if t.PubKey == "" || t.Signature == "" {
		return fmt.Errorf("transfer from %s is unsigned", t.From)
	}

	pub, err := hex.DecodeString(t.PubKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("public key is not a valid ed25519 key")
	}
	sig, err := hex.DecodeString(t.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("signature is not a valid ed25519 signature")
	}
	if addr := AddressOf(pub); addr != t.From {
		return fmt.Errorf("signing key belongs to %s, not to the sender %s", addr, t.From)
	}
	if !ed25519.Verify(pub, t.signingBytes(), sig) {
		return fmt.Errorf("signature does not match the transaction: it was altered after signing")
	}
	return nil
}
