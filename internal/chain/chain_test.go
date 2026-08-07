package chain

import (
	"context"
	"testing"
	"time"

	"toychain/internal/block"
	"toychain/internal/config"
	"toychain/internal/ledger"
)

// testConfig keeps difficulty low so the whole suite runs in well under a second.
func testConfig() config.Config {
	cfg := config.Default()
	cfg.Difficulty = 2
	cfg.MaxTxPerBlock = 5
	return cfg
}

// tickingClock returns a clock that advances one second per call, giving
// deterministic yet strictly increasing timestamps.
func tickingClock() func() time.Time {
	now := time.Unix(block.GenesisTimestamp+1, 0)
	return func() time.Time {
		now = now.Add(time.Second)
		return now
	}
}

func newTestChain(t *testing.T) *Chain {
	t.Helper()
	return New(testConfig(), WithClock(tickingClock()))
}

// actor is a test account: a key pair with a shorthand for signing transfers.
// Since signatures are mandatory, every test sender needs a key.
type actor struct{ ledger.KeyPair }

func newActor(t *testing.T) actor {
	t.Helper()
	pair, err := ledger.GenerateKey()
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	return actor{pair}
}

func (a actor) pay(to string, amount, timestamp int64) ledger.Transaction {
	return a.Sign(ledger.NewTransfer("", to, amount, timestamp))
}

// mineFunded returns a chain where alice already holds 100 confirmed coins,
// along with alice and bob.
func mineFunded(t *testing.T) (*Chain, actor, actor) {
	t.Helper()
	c := newTestChain(t)
	alice, bob := newActor(t), newActor(t)

	if err := c.Faucet(alice.Address(), 100); err != nil {
		t.Fatalf("faucet: %v", err)
	}
	if _, err := c.Mine(context.Background()); err != nil {
		t.Fatalf("mining the faucet block: %v", err)
	}
	return c, alice, bob
}

// FR-2 / acceptance: a fresh chain holds exactly one block, at height 0, whose
// prev-hash is the fixed genesis value.
func TestNewChainStartsAtGenesis(t *testing.T) {
	c := newTestChain(t)
	blocks := c.Blocks()
	if len(blocks) != 1 {
		t.Fatalf("a fresh chain holds %d blocks, want exactly 1", len(blocks))
	}
	if blocks[0].Height != 0 {
		t.Errorf("first block is at height %d, want 0", blocks[0].Height)
	}
	if blocks[0].PrevHash != block.GenesisPrevHash {
		t.Errorf("genesis prev-hash = %q, want %q", blocks[0].PrevHash, block.GenesisPrevHash)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("a chain holding only genesis must be valid: %v", err)
	}
}

func TestMinePaysTheRewardAndExtendsTheChain(t *testing.T) {
	c := newTestChain(t)
	cfg := testConfig()

	result, err := c.Mine(context.Background())
	if err != nil {
		t.Fatalf("mining: %v", err)
	}
	if c.Height() != 1 {
		t.Errorf("height = %d after one mine, want 1", c.Height())
	}
	if got := c.Balance(cfg.MinerAddress); got != cfg.MiningReward {
		t.Errorf("miner balance = %d, want the reward %d", got, cfg.MiningReward)
	}
	if result.Block.Transactions[0].From != ledger.Coinbase {
		t.Error("the first transaction in a block must be the coinbase reward")
	}
	if err := c.Validate(); err != nil {
		t.Errorf("a freshly mined chain must validate: %v", err)
	}
}

// The end-to-end path a reviewer will walk: fund, transfer, mine, check.
func TestTransferIsOnlyConfirmedOnceMined(t *testing.T) {
	c, alice, bob := mineFunded(t)

	if err := c.AddTransaction(alice.pay(bob.Address(), 30, 1)); err != nil {
		t.Fatalf("queueing a transfer: %v", err)
	}
	if c.Balance(bob.Address()) != 0 {
		t.Error("a queued transfer must not move coins")
	}
	if len(c.Pending()) != 1 {
		t.Errorf("pending = %d, want 1", len(c.Pending()))
	}

	if _, err := c.Mine(context.Background()); err != nil {
		t.Fatalf("mining: %v", err)
	}
	if got := c.Balance(alice.Address()); got != 70 {
		t.Errorf("alice = %d, want 70", got)
	}
	if got := c.Balance(bob.Address()); got != 30 {
		t.Errorf("bob = %d, want 30", got)
	}
	if len(c.Pending()) != 0 {
		t.Error("mined transactions must leave the pending pool")
	}
	if err := c.Validate(); err != nil {
		t.Errorf("validate: %v", err)
	}
}

// FR-4 / acceptance: overspending is rejected and balances are unchanged.
func TestAddTransactionRejectsOverspending(t *testing.T) {
	c := mineFunded(t)

	if err := c.AddTransaction(ledger.NewTransfer("alice", "bob", 150, 1)); err == nil {
		t.Fatal("sending 150 from a balance of 100 must be rejected")
	}
	if got := c.Balance("alice"); got != 100 {
		t.Errorf("alice = %d after a rejected transaction, want 100", got)
	}
	if len(c.Pending()) != 0 {
		t.Error("a rejected transaction must not be queued")
	}
}

// Block size is honoured, and the reward occupies one of the slots.
func TestMineRespectsMaxTransactionsPerBlock(t *testing.T) {
	c := mineFunded(t)
	for i := 0; i < 6; i++ {
		if err := c.AddTransaction(ledger.NewTransfer("alice", "bob", 1, int64(i))); err != nil {
			t.Fatalf("queueing transfer %d: %v", i, err)
		}
	}

	result, err := c.Mine(context.Background())
	if err != nil {
		t.Fatalf("mining: %v", err)
	}
	if got := len(result.Block.Transactions); got != testConfig().MaxTxPerBlock {
		t.Errorf("block holds %d transactions, want the configured maximum %d",
			got, testConfig().MaxTxPerBlock)
	}
	if got := len(c.Pending()); got != 2 {
		t.Errorf("%d transactions left pending, want the 2 that did not fit", got)
	}
}

// Cancelling mid-search must leave the chain exactly as it was.
func TestCancelledMineLeavesTheChainUntouched(t *testing.T) {
	cfg := testConfig()
	cfg.Difficulty = 8 // effectively unreachable within the test
	c := New(cfg, WithClock(tickingClock()))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := c.Mine(ctx); err == nil {
		t.Fatal("a cancelled mine must return an error")
	}
	if c.Height() != 0 || len(c.Blocks()) != 1 {
		t.Error("a failed mine must not append a block")
	}
	if c.Balance(cfg.MinerAddress) != 0 {
		t.Error("a failed mine must not pay the reward")
	}
}

func TestLoadRebuildsBalancesAndRejectsACorruptChain(t *testing.T) {
	c := mineFunded(t)
	if err := c.AddTransaction(ledger.NewTransfer("alice", "bob", 10, 1)); err != nil {
		t.Fatal(err)
	}

	restored, err := Load(testConfig(), c.Blocks(), c.Pending())
	if err != nil {
		t.Fatalf("loading a valid chain: %v", err)
	}
	if got := restored.Balance("alice"); got != 100 {
		t.Errorf("restored alice = %d, want 100 replayed from the blocks", got)
	}
	if len(restored.Pending()) != 1 {
		t.Error("pending transactions must survive a reload")
	}

	tampered := c.Blocks()
	tampered[1].Transactions[0].Amount = 999_999
	if _, err := Load(testConfig(), tampered, nil); err == nil {
		t.Fatal("loading a tampered chain must fail rather than become the new truth")
	}
}
