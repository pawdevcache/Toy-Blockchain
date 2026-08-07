// Package bench measures what proof of work actually costs.
//
// It exists because the research report should quote numbers this machine
// produced, not numbers copied from a textbook. Every figure in
// docs/RESEARCH.md comes from `tbc bench`.
package bench

import (
	"context"
	"math"
	"time"

	"toychain/internal/block"
	"toychain/internal/ledger"
	"toychain/internal/miner"
)

// Sample is the measured cost of mining at one difficulty.
type Sample struct {
	Difficulty int
	Runs       int
	AvgHashes  float64
	AvgElapsed time.Duration
	Workers    int
}

// Expected is how many hashes theory predicts: a hash digit is uniform over 16
// values, so the chance of d leading zeros is 16^-d, and the expected number of
// attempts before a success is 16^d.
func (s Sample) Expected() float64 { return math.Pow(16, float64(s.Difficulty)) }

// Ratio compares what we measured against what theory predicts. Values scatter
// around 1.0: mining is a geometric distribution, so individual runs vary wildly
// even though the mean converges.
func (s Sample) Ratio() float64 { return s.AvgHashes / s.Expected() }

// HashRate is the average hashes per second achieved at this difficulty.
func (s Sample) HashRate() float64 {
	if s.AvgElapsed <= 0 {
		return 0
	}
	return s.AvgHashes / s.AvgElapsed.Seconds()
}

// Run mines `runs` throwaway blocks at each difficulty and averages the cost.
//
// Each run uses a different timestamp, so every run is a genuinely different
// search rather than the same nonce hunt repeated. Nothing here touches the real
// chain: these blocks are mined and discarded.
func Run(ctx context.Context, difficulties []int, runs, workers int) ([]Sample, error) {
	if runs < 1 {
		runs = 1
	}
	samples := make([]Sample, 0, len(difficulties))

	for _, difficulty := range difficulties {
		var totalHashes uint64
		var totalElapsed time.Duration

		for run := 0; run < runs; run++ {
			candidate := block.New(1, block.GenesisPrevHash,
				[]ledger.Transaction{ledger.NewCoinbase("miner", 50, int64(run))},
				difficulty, block.GenesisTimestamp+int64(run))

			result, err := miner.MineWith(ctx, candidate, workers)
			if err != nil {
				return samples, err
			}
			totalHashes += result.Hashes
			totalElapsed += result.Elapsed
		}

		samples = append(samples, Sample{
			Difficulty: difficulty,
			Runs:       runs,
			AvgHashes:  float64(totalHashes) / float64(runs),
			AvgElapsed: totalElapsed / time.Duration(runs),
			Workers:    workers,
		})
	}
	return samples, nil
}
