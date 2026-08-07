// Package wallet stores private keys locally and maps friendly labels to the
// addresses they control.
//
// This is a convenience for humans, not part of the chain. The chain only ever
// sees addresses and signatures; "alice" exists purely so a reviewer can type
// something memorable. That is exactly the split a real wallet makes.
package wallet

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"toychain/internal/ledger"
)

// Keystore is a label -> private key map backed by a JSON file.
type Keystore struct {
	path string
	keys map[string]string // label -> hex-encoded ed25519 private key
}

// Open reads the keystore, treating a missing file as an empty one.
func Open(path string) (*Keystore, error) {
	ks := &Keystore{path: path, keys: map[string]string{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ks, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, &ks.keys); err != nil {
		return nil, fmt.Errorf("%s is not a valid keystore: %w", path, err)
	}
	return ks, nil
}

// Create generates a new key pair under label and saves the keystore.
func (k *Keystore) Create(label string) (ledger.KeyPair, error) {
	if label == "" {
		return ledger.KeyPair{}, fmt.Errorf("a key needs a label, e.g. 'alice'")
	}
	if _, exists := k.keys[label]; exists {
		return ledger.KeyPair{}, fmt.Errorf("a key labelled %q already exists", label)
	}
	pair, err := ledger.GenerateKey()
	if err != nil {
		return ledger.KeyPair{}, err
	}
	k.keys[label] = pair.PrivateHex()
	return pair, k.save()
}

// KeyPair returns the key stored under label.
func (k *Keystore) KeyPair(label string) (ledger.KeyPair, error) {
	priv, ok := k.keys[label]
	if !ok {
		return ledger.KeyPair{}, fmt.Errorf("no key labelled %q: run 'tbc keygen %s' first", label, label)
	}
	return ledger.KeyPairFromHex(priv)
}

// Entry is one labelled account, for listing.
type Entry struct {
	Label   string
	Address string
}

// Entries lists every stored key, sorted by label.
func (k *Keystore) Entries() ([]Entry, error) {
	out := make([]Entry, 0, len(k.keys))
	for label := range k.keys {
		pair, err := ledger.KeyPairFromHex(k.keys[label])
		if err != nil {
			return nil, fmt.Errorf("key %q is corrupt: %w", label, err)
		}
		out = append(out, Entry{Label: label, Address: pair.Address()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out, nil
}

// Resolve turns a label into the address it controls. Anything that is not a
// known label is passed through unchanged, so raw addresses work everywhere a
// label does.
func (k *Keystore) Resolve(labelOrAddress string) string {
	if priv, ok := k.keys[labelOrAddress]; ok {
		if pair, err := ledger.KeyPairFromHex(priv); err == nil {
			return pair.Address()
		}
	}
	return labelOrAddress
}

// LabelFor is the reverse lookup, used to annotate output. It returns "" when
// the address is not one of ours, which is normal: most addresses are not.
func (k *Keystore) LabelFor(address string) string {
	entries, err := k.Entries()
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.Address == address {
			return e.Label
		}
	}
	return ""
}

// save writes the keystore with owner-only permissions. These are private keys:
// on a real system they would also be encrypted with a passphrase, which this
// toy deliberately does not pretend to do.
func (k *Keystore) save() error {
	if dir := filepath.Dir(k.path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
	}
	raw, err := json.MarshalIndent(k.keys, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(k.path, append(raw, '\n'), 0o600)
}
