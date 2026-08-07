// Package config holds every tunable parameter of the toy blockchain in one
// place, so no other package ever has to touch os.Getenv or hard-code a magic
// number.
//
// Resolution order, last one wins:
//
//	built-in defaults -> .env file -> process environment -> CLI flags
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Environment variable names, prefixed to avoid collisions with other tools.
const (
	EnvDifficulty   = "TBC_DIFFICULTY"
	EnvMaxTxPerBlk  = "TBC_MAX_TX_PER_BLOCK"
	EnvDataFile     = "TBC_DATA_FILE"
	EnvKeyFile      = "TBC_KEY_FILE"
	EnvHTTPAddr     = "TBC_HTTP_ADDR"
	EnvMinerAddress = "TBC_MINER_ADDRESS"
	EnvMiningReward = "TBC_MINING_REWARD"
)

// MaxDifficulty is a guard rail: a SHA-256 hash is 64 hex digits long, but
// anything above ~8 would keep a laptop busy for hours, which is not the point
// of this exercise.
const MaxDifficulty = 8

// Config is the fully resolved runtime configuration.
type Config struct {
	// Difficulty is how many leading zero hex digits a block hash must have
	// before the block is considered mined. See internal/miner.
	Difficulty int
	// MaxTxPerBlock caps how many pending transactions are packed per block.
	MaxTxPerBlock int
	// DataFile is the JSON file the chain is saved to and reloaded from.
	DataFile string
	// KeyFile is the local keystore: private keys, never part of the chain.
	KeyFile string
	// HTTPAddr is where `tbc serve` listens. Loopback by default: this API has
	// no authentication and can spend any key in the keystore.
	HTTPAddr string
	// MinerAddress receives the coinbase reward of blocks mined by this node.
	MinerAddress string
	// MiningReward is the amount minted for MinerAddress per mined block.
	MiningReward int64
}

// Default returns the built-in configuration. It is deliberately tuned so a
// reviewer can mine a block in well under a second.
func Default() Config {
	return Config{
		Difficulty:    4,
		MaxTxPerBlock: 10,
		DataFile:      "data/chain.json",
		KeyFile:       "data/keys.json",
		HTTPAddr:      "127.0.0.1:8080",
		MinerAddress:  "miner",
		MiningReward:  50,
	}
}

// Load returns the defaults overlaid with the optional .env file at envPath and
// then with the real process environment. A missing .env file is not an error:
// the defaults are a complete, working configuration on their own.
func Load(envPath string) (Config, error) {
	if err := loadDotEnv(envPath); err != nil {
		return Config{}, err
	}

	c := Default()
	var err error
	if c.Difficulty, err = envInt(EnvDifficulty, c.Difficulty); err != nil {
		return Config{}, err
	}
	if c.MaxTxPerBlock, err = envInt(EnvMaxTxPerBlk, c.MaxTxPerBlock); err != nil {
		return Config{}, err
	}
	reward, err := envInt(EnvMiningReward, int(c.MiningReward))
	if err != nil {
		return Config{}, err
	}
	c.MiningReward = int64(reward)
	c.DataFile = envStr(EnvDataFile, c.DataFile)
	c.KeyFile = envStr(EnvKeyFile, c.KeyFile)
	c.HTTPAddr = envStr(EnvHTTPAddr, c.HTTPAddr)
	c.MinerAddress = envStr(EnvMinerAddress, c.MinerAddress)

	return c, c.Validate()
}

// Validate rejects configurations that would make the node behave nonsensically
// (or hang forever). Called by Load and again after CLI flags are applied.
func (c Config) Validate() error {
	switch {
	case c.Difficulty < 1 || c.Difficulty > MaxDifficulty:
		return fmt.Errorf("difficulty must be between 1 and %d, got %d", MaxDifficulty, c.Difficulty)
	case c.MaxTxPerBlock < 1:
		return fmt.Errorf("max transactions per block must be at least 1, got %d", c.MaxTxPerBlock)
	case c.DataFile == "":
		return fmt.Errorf("data file path must not be empty")
	case c.KeyFile == "":
		return fmt.Errorf("key file path must not be empty")
	case c.HTTPAddr == "":
		return fmt.Errorf("http address must not be empty")
	case c.MinerAddress == "":
		return fmt.Errorf("miner address must not be empty")
	case c.MiningReward < 0:
		return fmt.Errorf("mining reward must not be negative, got %d", c.MiningReward)
	}
	return nil
}

// envStr returns the environment value for key, or def when unset/empty.
func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envInt is envStr for integers; a present-but-unparseable value is an error
// rather than a silent fallback, so typos surface immediately.
func envInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a number", key, v)
	}
	return n, nil
}
