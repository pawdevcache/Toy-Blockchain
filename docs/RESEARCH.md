# Research Report — Toy Blockchain and Ledger Simulator

Every number and every block of output below was produced by this
implementation on the machine described in *Setup*. Nothing is copied from a
textbook, and where a result surprised me I have said so rather than tidied it
away.

**Setup.** Intel Core i7-10750H (6 physical cores, 12 logical), Windows 11,
Go 1.23.1, `CGO_ENABLED=0`. Reproduce with:

```bash
go build -o bin/tbc ./cmd/tbc
./bin/tbc bench 6 8 1      # difficulty sweep, 8 runs each, single worker
./bin/tbc bench 6 4 12     # the same sweep across 12 goroutines
```

---

## 1. Tamper-evidence

### The experiment

Build an honest chain, confirm it validates, then edit a transaction inside an
early block by hand and validate again.

```bash
tbc faucet alice 100 && tbc mine        # block 1 mints 100 coins for alice
tbc send alice bob 30  && tbc mine      # block 2 moves 30 of them to bob
tbc validate
```

**Before** — the untouched chain:

```
alice                70
bob                  30
miner                100
total supply         200
VALID: 3 blocks, every hash, link and balance checks out
```

Then `data/chain.json` was opened in an editor and alice's faucet amount changed
from `100` to `999999`. Nothing else was touched.

**After**:

```
tbc: ...chain.json holds a chain that no longer validates: block 1
(0000aa1967ae...) failed the merkle root check: the transactions stored in this
block no longer produce its Merkle root, so at least one of them has been
altered
```

Exit code 1. `tbc balance` fails with the same error: loading refuses a chain
that does not validate, so the edited file cannot quietly become the new truth.

### Which check catches it, and why

The **Merkle root** check — not, as I first assumed, the hash check.

This is the most useful thing I learned building this. A block's hash is computed
over its *header*: height, timestamp, previous hash, **Merkle root**, difficulty
and nonce. The transaction list itself is not in the preimage. So when a
transaction is edited:

* the transactions no longer hash to the stored Merkle root — **caught here**;
* but the stored block hash still matches a recomputation, because the preimage
  it covers (including that now-stale root) has not changed at all.

Had validation only recomputed the block hash, the edit would have passed
undetected. Both checks are needed, and each closes the door the previous one
leaves open. `TestTamperingIsDetectedAtEveryLevelOfCoverUp` in
[`internal/chain/validate_test.go`](../internal/chain/validate_test.go) walks an
attacker through all four stages:

| Attacker's move | What now fails | Cost to them |
|---|---|---|
| Edit a transaction | block 1, `merkle root` | free |
| Also repair the Merkle root | block 1, `hash` | free |
| Also recompute the block hash | block 1, `difficulty` — the new hash no longer starts with the required zeros | free, but stuck |
| Also re-mine block 1 properly | block 2, `link` — it still stores block 1's *old* hash | one proof of work, and now one per following block |

The fourth row is the whole point. Each block's `prev_hash` welds it to the exact
bytes of its predecessor, so a single edit invalidates every block after it. The
forger's bill is not one proof of work, it is one per block from the edit to the
tip.

---

## 2. Difficulty versus effort

Difficulty *d* means the block hash must begin with *d* zero hex digits. A hash
digit is uniform over 16 values, so the probability of any single nonce
succeeding is 16⁻ᵈ and the expected number of attempts is **16ᵈ**.

### Single worker — 8 blocks mined per difficulty

| Difficulty | Expected hashes | Avg hashes measured | Measured / expected | Avg time | Hash rate |
|---:|---:|---:|---:|---:|---:|
| 1 | 16 | 14 | 0.90 | < 1 ms | — |
| 2 | 256 | 178 | 0.69 | < 1 ms | 0.9 MH/s |
| 3 | 4,096 | 3,433 | 0.84 | 4 ms | 0.8 MH/s |
| 4 | 65,536 | 89,255 | 1.36 | 98 ms | 0.9 MH/s |
| 5 | 1,048,576 | 1,556,987 | 1.48 | 1.448 s | 1.1 MH/s |
| 6 | 16,777,216 | 19,660,428 | 1.17 | 18.168 s | 1.1 MH/s |

*(Times below a millisecond read as zero: the Windows clock cannot resolve them.
The test suite asserts on hash counts rather than elapsed time for exactly this
reason.)*

### The trend

**It is exponential, not linear.** Each extra zero digit multiplies the work by
16, because it multiplies the number of hashes that must be rejected by 16.
Measured time per digit: 4 ms → 98 ms → 1.45 s → 18.2 s, i.e. ratios of 24×,
15× and 12.5×, scattered around the predicted 16×.

Two things this makes obvious:

1. **The hash rate is flat** at roughly 1 MH/s regardless of difficulty. The work
   per attempt never changes; only the number of attempts does. Difficulty does
   not make hashing slower, it makes success rarer.
2. **The scatter is huge and it is supposed to be.** Individual measurements land
   between 0.69× and 1.48× of prediction. Mining is a geometric process: the
   standard deviation of the number of attempts is almost exactly equal to its
   mean, so even an eight-run average stays visibly noisy. Averaging more runs
   narrows it, but slowly — the honest conclusion is that a table like this
   confirms the *order of magnitude*, not the constant.

An early two-run measurement reported 30 hashes at difficulty 2 against a
predicted 256 — a ratio of 0.12, which looked like a bug in the counter. It was
not; it was variance. Eight runs brought it back to 0.69, and that is itself the
lesson: with a geometric distribution, a two-sample average is not evidence of
anything.

### Concurrency — the same sweep across 12 goroutines

| Difficulty | Avg hashes (12 workers) | vs expected | Avg time | 1-worker time | Speed-up |
|---:|---:|---:|---:|---:|---:|
| 1 | 183 | 11.4× | 5 ms | < 1 ms | slower |
| 2 | 3,792 | 14.8× | 2 ms | < 1 ms | slower |
| 3 | 30,736 | 7.5× | 12 ms | 4 ms | 0.3× |
| 4 | 114,908 | 1.75× | 42 ms | 98 ms | 2.3× |
| 5 | 2,597,310 | 2.48× | 973 ms | 1.448 s | 1.5× |
| 6 | 16,097,570 | 0.96× | 6.312 s | 18.168 s | **2.9×** |

Two honest observations:

* **Below difficulty 4, parallelism costs more than it saves.** At difficulty 1
  the twelve workers computed 183 hashes for a job needing 16. The reason is in
  the code: a worker only checks whether someone else has won every 4,096 nonces
  (checking every iteration would cost more than hashing). At low difficulty
  *every* worker finds its own valid nonce long before it ever looks up — twelve
  workers × ~16 expected hashes ≈ 192, which is what was measured. The design
  trades a little wasted work at trivial difficulty for near-zero overhead where
  it matters; at difficulty 6 the overhead has vanished into the noise (0.96× of
  expected).
* **The speed-up is 2.9×, not 12×,** on 6 physical cores. Hyper-threading
  contributes well under a second core's worth of throughput on a workload that
  saturates the same execution units, and all-core turbo clocks lower than
  single-core turbo. A 2.9× gain from 6 cores is unremarkable for a pure-ALU
  workload, and inflating the worker count to 12 did not buy the extra.

---

## 3. Design write-up

### The hashing scheme

Block hash = SHA-256 over exactly this byte string, built in one place,
[`Block.hashPreimage`](../internal/block/block.go):

```
block|height|timestamp|prev_hash|merkle_root|difficulty|nonce
```

The choices behind it:

* **The stored `hash` field is excluded.** Including a block's hash in its own
  preimage is circular.
* **Transactions enter via the Merkle root**, not as raw text, so the preimage is
  a fixed ~150 bytes whether the block carries one transaction or a thousand,
  while any change to any transaction still changes the block hash.
* **`difficulty` is inside the preimage.** Each block therefore records the target
  it was mined against, and validating a chain years later does not depend on the
  node's current setting. Leaving it out would mean a chain mined at difficulty 4
  fails validation the day someone sets difficulty 5.
* **Field order is fixed here and nowhere else.** Two implementations agreeing on
  the fields but not the order would compute different hashes for the same block.
* **The `|` separator is unambiguous** because every field is a number or a hex
  digest — no value can contain the delimiter.

Transactions hash the same way, with each address **length-prefixed**:

```
tx|5:alice|3:bob|30|1723041000000000
```

Without the length prefix, `("ab", "c")` and `("a", "bc")` would produce the same
byte string and therefore the same transaction ID. With it they cannot. Addresses
are additionally restricted to `[A-Za-z0-9_-]`, so a crafted address cannot
imitate the encoding.

The Merkle root hashes transaction pairs upwards until one value remains, padding
an odd level by hashing the last node with itself (Bitcoin's rule; it is known to
allow a duplicate-transaction ambiguity, CVE-2012-2459, which a production
implementation must reject explicitly — this one does not).

### How validation guarantees integrity across the whole chain

[`chain.Validate`](../internal/chain/validate.go) walks from genesis and applies
seven rules per block, stopping at the first failure and naming the block:

1. **genesis** — block 0 is byte-for-byte the canonical genesis
2. **height** — each block sits exactly one above its predecessor
3. **timestamp** — time never runs backwards
4. **link** — `prev_hash` equals the previous block's stored hash
5. **merkle root** — the root still summarises the transactions stored
6. **hash** — the stored hash equals a fresh recomputation
7. **difficulty** — that hash meets the target the block itself claims

Then every transaction is replayed into a fresh `ledger.State`, which catches a
block that is flawlessly hashed but spends coins that never existed —
`TestValidateCatchesAFullyMinedButUnaffordableBlock` mines exactly such a block
and confirms it is rejected.

The guarantee is inductive. Genesis is fixed and known. If block *n−1* is
trustworthy and block *n* names its exact hash, then block *n* can only be
altered by changing its own hash, which breaks *n+1*'s link, and so on to the
tip. Each block's hash is a commitment to its entire ancestry, so verifying the
tip's hash verifies the whole history — and because balances are replayed from
the blocks rather than read from disk, no stored balance can contradict them.

---

## 4. Discussion

### Why the previous-hash link makes tampering impractical in a real chain

In this toy, rewriting history is trivial: `TestTamperingIsDetectedAtEveryLevelOf
CoverUp` shows an attacker re-mining a block in milliseconds, and at difficulty 4
they could re-mine an entire hundred-block chain in a couple of seconds. The link
makes tampering *detectable*, not expensive.

Three things convert "detectable" into "impractical" in a real chain, none of
which exist here:

1. **The work is enormous.** Bitcoin's difficulty is tuned so the entire network
   needs ten minutes per block; a single machine at 1 MH/s would need longer than
   the universe has existed. Editing block *n* means redoing that for block *n*
   and every block after it.
2. **The target keeps moving.** While the attacker re-mines the past, honest
   miners extend the present. Because peers follow the chain with the most
   accumulated work, the forger must not only catch up but overtake — which needs
   more than half the network's hash power, permanently, not just once.
3. **Everyone holds a copy.** Thousands of independent nodes validate the same
   rules. A rewritten history is not merely invalid, it is *visibly* different
   from what everyone else already has.

The links are the mechanism; distributed replication plus accumulated work are
what give that mechanism teeth. My implementation has the first and neither of
the others, which is exactly why "trivial locally, impractical globally" is the
right way to describe the difference.

### An alternative to proof of work

**Proof of stake** (Ethereum since 2022) replaces "who burned the most
electricity" with "who has the most capital locked up". Validators deposit coins,
are chosen to propose blocks in proportion to their deposit, and lose part of the
deposit if they sign conflicting blocks.

* *Advantage over PoW:* the security budget is capital rather than energy.
  Ethereum's switch cut its energy use by roughly 99.9%, and it makes attack cost
  explicit — misbehaviour is punished by slashing rather than merely wasted.
* *Drawback:* the ordering of validators is decided by capital that is itself
  recorded on the chain, so security becomes partly circular. A new node cannot
  determine the correct chain from physics alone, as it can with PoW, and needs a
  recent trusted checkpoint ("weak subjectivity"). Concentration of stake also
  compounds: rewards accrue to those already holding the most.

**Proof of authority** goes further: a fixed list of identified signers takes
turns. Fast, cheap and perfectly reasonable for a consortium chain where the
participants already know each other; useless if the point is that no one has to
be trusted.

### Three ways this toy differs from a production blockchain

1. **No consensus among peers.** One process, one chain, no gossip, no fork
   choice. The hardest problem in the field — agreeing on an ordering with no
   coordinator and no assumption of honesty — is entirely absent.
2. **No transaction signatures.** `send bob alice 50` succeeds no matter who
   types it: naming a sender is the same as authorising them. Every real chain
   requires a signature from the private key that owns the funds.
3. **No inclusion proofs or finality.** The Merkle root is computed and verified
   but nothing uses it to prove a single transaction belongs to a block, so there
   is no light-client story. Nor is there any notion of finality: no confirmation
   depth, no checkpoint, nothing that says a block will never be reversed.

### Sketch: adding signatures

The gap I would close first, because it is the one that makes the ledger a toy
rather than a ledger.

* **Keys.** `crypto/ed25519` from the standard library — 32-byte public keys,
  64-byte signatures, fast verification, no parameter choices to get wrong. A
  `tbc keygen` command writes a key file; the **address becomes the hex of the
  public key** (or better, of its SHA-256, which shortens it and hides the key
  until first spend).
* **Transaction shape.** Add `PubKey` and `Signature` fields. Signing covers the
  existing canonical bytes *excluding* the signature itself — a signature cannot
  commit to itself, the same circularity as the block hash.
* **Replay protection.** The timestamp currently makes each transaction unique,
  but a signed transaction can simply be resubmitted. It would be replaced by a
  per-account sequence number, with `State` tracking the next expected value and
  rejecting anything out of order.
* **Validation.** `Transaction.Validate` gains two checks — the public key hashes
  to `From`, and the signature verifies against the canonical bytes — so every
  path already calling it (mempool admission, block assembly, full-chain
  validation) is covered without new call sites. Coinbase transactions stay
  exempt, since they have no sender.
* **Cost.** Verification is not free: roughly 30 µs per signature, so a
  thousand-transaction block costs ~30 ms to validate. That is the argument for
  caching verification results per transaction ID, which is what real nodes do.

The work is perhaps half a day. What it buys is the difference between "the
ledger records who paid whom" and "the ledger proves who paid whom".

---

## 5. Honest limitations

Beyond the three differences above: the faucet mints coins on demand; the
coinbase reward is not validated, so a block could claim any reward it liked;
timestamps are only required not to move backwards; two concurrent `tbc mine`
processes would race on the data file; and the Merkle implementation inherits
Bitcoin's duplicate-transaction ambiguity without guarding against it. Each is
listed in the README so a reader does not have to discover them by reading the
source.

---

## Sources

1. S. Nakamoto, *Bitcoin: A Peer-to-Peer Electronic Cash System*, 2008 —
   sections 4 (Proof-of-Work) and 7 (Reclaiming Disk Space, i.e. Merkle trees).
   <https://bitcoin.org/bitcoin.pdf>
2. Bitcoin developer documentation, *Block Chain* and *Merkle Trees*.
   <https://developer.bitcoin.org/reference/block_chain.html>
3. CVE-2012-2459 — duplicate-transaction Merkle root ambiguity in Bitcoin Core.
4. Ethereum Foundation, *Proof-of-stake (PoS)* and *The Merge* energy figures.
   <https://ethereum.org/en/developers/docs/consensus-mechanisms/pos/>
5. Go standard library documentation for `crypto/sha256`, `crypto/ed25519`,
   `sync/atomic` and `context`. <https://pkg.go.dev/std>
6. V. Buterin, *Proof of Stake: How I Learned to Love Weak Subjectivity*, 2014 —
   the checkpoint requirement described above.
