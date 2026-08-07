package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"toychain/internal/config"
)

// runner returns a helper that runs commands against one temporary chain file
// and keystore, exactly as a user would in a terminal, plus what was printed.
func runner(t *testing.T) func(args ...string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Difficulty = 2 // keep the suite fast
	cfg.DataFile = filepath.Join(dir, "chain.json")
	cfg.KeyFile = filepath.Join(dir, "keys.json")

	return func(args ...string) (string, error) {
		var out bytes.Buffer
		err := Run(args, &out, cfg)
		return out.String(), err
	}
}

// withKeys returns a runner that already has keys for alice and bob, since a
// transfer cannot exist without the sender's key.
func withKeys(t *testing.T) func(args ...string) (string, error) {
	t.Helper()
	tbc := runner(t)
	for _, label := range []string{"alice", "bob"} {
		if _, err := tbc("keygen", label); err != nil {
			t.Fatalf("keygen %s: %v", label, err)
		}
	}
	return tbc
}

// The end-to-end path from the README: keys, fund, mine, transfer, mine,
// validate.
func TestFullWalkthrough(t *testing.T) {
	tbc := withKeys(t)

	if _, err := tbc("faucet", "alice", "100"); err != nil {
		t.Fatalf("faucet: %v", err)
	}
	if _, err := tbc("mine"); err != nil {
		t.Fatalf("mine: %v", err)
	}
	if _, err := tbc("send", "alice", "bob", "30"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, err := tbc("mine"); err != nil {
		t.Fatalf("mine: %v", err)
	}

	out, err := tbc("balance")
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	// Balances are keyed by address; the keystore annotates the ones it knows.
	for _, want := range []string{"alice", "70", "bob", "30", "total supply"} {
		if !strings.Contains(out, want) {
			t.Errorf("balance output is missing %q:\n%s", want, out)
		}
	}

	out, err = tbc("validate")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !strings.Contains(out, "VALID") {
		t.Errorf("validate output = %q, want it to report the chain as valid", out)
	}

	out, _ = tbc("print")
	if !strings.Contains(out, "genesis") || !strings.Contains(out, "block 2") {
		t.Errorf("print output does not show the whole chain:\n%s", out)
	}
}

// State must survive between invocations: each call above re-read the file.
func TestStatePersistsBetweenCommands(t *testing.T) {
	tbc := withKeys(t)

	address, err := tbc("keys")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(address, "alice") {
		t.Fatalf("the keystore did not survive to the next command:\n%s", address)
	}

	if _, err := tbc("faucet", "alice", "100"); err != nil {
		t.Fatal(err)
	}
	out, err := tbc("pending")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "100") {
		t.Errorf("a queued transaction did not survive to the next command:\n%s", out)
	}
}

func TestCommandsRejectBadInput(t *testing.T) {
	tbc := withKeys(t)
	if _, err := tbc("faucet", "alice", "100"); err != nil {
		t.Fatal(err)
	}
	if _, err := tbc("mine"); err != nil {
		t.Fatal(err)
	}

	bad := map[string][]string{
		"unknown command":     {"teleport"},
		"no command":          {},
		"missing argument":    {"send", "alice", "bob"},
		"amount not a number": {"send", "alice", "bob", "ten"},
		"overspend":           {"send", "alice", "bob", "1000"},
		"unknown flag":        {"-nope", "print"},
		"invalid difficulty":  {"-difficulty", "0", "mine"},
		// Without the sender's private key there is no way to author a
		// transfer at all: this is the point of signatures.
		"sending from an account we hold no key for": {"send", "carol", "bob", "1"},
		"duplicate key label":                        {"keygen", "alice"},
	}
	for name, args := range bad {
		if _, err := tbc(args...); err == nil {
			t.Errorf("%s (%v) should have failed", name, args)
		}
	}
}

// Tamper detection through the CLI: edit the saved JSON, then validate.
// This is the experiment reproduced in docs/RESEARCH.md.
func TestValidateReportsTamperingInTheSavedFile(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Difficulty = 2
	cfg.DataFile = filepath.Join(dir, "chain.json")
	cfg.KeyFile = filepath.Join(dir, "keys.json")
	run := func(args ...string) (string, error) {
		var out bytes.Buffer
		err := Run(args, &out, cfg)
		return out.String(), err
	}

	if _, err := run("keygen", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := run("faucet", "alice", "100"); err != nil {
		t.Fatal(err)
	}
	if _, err := run("mine"); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(cfg.DataFile)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(raw), `"amount": 100`, `"amount": 999999`, 1)
	if tampered == string(raw) {
		t.Fatal("test setup: the faucet amount was not found in the saved file")
	}
	if err := os.WriteFile(cfg.DataFile, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}

	// Loading refuses a chain that no longer validates, so every command that
	// touches it now fails: the tampered file cannot become the new truth.
	out, err := run("validate")
	if err == nil {
		t.Fatal("validation must fail after the file was edited by hand")
	}
	if !strings.Contains(err.Error(), "merkle") {
		t.Errorf("error should name the check that caught it, got: %v\n%s", err, out)
	}
}
