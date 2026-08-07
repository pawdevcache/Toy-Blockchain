package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"time"

	"toychain/internal/bench"
	"toychain/internal/chain"
	"toychain/internal/config"
	"toychain/internal/ledger"
)

// faucet mints coins into an account so a fresh chain has money to move.
func (a *app) faucet(args []string) error {
	if err := wantArgs(args, 2, "faucet <address> <amount>"); err != nil {
		return err
	}
	amount, err := parseAmount(args[1])
	if err != nil {
		return err
	}
	if err := a.chain.Faucet(args[0], amount); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "queued: %d coins minted for %s (mine a block to confirm)\n", amount, args[0])
	return nil
}

// send queues a transfer. It is checked against confirmed balances plus the
// pending pool immediately, so a bad transfer is refused here and not silently
// carried until mining time.
func (a *app) send(args []string) error {
	if err := wantArgs(args, 3, "send <from> <to> <amount>"); err != nil {
		return err
	}
	amount, err := parseAmount(args[2])
	if err != nil {
		return err
	}
	tx := ledger.NewTransfer(args[0], args[1], amount, time.Now().UnixNano())
	if err := a.chain.AddTransaction(tx); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "queued: %s\n  id %s\n", tx, tx.ID())
	return nil
}

// mine performs the proof of work. Ctrl-C cancels the search cleanly, leaving
// the chain exactly as it was.
func (a *app) mine(args []string) error {
	if err := wantArgs(args, 0, "mine"); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	fmt.Fprintf(a.out, "mining block %d at difficulty %d (%d transactions pending)...\n",
		a.chain.Height()+1, a.cfg.Difficulty, len(a.chain.Pending()))

	result, err := a.chain.Mine(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "mined block %d\n  hash %s\n  %s\n",
		result.Block.Height, result.Block.Hash, result)
	return nil
}

// print renders the whole chain, oldest first.
func (a *app) print(args []string) error {
	if err := wantArgs(args, 0, "print"); err != nil {
		return err
	}
	for _, b := range a.chain.Blocks() {
		label := ""
		if b.Height == 0 {
			label = "  (genesis)"
		}
		fmt.Fprintf(a.out, "\nblock %d%s\n", b.Height, label)
		fmt.Fprintf(a.out, "  time       %s\n", time.Unix(b.Timestamp, 0).UTC().Format(time.RFC3339))
		fmt.Fprintf(a.out, "  hash       %s\n", b.Hash)
		fmt.Fprintf(a.out, "  prev       %s\n", b.PrevHash)
		fmt.Fprintf(a.out, "  merkle     %s\n", b.MerkleRoot)
		fmt.Fprintf(a.out, "  nonce      %d (difficulty %d)\n", b.Nonce, b.Difficulty)
		fmt.Fprintf(a.out, "  transactions (%d)\n", len(b.Transactions))
		for _, tx := range b.Transactions {
			fmt.Fprintf(a.out, "    %s\n", tx)
		}
	}
	fmt.Fprintf(a.out, "\n%d blocks, tip at height %d\n", len(a.chain.Blocks()), a.chain.Height())
	return nil
}

// validate re-checks the chain and names the first block that fails.
func (a *app) validate(args []string) error {
	if err := wantArgs(args, 0, "validate"); err != nil {
		return err
	}
	err := a.chain.Validate()
	if err == nil {
		fmt.Fprintf(a.out, "VALID: %d blocks, every hash, link and balance checks out\n",
			len(a.chain.Blocks()))
		return nil
	}

	var ve *chain.ValidationError
	if errors.As(err, &ve) {
		fmt.Fprintf(a.out, "INVALID\n  first bad block  %d\n  hash             %s\n"+
			"  failed check     %s\n  detail           %s\n", ve.Height, ve.Hash, ve.Check, ve.Detail)
	}
	return err
}

// balance shows one account, or every account when no address is given.
func (a *app) balance(args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("expected balance [address]")
	}
	if len(args) == 1 {
		fmt.Fprintf(a.out, "%-20s %d\n", args[0], a.chain.Balance(args[0]))
		return nil
	}

	accounts := a.chain.Accounts()
	if len(accounts) == 0 {
		fmt.Fprintln(a.out, "no accounts yet: try 'tbc faucet alice 100' then 'tbc mine'")
		return nil
	}
	var total int64
	for _, acc := range accounts {
		fmt.Fprintf(a.out, "%-20s %d\n", acc.Address, acc.Balance)
		total += acc.Balance
	}
	fmt.Fprintf(a.out, "%-20s %d\n", "total supply", total)
	return nil
}

// bench measures mining cost across difficulties and prints a Markdown table,
// ready to paste into the research report. It mines throwaway blocks and never
// touches the stored chain.
//
// Defaults to one worker: a single-threaded search counts every nonce from zero,
// which is what makes the numbers comparable with the 16^difficulty prediction.
func (a *app) bench(args []string) error {
	maxDifficulty, runs, workers := 5, 3, 1
	for i, target := range []*int{&maxDifficulty, &runs, &workers} {
		if len(args) > i {
			n, err := parseAmount(args[i])
			if err != nil || n < 1 {
				return fmt.Errorf("bench arguments must be positive whole numbers, got %q", args[i])
			}
			*target = int(n)
		}
	}
	if maxDifficulty > config.MaxDifficulty {
		return fmt.Errorf("difficulty above %d would take far too long", config.MaxDifficulty)
	}

	difficulties := make([]int, 0, maxDifficulty)
	for d := 1; d <= maxDifficulty; d++ {
		difficulties = append(difficulties, d)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	fmt.Fprintf(a.out, "mining %d block(s) per difficulty with %d worker(s)...\n\n", runs, workers)
	samples, err := bench.Run(ctx, difficulties, runs, workers)
	if err != nil {
		return err
	}

	fmt.Fprintln(a.out, "| Difficulty | Expected hashes | Avg hashes | Measured/expected | Avg time | Hash rate |")
	fmt.Fprintln(a.out, "|---:|---:|---:|---:|---:|---:|")
	for _, s := range samples {
		fmt.Fprintf(a.out, "| %d | %.0f | %.0f | %.2f | %s | %.1f MH/s |\n",
			s.Difficulty, s.Expected(), s.AvgHashes, s.Ratio(),
			s.AvgElapsed.Round(time.Millisecond), s.HashRate()/1e6)
	}
	return nil
}

// pending lists what would go into the next block.
func (a *app) pending(args []string) error {
	if err := wantArgs(args, 0, "pending"); err != nil {
		return err
	}
	txs := a.chain.Pending()
	if len(txs) == 0 {
		fmt.Fprintln(a.out, "nothing pending")
		return nil
	}
	for _, tx := range txs {
		fmt.Fprintf(a.out, "%s\n  id %s\n", tx, tx.ID())
	}
	fmt.Fprintf(a.out, "%d transactions pending\n", len(txs))
	return nil
}
