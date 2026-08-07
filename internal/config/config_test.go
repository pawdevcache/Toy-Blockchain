package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUsesDefaultsWhenNothingIsSet(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "absent.env"))
	if err != nil {
		t.Fatalf("Load with a missing .env must not fail: %v", err)
	}
	if got != Default() {
		t.Errorf("got %+v, want defaults %+v", got, Default())
	}
}

func TestLoadReadsDotEnvAndProcessEnvWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	body := "# comment\n\nTBC_DIFFICULTY=3\nTBC_MINER_ADDRESS=\"alice\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	// A real environment variable must beat the file's value.
	t.Setenv(EnvMinerAddress, "bob")

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Difficulty != 3 {
		t.Errorf("difficulty from .env = %d, want 3", got.Difficulty)
	}
	if got.MinerAddress != "bob" {
		t.Errorf("miner address = %q, want the process env value %q", got.MinerAddress, "bob")
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	t.Setenv(EnvDifficulty, "not-a-number")
	if _, err := Load(""); err == nil {
		t.Error("a non-numeric difficulty must be reported, not silently ignored")
	}
}

func TestValidate(t *testing.T) {
	tests := map[string]func(*Config){
		"difficulty too low":  func(c *Config) { c.Difficulty = 0 },
		"difficulty too high": func(c *Config) { c.Difficulty = MaxDifficulty + 1 },
		"empty block":         func(c *Config) { c.MaxTxPerBlock = 0 },
		"no data file":        func(c *Config) { c.DataFile = "" },
		"no key file":         func(c *Config) { c.KeyFile = "" },
		"no miner address":    func(c *Config) { c.MinerAddress = "" },
		"negative reward":     func(c *Config) { c.MiningReward = -1 },
	}
	for name, corrupt := range tests {
		t.Run(name, func(t *testing.T) {
			c := Default()
			corrupt(&c)
			if err := c.Validate(); err == nil {
				t.Errorf("%s should have been rejected", name)
			}
		})
	}
	if err := Default().Validate(); err != nil {
		t.Errorf("the default config must be valid: %v", err)
	}
}
