// Package cli turns command-line arguments into operations on the chain.
//
// Everything here is plumbing: parse flags, load the chain from disk, run one
// command, save if it changed anything. No blockchain rule lives in this
// package, which is why it can stay this short.
package cli

import (
	"flag"
	"fmt"
	"io"
	"strconv"

	"toychain/internal/chain"
	"toychain/internal/config"
	"toychain/internal/store"
	"toychain/internal/wallet"
)

// command is one CLI verb. Declaring them in a table means the help text and the
// dispatcher can never drift apart: both read this list.
type command struct {
	name    string
	args    string // argument summary shown in help
	summary string
	mutates bool // if true, the chain is saved after a successful run
	run     func(*app, []string) error
}

// app is one command's worth of state: the loaded chain, the local keystore,
// and where to print.
type app struct {
	cfg    config.Config
	chain  *chain.Chain
	store  *store.Store
	wallet *wallet.Keystore
	out    io.Writer
}

func commands() []command {
	return []command{
		{"keygen", "<label>", "create a key pair and show its address", false, (*app).keygen},
		{"keys", "", "list the key pairs in the local keystore", false, (*app).keys},
		{"faucet", "<label|address> <amount>", "mint coins into an account (toy only)", true, (*app).faucet},
		{"send", "<from-label> <to> <amount>", "sign and queue a transfer", true, (*app).send},
		{"mine", "", "mine the pending transactions into a new block", true, (*app).mine},
		{"print", "", "print the chain, newest block last", false, (*app).print},
		{"validate", "", "re-check the whole chain and report the first fault", false, (*app).validate},
		{"balance", "[address]", "show confirmed balances", false, (*app).balance},
		{"pending", "", "list transactions waiting to be mined", false, (*app).pending},
		{"bench", "[max-difficulty] [runs] [workers]", "measure mining cost per difficulty", false, (*app).bench},
	}
}

// Run executes a single command and returns an error if it failed, which the
// caller turns into a non-zero exit status. Output goes to out rather than
// straight to stdout so the commands can be tested.
func Run(args []string, out io.Writer, cfg config.Config) error {
	cfg, rest, err := parseFlags(args, out, cfg)
	if err != nil {
		return err
	}
	if len(rest) == 0 {
		usage(out, cfg)
		return fmt.Errorf("no command given")
	}

	name := rest[0]
	for _, cmd := range commands() {
		if cmd.name != name {
			continue
		}
		a, err := open(cfg, out)
		if err != nil {
			return err
		}
		if err := cmd.run(a, rest[1:]); err != nil {
			return err
		}
		if cmd.mutates {
			return a.store.Save(a.chain.Blocks(), a.chain.Pending())
		}
		return nil
	}

	usage(out, cfg)
	return fmt.Errorf("unknown command %q", name)
}

// parseFlags applies the global flags on top of the configuration that was
// already resolved from defaults and the environment.
func parseFlags(args []string, out io.Writer, cfg config.Config) (config.Config, []string, error) {
	fs := flag.NewFlagSet("tbc", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.Usage = func() { usage(out, cfg) }

	fs.IntVar(&cfg.Difficulty, "difficulty", cfg.Difficulty, "leading zero hex digits a block hash must have")
	fs.IntVar(&cfg.MaxTxPerBlock, "max-tx", cfg.MaxTxPerBlock, "maximum transactions per block, reward included")
	fs.StringVar(&cfg.DataFile, "data", cfg.DataFile, "path to the chain file")
	fs.StringVar(&cfg.KeyFile, "keys", cfg.KeyFile, "path to the local keystore")
	fs.StringVar(&cfg.MinerAddress, "miner", cfg.MinerAddress, "account that receives the block reward")
	fs.Int64Var(&cfg.MiningReward, "reward", cfg.MiningReward, "coins minted per mined block")

	if err := fs.Parse(args); err != nil {
		return cfg, nil, err
	}
	return cfg, fs.Args(), cfg.Validate()
}

// open loads the chain from disk, or starts a new one on first run.
func open(cfg config.Config, out io.Writer) (*app, error) {
	s := store.New(cfg.DataFile)
	data, found, err := s.Load()
	if err != nil {
		return nil, err
	}

	c := chain.New(cfg)
	if found {
		if c, err = chain.Load(cfg, data.Blocks, data.Pending); err != nil {
			return nil, fmt.Errorf("%s holds a chain that no longer validates: %w", s.Path(), err)
		}
	}

	ks, err := wallet.Open(cfg.KeyFile)
	if err != nil {
		return nil, err
	}
	return &app{cfg: cfg, chain: c, store: s, wallet: ks, out: out}, nil
}

func usage(out io.Writer, cfg config.Config) {
	fmt.Fprint(out, "tbc - a toy blockchain\n\nUsage:\n  tbc [flags] <command> [arguments]\n\nCommands:\n")
	for _, c := range commands() {
		fmt.Fprintf(out, "  %-9s %-22s %s\n", c.name, c.args, c.summary)
	}
	fmt.Fprintf(out, "\nFlags (defaults shown, overridable in .env):\n"+
		"  -difficulty %-6d leading zero hex digits required\n"+
		"  -max-tx     %-6d maximum transactions per block\n"+
		"  -reward     %-6d coins minted per mined block\n"+
		"  -miner      %-6s account paid the block reward\n"+
		"  -data       %s\n  -keys       %s\n\nExample:\n"+
		"  tbc keygen alice && tbc keygen bob\n"+
		"  tbc faucet alice 100 && tbc mine && tbc send alice bob 30 && tbc mine && tbc validate\n",
		cfg.Difficulty, cfg.MaxTxPerBlock, cfg.MiningReward, cfg.MinerAddress, cfg.DataFile, cfg.KeyFile)
}

// parseAmount converts a command-line amount, rejecting anything that is not a
// plain positive whole number before it reaches the ledger.
func parseAmount(s string) (int64, error) {
	amount, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("amount %q is not a whole number", s)
	}
	return amount, nil
}

// wantArgs gives a usable message when the argument count is wrong.
func wantArgs(args []string, n int, form string) error {
	if len(args) != n {
		return fmt.Errorf("expected %s", form)
	}
	return nil
}
