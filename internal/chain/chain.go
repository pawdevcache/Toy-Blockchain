// Package chain ties everything together: it owns the ordered list of blocks,
// the pending pool, and the balances those blocks imply.
//
// It is the only package that knows how a block is built and appended, which
// keeps the rules in one readable place:
//
//	block  - what a block is and how it hashes
//	ledger - what a transaction is and who owns what
//	miner  - how the nonce is found
//	chain  - when a block may be added, and whether the history is intact
package chain

import (
	"context"
	"fmt"
	"time"

	"toychain/internal/block"
	"toychain/internal/config"
	"toychain/internal/ledger"
	"toychain/internal/miner"
)

// Chain is an append-only sequence of blocks plus the state derived from it.
// Blocks are never edited or removed; the only write operation is Mine.
type Chain struct {
	blocks []block.Block
	state  *ledger.State
	pool   *ledger.Mempool
	cfg    config.Config
	now    func() time.Time // injectable so tests are deterministic
}

// Option customises a Chain at construction time.
type Option func(*Chain)

// WithClock replaces the wall clock, used by tests to produce fixed timestamps.
func WithClock(now func() time.Time) Option {
	return func(c *Chain) { c.now = now }
}

// New starts a fresh chain containing only the genesis block.
func New(cfg config.Config, opts ...Option) *Chain {
	c := &Chain{
		blocks: []block.Block{block.Genesis()},
		state:  ledger.NewState(),
		pool:   ledger.NewMempool(),
		cfg:    cfg,
		now:    time.Now,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Load rebuilds a chain from persisted data. It refuses to load a chain that
// does not validate, so a corrupted file surfaces at startup rather than
// silently becoming the new truth.
//
// Balances are recomputed by replaying every block from genesis: they are never
// read from disk, so they cannot be tampered with independently of the blocks.
// Pending transactions that no longer fit the restored state are dropped, since
// they can never be mined.
func Load(cfg config.Config, blocks []block.Block, pending []ledger.Transaction, opts ...Option) (*Chain, error) {
	if err := Validate(blocks); err != nil {
		return nil, err
	}
	c := New(cfg, opts...)
	c.blocks = blocks
	for _, b := range blocks {
		if err := c.state.ApplyAll(b.Transactions); err != nil {
			return nil, fmt.Errorf("replaying block %d: %w", b.Height, err)
		}
	}
	for _, tx := range pending {
		_ = c.pool.Add(tx, c.state) // silently drop what is no longer affordable
	}
	return c, nil
}

// Blocks returns a deep copy of the chain, newest last, so a caller cannot
// reach into a stored block and edit a transaction it already committed to.
func (c *Chain) Blocks() []block.Block {
	out := make([]block.Block, len(c.blocks))
	for i, b := range c.blocks {
		b.Transactions = append([]ledger.Transaction(nil), b.Transactions...)
		out[i] = b
	}
	return out
}

// Tip is the most recent block; there is always at least genesis.
func (c *Chain) Tip() block.Block { return c.blocks[len(c.blocks)-1] }

// Height is the height of the tip: 0 for a chain holding only genesis.
func (c *Chain) Height() uint64 { return c.Tip().Height }

// Pending returns the transactions waiting to be mined.
func (c *Chain) Pending() []ledger.Transaction { return c.pool.Pending() }

// Balance is the confirmed balance of addr, ignoring anything still pending.
func (c *Chain) Balance(addr string) int64 { return c.state.Balance(addr) }

// Accounts lists every account with confirmed coins, sorted by address.
func (c *Chain) Accounts() []ledger.Account { return c.state.Accounts() }

// AddTransaction queues a transfer after checking it is well formed and
// affordable. Queuing moves no coins: that only happens when a block is mined.
func (c *Chain) AddTransaction(tx ledger.Transaction) error {
	return c.pool.Add(tx, c.state)
}

// Faucet queues newly minted coins for an account, so a fresh chain has money to
// move. A real chain has no such thing; it exists purely to make the demo
// runnable and is called out as a limitation in the README.
func (c *Chain) Faucet(to string, amount int64) error {
	return c.pool.Add(ledger.NewCoinbase(to, amount, c.now().UnixNano()), c.state)
}

// Mine packs pending transactions into a block, finds a nonce that satisfies the
// configured difficulty, and appends the block.
//
// Ordering is deliberate: the block is only appended, the balances only updated
// and the pool only drained *after* mining succeeds. If ctx is cancelled
// mid-search, the chain is left exactly as it was.
func (c *Chain) Mine(ctx context.Context) (miner.Result, error) {
	body, fromPool := c.buildBody()

	// Reject a block that could not be applied before paying to mine it.
	if err := c.state.Clone().ApplyAll(body); err != nil {
		return miner.Result{}, fmt.Errorf("block rejected before mining: %w", err)
	}

	candidate := block.New(c.Height()+1, c.Tip().Hash, body, c.cfg.Difficulty, c.now().Unix())
	result, err := miner.Mine(ctx, candidate)
	if err != nil {
		return miner.Result{}, err
	}

	if err := c.state.ApplyAll(result.Block.Transactions); err != nil {
		return miner.Result{}, fmt.Errorf("applying mined block: %w", err) // unreachable
	}
	c.blocks = append(c.blocks, result.Block)
	c.pool.Remove(fromPool)
	return result, nil
}

// Validate re-checks the whole chain from genesis. See validate.go.
func (c *Chain) Validate() error { return Validate(c.blocks) }

// buildBody assembles the transactions for the next block: the miner's reward
// first (the convention in real chains), then as many pending transactions as
// the configured block size allows. fromPool is how many came off the pool, so
// Mine knows exactly how much to drain once the block is stored.
func (c *Chain) buildBody() (body []ledger.Transaction, fromPool int) {
	room := c.cfg.MaxTxPerBlock
	if c.cfg.MiningReward > 0 {
		body = append(body, ledger.NewCoinbase(c.cfg.MinerAddress, c.cfg.MiningReward, c.now().UnixNano()))
		room-- // the reward occupies one slot
	}
	queued := c.pool.Peek(room)
	return append(body, queued...), len(queued)
}
