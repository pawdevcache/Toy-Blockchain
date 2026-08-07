// Package miner performs the proof of work: the deliberate, unavoidable cost of
// adding a block.
//
// The rule is simple. A block's hash is fixed by its contents, so the only field
// a miner may vary is the Nonce. Change the nonce, get a completely different
// hash, and keep going until that hash happens to start with the required number
// of zero digits. There is no shortcut: SHA-256 is not reversible, so the only
// strategy is to guess. Verifying the answer, by contrast, is a single hash.
//
// That asymmetry (expensive to produce, cheap to check) is what makes rewriting
// history costly in a real chain: an attacker would have to redo the work for
// the tampered block and every block after it.
package miner

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"toychain/internal/block"
)

// cancelCheckInterval is how many nonces a worker tries between checks of the
// context. Checking every iteration would cost more than hashing does.
const cancelCheckInterval = 4096

// Result describes both the mined block and what it cost to find it, which is
// the raw material for the difficulty experiments in the research report.
type Result struct {
	Block   block.Block   // the block, with Nonce and Hash filled in
	Hashes  uint64        // how many nonces were tried in total
	Elapsed time.Duration // wall-clock time spent searching
	Workers int           // how many goroutines searched in parallel
}

// HashRate is the average number of hashes computed per second.
func (r Result) HashRate() float64 {
	if r.Elapsed <= 0 {
		return 0
	}
	return float64(r.Hashes) / r.Elapsed.Seconds()
}

// String renders the one-line summary the CLI prints after mining.
func (r Result) String() string {
	return fmt.Sprintf("nonce=%d hashes=%d elapsed=%s rate=%.0f H/s",
		r.Block.Nonce, r.Hashes, r.Elapsed.Round(time.Millisecond), r.HashRate())
}

// Mine searches for a nonce using every available CPU core. Cancelling ctx stops
// the search promptly and returns ctx.Err().
func Mine(ctx context.Context, candidate block.Block) (Result, error) {
	return MineWith(ctx, candidate, runtime.NumCPU())
}

// MineWith is Mine with an explicit worker count. Worker w starts at nonce w and
// steps by the number of workers, so the goroutines partition the nonce space
// without overlapping and without needing a shared counter.
//
// With workers == 1 the search is exhaustive from zero upwards, so it always
// returns the smallest valid nonce: handy for reproducible tests.
func MineWith(ctx context.Context, candidate block.Block, workers int) (Result, error) {
	if workers < 1 {
		workers = 1
	}
	// The first worker to succeed cancels the rest; deferring it also releases
	// the workers when the caller's own context is cancelled.
	ctx, stop := context.WithCancel(ctx)
	defer stop()

	var (
		hashes atomic.Uint64
		wg     sync.WaitGroup
		found  = make(chan block.Block, workers)
		start  = time.Now()
		stride = uint64(workers)
	)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(offset uint64) {
			defer wg.Done()
			// Each worker owns a copy of the block and only ever writes its
			// own Nonce and Hash, so no locking is needed.
			b := candidate
			var tried uint64

			for nonce := offset; ; nonce += stride {
				b.Nonce = nonce
				b.Hash = b.ComputeHash()
				tried++

				if block.MeetsDifficulty(b.Hash, b.Difficulty) {
					found <- b
					stop() // tell the other workers to give up
					break
				}
				if tried%cancelCheckInterval == 0 && ctx.Err() != nil {
					break
				}
				if nonce+stride < nonce {
					break // nonce space exhausted (not reachable in practice)
				}
			}
			hashes.Add(tried) // one atomic per worker, not one per hash
		}(uint64(w))
	}

	wg.Wait()
	close(found)

	winner, ok := <-found
	if !ok {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		return Result{}, fmt.Errorf("no nonce satisfies difficulty %d", candidate.Difficulty)
	}
	return Result{
		Block:   winner,
		Hashes:  hashes.Load(),
		Elapsed: time.Since(start),
		Workers: workers,
	}, nil
}
