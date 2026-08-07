// Package store saves the chain to disk and reads it back, so state survives
// between runs of the CLI.
//
// Only two things are persisted: the blocks, and the transactions still waiting
// to be mined. Balances are deliberately *not* stored. They are derived by
// replaying the blocks on load (see chain.Load), which means a corrupted file
// can never quietly hand someone a balance the blocks do not justify.
package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"toychain/internal/block"
	"toychain/internal/ledger"
)

// Version is the on-disk format version. Storing it means a future format change
// fails loudly instead of being misread as the current one.
const Version = 1

// File is exactly what lands in chain.json.
type File struct {
	Version int                  `json:"version"`
	Blocks  []block.Block        `json:"blocks"`
	Pending []ledger.Transaction `json:"pending"`
}

// Store reads and writes one chain file.
type Store struct{ path string }

// New returns a store backed by the JSON file at path.
func New(path string) *Store { return &Store{path: path} }

// Path is the file this store reads and writes, for display in the CLI.
func (s *Store) Path() string { return s.path }

// Load reads the chain file. A missing file is not an error: it simply means
// this is the first run, and the caller should start a fresh chain.
func (s *Store) Load() (data File, found bool, err error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return File{}, false, nil
	}
	if err != nil {
		return File{}, false, fmt.Errorf("reading %s: %w", s.path, err)
	}
	// Windows editors (Notepad, PowerShell's Set-Content) prepend a UTF-8 byte
	// order mark. It is invalid JSON, so strip it rather than reject a file a
	// user legitimately hand-edited, which is exactly what the tamper
	// experiment in the report asks them to do.
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	if err := json.Unmarshal(raw, &data); err != nil {
		return File{}, false, fmt.Errorf("%s is not valid chain JSON: %w", s.path, err)
	}
	if data.Version != Version {
		return File{}, false, fmt.Errorf("%s was written by format version %d, this build reads version %d",
			s.path, data.Version, Version)
	}
	return data, true, nil
}

// Save writes the chain atomically: it fills a temporary file in the same
// directory and renames it over the target. A rename is atomic on every
// mainstream filesystem, so a crash mid-write leaves the previous chain intact
// rather than a half-written file that would fail to load.
func (s *Store) Save(blocks []block.Block, pending []ledger.Transaction) error {
	if dir := filepath.Dir(s.path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
	}

	// Indented on purpose: the file is meant to be opened, read and (for the
	// tamper experiment in the report) edited by hand.
	raw, err := json.MarshalIndent(File{Version: Version, Blocks: blocks, Pending: pending}, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding chain: %w", err)
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp) // best effort: never leave debris behind
		return fmt.Errorf("replacing %s: %w", s.path, err)
	}
	return nil
}
