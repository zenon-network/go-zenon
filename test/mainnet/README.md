# Mainnet comparison tools for the go-zenon test framework

Two tools for [test level L6](../../docs/TESTING.md) (mainnet history): testing a
`dev` binary against a v0.0.10 reference node on mainnet. They are
standard-library Python 3, read-only, and live in their own package
(`test/mainnet/`) so they never mix with the devnet driver (which signs and
publishes transactions — do not run `test/e2e` or `test/devnet` against a
mainnet host). Both tools:

- refuse to run unless every endpoint reports chain identifier 1 and the
  mainnet momentum-1 hash `9e204601…8eeb0` (spec: 0x3639/go-zenon#17),
- emit one JSON line per comparison/finding (JSONL), a final summary by case
  id, and the go-zenon commit the tool runs from (`-dirty` appended unless
  the tree is clean, so a run is attributable to the code that produced it),
- retry transport-level failures (a node under sync can drop connections).

## `compare.py` — M-2: mainnet RPC state differ

```sh
python3 test/mainnet/compare.py \
    --ref http://127.0.0.1:35997 \          # v0.0.10 reference node
    --test http://127.0.0.1:36997 \         # dev binary under test
    --sample-height 14700000 \               # S of the #17 sampling rule
    --addr-count 1000 --batch-size 50 \     # M-2c sample size / batching
    --out results.jsonl                      # sample manifest: results.jsonl.sample.jsonl
```

- **M-2a**: full `ledger.getDetailedMomentumsByHeight` answers (momentum
  hash, changes hash, every account block) at each spork activation height
  and the one before it, 4,188,198-4,188,207, 14,567,886-14,567,888, and at
  the frontier (the frontier answer runs only when the M-2b frontier hashes
  are identical). Heights the test node has not synced to are `INFO`.
- **M-2b**: frontier momentum hash; same height with different hash is a
  `FAIL` (split chain).
- **M-2c**: frontier state. The 1,000-address sample is drawn by the rule
  of 0x3639/go-zenon#17: seed = the momentum hash at height `S` (the first
  height divisible by 10,000 after the test node reports sync state 2,
  cross-checked against the reference node); for `i` = 0, 1, 2, ...,
  `sha3-256(seed || i as 8 big-endian bytes)`, the first 8 bytes of the
  digest as a big-endian integer `n` name the momentum at `1 + (n mod S)`;
  the address of each account block in those momentums is collected until
  exactly `--addr-count` distinct ones (the momentum that reaches the count
  is not read further). The manifest — address, the height it was found at,
  and the account's height and frontier block hash at sampling time — is
  written to `--sample-out`, one JSON line per address, with its sha256 in
  the log, so the sample can be checked later from any node. Account infos
  are read in batches of `--batch-size`; a batch counts only if the frontier
  hashes of both nodes are identical before and after the batch. Then the
  getters: all 60 `embedded.*` read methods listed in
  `test/inventory/coverage.tsv` (including the three `TokenAPI` getters),
  with lazily resolved arguments (pillar name, accelerator project/phase,
  bridge network/wrap/unwrap ids, ZNN zts, ...) and paging to the end
  (pageSize 1000).
- **M-2d** controls: self-self at height 100 (must match) and a planted
  height difference 100 vs 101 (must be detected).

Comparison semantics: node-local contextual fields (`numConfirmations`,
`token.totalSupply`/`maxSupply`, recursively incl. `pairedAccountBlock`) are
normalized before comparison. Pillar answers additionally have the
consensus-cache fields `weight`/`rank`/`currentStats` normalized — they come
from a node-local cache refreshed asynchronously (at most every 5 min), so
two nodes on the same frontier legitimately report different values; the
strip is guarded by the `producerAddress` discriminator so a same-named
field elsewhere is never silently dropped. RPC-level errors are comparable
answers:
identical errors on both sides are a PASS; a method missing on exactly one
build (a getter added after the reference release) is `INFO`
(`method-missing-on-one-side`); an error on exactly one side, a different
error surface, and blocks present on exactly one side
(`block-availability-mismatch`) are FAILs — the #17 reference node is full
history and serves every height. A batch/getter taken while the frontier
moved is INFO, never FAIL.

Left out of the getter list (spec: listed): `stats.*` (node-local),
`ledger.getUnconfirmedBlocksByAddress` (unconfirmed pool), all write methods
and subscriptions.

**Validation** (self-self against one v0.0.10 node, 2026-11-22, S = 14,700,000,
clean tree): exit 0 — M-2a 15 PASS + the frontier entry, M-2b PASS, sample of
exactly 1,000 addresses drawn per the rule above (manifest sha256 in the log;
independently re-simulated height-for-height outside the tool: exact match),
M-2c-accountInfos batches PASS, getters PASS, controls 2 PASS. An address
that never transacted records `accountHeight` 0 and a null frontier hash.

## `scan.py` — M-3: mainnet history scanner

M-3 reads the v0.0.10 reference node (the full-history node of #17).

```sh
# hash-link walk over momentum headers 1..H (pages of 1000, verifies each
# page is height-contiguous and that page boundaries chain by previousHash;
# the walk is capped at --to and FAILs unless it reaches exactly --to)
python3 test/mainnet/scan.py --node http://127.0.0.1:35997 \
    --headers --to 14661812 --out scan.jsonl

# full account-block scan over a stated range (10 momentums per call):
# counts momentums and account blocks read, reports every
# ChangeTssECDSAPubKey send to the embedded bridge contract (selector
# 15a0c641 = first 4 bytes of the SHA3-256 of
# "ChangeTssECDSAPubKey(string,string,string)", same computation as vm/abi
# Method.Id), ABI-decodes the new key and checks it lies on secp256k1, and
# reports every block whose Difficulty read as unsigned 64-bit is >= 2^63
python3 test/mainnet/scan.py --node http://127.0.0.1:35997 \
    --blocks --from 1 --to 14661812 --out scan.jsonl
```

Momentum 1 is excluded from `--blocks`: `getDetailedMomentumsByHeight` does
not answer at height 1 on full-history nodes (momentum 1 reads fine via
`getMomentumByHash`; the exclusion is logged, and the `from` field records
the real range). As for headers, the scan FAILs unless it reaches exactly
`--to`; a page shorter than requested before `--to` is a short read, not a
pass.

**Validation**: header walk 1..200,000 on a dev node — 200,000 momentums,
0 link breaks; header walk `--to 1500` on the v0.0.10 node — exactly 1,500
momentums, exit 0; block scan 1..200 on the v0.0.10 node — momentum-1
exclusion logged, 199 momentums / 26 account blocks, exit 0; block scan over
a known ChangeTssECDSAPubKey send — the send is found at the recorded
height/block hash, its key decodes and is on-curve, exit 0.

**Limits**: a block-level scan of the full chain is ~1.5M RPC calls; run it
in slices. `ledger.getDetailedMomentumsByHeight(1, 10)` does not answer on
full-history nodes; start block scans at 1 (the tool skips momentum 1
itself) and header walks wherever the question starts.
