package bench

import (
	"context"
	"math"
	"testing"
)

func TestRunMeasuresEachDifficulty(t *testing.T) {
	samples, err := Run(context.Background(), []int{1, 2}, 2, 1)
	if err != nil {
		t.Fatalf("bench: %v", err)
	}
	if len(samples) != 2 {
		t.Fatalf("got %d samples, want one per difficulty", len(samples))
	}
	for _, s := range samples {
		if s.AvgHashes < 1 || s.AvgElapsed <= 0 {
			t.Errorf("difficulty %d reported no work: %+v", s.Difficulty, s)
		}
	}
	// Harder targets cost more. Mining is random, so this could in principle
	// invert; at 16x apart over two runs it effectively never does.
	if samples[1].AvgHashes <= samples[0].AvgHashes {
		t.Errorf("difficulty 2 (%.0f hashes) should cost more than difficulty 1 (%.0f)",
			samples[1].AvgHashes, samples[0].AvgHashes)
	}
}

func TestExpectedIsSixteenToThePowerOfDifficulty(t *testing.T) {
	for d := 0; d <= 6; d++ {
		want := math.Pow(16, float64(d))
		if got := (Sample{Difficulty: d}).Expected(); got != want {
			t.Errorf("difficulty %d expects %.0f hashes, got %.0f", d, want, got)
		}
	}
}

// A cancelled context must abort the sweep rather than run to completion.
func TestRunStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, []int{8}, 1, 1); err == nil {
		t.Error("a cancelled benchmark must return an error")
	}
}
