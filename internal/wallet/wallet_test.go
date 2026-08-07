package wallet

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func tempKeystore(t *testing.T) *Keystore {
	t.Helper()
	// A nested path also proves the keystore creates its directory.
	ks, err := Open(filepath.Join(t.TempDir(), "data", "keys.json"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return ks
}

func TestCreateAndReopen(t *testing.T) {
	ks := tempKeystore(t)
	alice, err := ks.Create("alice")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(alice.Address()) == 0 {
		t.Fatal("a created key must have an address")
	}

	// A second process must find the same key with the same address.
	reopened, err := Open(ks.path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	restored, err := reopened.KeyPair("alice")
	if err != nil {
		t.Fatalf("restoring alice: %v", err)
	}
	if restored.Address() != alice.Address() {
		t.Errorf("restored address %s, want %s", restored.Address(), alice.Address())
	}
}

func TestCreateRejectsDuplicatesAndEmptyLabels(t *testing.T) {
	ks := tempKeystore(t)
	if _, err := ks.Create("alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := ks.Create("alice"); err == nil {
		t.Error("reusing a label must be refused: it would silently orphan the first key")
	}
	if _, err := ks.Create(""); err == nil {
		t.Error("a key needs a label")
	}
}

func TestResolveAndLabelFor(t *testing.T) {
	ks := tempKeystore(t)
	alice, err := ks.Create("alice")
	if err != nil {
		t.Fatal(err)
	}

	if got := ks.Resolve("alice"); got != alice.Address() {
		t.Errorf("Resolve(\"alice\") = %q, want the address %q", got, alice.Address())
	}
	// Anything unknown passes through unchanged, so raw addresses work too.
	if got := ks.Resolve(alice.Address()); got != alice.Address() {
		t.Errorf("a raw address must pass through unchanged, got %q", got)
	}
	if got := ks.Resolve("stranger"); got != "stranger" {
		t.Errorf("an unknown label must pass through unchanged, got %q", got)
	}

	if got := ks.LabelFor(alice.Address()); got != "alice" {
		t.Errorf("LabelFor = %q, want \"alice\"", got)
	}
	if got := ks.LabelFor("0000000000000000"); got != "" {
		t.Errorf("an address we hold no key for has no label, got %q", got)
	}
}

func TestEntriesAreSorted(t *testing.T) {
	ks := tempKeystore(t)
	for _, label := range []string{"zoe", "adam", "molly"} {
		if _, err := ks.Create(label); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := ks.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[0].Label != "adam" || entries[2].Label != "zoe" {
		t.Errorf("entries = %+v, want them sorted by label", entries)
	}
}

func TestMissingFileAndCorruptFile(t *testing.T) {
	ks, err := Open(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Errorf("a missing keystore is a first run, not an error: %v", err)
	}
	if _, err := ks.KeyPair("alice"); err == nil {
		t.Error("asking for a key that does not exist must fail")
	}

	corrupt := filepath.Join(t.TempDir(), "keys.json")
	if err := os.WriteFile(corrupt, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(corrupt); err == nil {
		t.Error("a corrupt keystore must be reported, not silently emptied")
	}
}

// These are private keys: the file must not be world readable.
func TestKeystoreIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file modes are not meaningful on Windows")
	}
	ks := tempKeystore(t)
	if _, err := ks.Create("alice"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(ks.path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("keystore mode is %o, want 600", perm)
	}
}

// The stored form must be a private key that actually round-trips, not some
// truncated or re-encoded version of one.
func TestStoredKeyIsUsable(t *testing.T) {
	ks := tempKeystore(t)
	alice, err := ks.Create("alice")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(ks.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), alice.PrivateHex()) {
		t.Error("the keystore must contain the private key it handed out")
	}
}
