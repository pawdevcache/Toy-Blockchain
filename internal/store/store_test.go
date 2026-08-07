package store

import (
	"os"
	"path/filepath"
	"testing"

	"toychain/internal/block"
	"toychain/internal/ledger"
)

func sampleBlocks() []block.Block {
	genesis := block.Genesis()
	b := block.New(1, genesis.Hash,
		[]ledger.Transaction{ledger.NewCoinbase("miner", 50, 1)},
		2, block.GenesisTimestamp+60)
	b.Nonce = 12345
	b.Hash = b.ComputeHash()
	return []block.Block{genesis, b}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	// A nested path also proves Save creates missing directories.
	s := New(filepath.Join(t.TempDir(), "data", "chain.json"))
	blocks := sampleBlocks()
	pending := []ledger.Transaction{ledger.NewTransfer("alice", "bob", 7, 99)}

	if err := s.Save(blocks, pending); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, found, err := s.Load()
	if err != nil || !found {
		t.Fatalf("load: found=%v err=%v", found, err)
	}

	if len(loaded.Blocks) != len(blocks) || len(loaded.Pending) != 1 {
		t.Fatalf("loaded %d blocks and %d pending, want %d and 1",
			len(loaded.Blocks), len(loaded.Pending), len(blocks))
	}
	for i, b := range loaded.Blocks {
		if b.Hash != blocks[i].Hash {
			t.Errorf("block %d hash changed in transit: %s vs %s", i, b.Hash, blocks[i].Hash)
		}
		if !b.HasValidHash() {
			t.Errorf("block %d does not rehash to its stored value after a round trip", i)
		}
	}
	if loaded.Pending[0].ID() != pending[0].ID() {
		t.Error("a pending transaction changed identity in transit")
	}
}

// First run: no file yet. That is normal, not a failure.
func TestLoadMissingFileIsNotAnError(t *testing.T) {
	_, found, err := New(filepath.Join(t.TempDir(), "absent.json")).Load()
	if err != nil {
		t.Errorf("a missing chain file must not be an error: %v", err)
	}
	if found {
		t.Error("found must be false when there is no file")
	}
}

func TestLoadRejectsGarbageAndOldFormats(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"not json":      "this is not json",
		"wrong version": `{"version":99,"blocks":[]}`,
	}
	for name, body := range cases {
		path := filepath.Join(dir, name+".json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := New(path).Load(); err == nil {
			t.Errorf("%s must be rejected, not silently accepted", name)
		}
	}
}

// A second Save must replace the first cleanly and leave no temporary file.
func TestSaveIsAtomicAndOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chain.json")
	s := New(path)

	if err := s.Save(sampleBlocks(), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(sampleBlocks()[:1], nil); err != nil {
		t.Fatal(err)
	}

	loaded, _, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Blocks) != 1 {
		t.Errorf("the second save left %d blocks, want 1", len(loaded.Blocks))
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("the temporary file must not survive a successful save")
	}
}
