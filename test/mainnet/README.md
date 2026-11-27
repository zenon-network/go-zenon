# Mainnet comparison tools for the go-zenon test framework

Two tools for test level L6 (mainnet history): testing a `dev` binary
against a v0.0.10 reference node on mainnet. L6 and the M-2/M-3 cases are
defined in [docs/TEST-PLAN.md](../../docs/TEST-PLAN.md) (section 7);
[docs/TESTING.md](../../docs/TESTING.md) holds the procedures. The tools
are standard-library Python 3, read-only, and live in their own package
(`test/mainnet/`) so they never mix with the devnet driver (which signs and
publishes transactions — do not run `test/e2e` or `test/devnet` against a
mainnet host). Both tools:

- refuse to run unless every endpoint reports chain identifier 1 and the
  mainnet momentum-1 hash `9e204601…8eeb0` (spec: 0x3639/go-zenon#17),
- emit one JSON line per comparison/finding (JSONL), a final summary by case
  id, and the go-zenon commit the tool runs from (`-dirty` appended unless
  the tree is clean, so a run is attributable to the code that produced it),
- retry transport-level failures (a node under sync can drop connections),
- write to a unique `tempfile.mkdtemp` directory when `--out` is not given
  (removed after the results are echoed to stdout), so concurrent slices
  cannot clobber each other.

## `compare.py` — M-2: mainnet RPC state differ

```sh
# --sample-height is S of the #17 sampling rule: the first height divisible
# by 10,000 after the test node reported sync state 2 (a node that finished
# syncing at height 14,691,234 uses S = 14,700,000); the momentum at S must
# exist on both nodes
python3 test/mainnet/compare.py \
    --ref http://127.0.0.1:35997 \
    --test http://127.0.0.1:36997 \
    --sample-height 14700000 \
    --addr-count 1000 --batch-size 50 \
    --out results.jsonl
```

(`--ref` is the v0.0.10 reference node, `--test` the dev binary under test;
the sample manifest goes to `results.jsonl.sample.jsonl`.)

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
  (pageSize 1000). Paged getters keep the top-level fields next to `list`
  (`count`, `qsrAmount`, `totalAmount`, `totalWeightedAmount`, ...) from the
  first page and compare them too — a disagreement there is a `FAIL` even
  when the merged lists match.
- **M-2d** controls: self-self at height 100 (must match) and a planted
  height difference 100 vs 101 (must be detected).

### Comparison semantics

Normalization: node-local contextual fields (`numConfirmations`,
`token.totalSupply`/`maxSupply`, recursively incl. `pairedAccountBlock`) are
normalized before comparison.

Pillar answers: `embedded.pillar.getAll` sorts pillars by descending
`weight`, and `weight`/`rank`/`currentStats` come from a node-local
consensus cache refreshed asynchronously at most every 5 minutes
(`rpc/api/embedded/pillar.go`: `GetAll` ranks by weight before cutting the
page; while the cache is cold the weights are zero placeholders and the
order degrades to ascending name). Two nodes on the same frontier therefore
legitimately report the same pillars in different orders with different
cache values. `embedded.pillar.getByOwner` answers with the same PillarInfo
list and the same node-local ordering. These lists are sorted by
`(name, ownerAddress)` before comparison and the cache-derived fields
`weight`/`rank`/`currentStats` are excluded from equality; the strip is
guarded by the `producerAddress` discriminator so a same-named field
elsewhere is never silently dropped. Every other pillar field is compared.

Classification of one getter pair (see `classify_getter`):

- **Transport failures are `ERROR`, never PASS/FAIL.** Identical transport
  failures on both sides ("timed out" twice) are not agreement — neither
  getter returned an answer — and a one-sided transport failure is not a
  chain difference either; both record `ERROR`.
- **Method-missing is classified before the one-sided-error rule** (-32601,
  "the method X does not exist/is not available"). Missing on the
  reference but answered by the test node is `INFO`
  (`method-missing-on-ref`: a getter added on dev after the reference
  release, e.g. `embedded.plasma.getVariables`). Missing on the test node
  but answered by the reference is a `FAIL` (`method-missing-on-test`): a
  getter the reference release serves but the binary under test dropped is
  a removed RPC surface, i.e. a regression that must gate the run, not a
  note. Missing on
  both is `INFO` (`method-missing-on-both`): consistent, documented as INFO
  rather than PASS so the missing surface stays visible.
- Identical RPC-level errors on both sides are a `PASS` (e.g. a not-found
  id); different errors are a `FAIL` (`error-surface-differs`); an RPC
  error on exactly one side is a `FAIL` (`error-on-one-side`) — except a
  reference-side error listed in `EXPECTED_ERROR_SURFACE` whose message
  matches: an intended behavior change on dev (currently the two bridge
  unwrap-request lists, where v0.0.10 fails the whole call on a stored
  request whose network was removed and dev filters it), logged as `INFO`
  (`expected-error-surface-change`) with the reason. Only that direction
  is allowlisted; the binary under test erroring where the reference
  answers stays a `FAIL`.
- Results are compared after normalization. A diff consisting exactly of
  the known, intended API additions in `EXPECTED_API_ADDITIONS` (top of
  `compare.py`, one comment per entry — currently `requiredFusedPlasma` on
  `embedded.plasma.getRequiredPoWForAccountBlock`, added on dev with
  Dynamic Plasma) is `INFO` (`expected-api-addition`, logged with the
  reason), so exit status 0 is a real gate for dev-vs-v0.0.10 runs. The
  same applies to additions inside list items via
  `EXPECTED_ITEM_ADDITIONS` (currently `isRevocable` on
  `embedded.plasma.getEntriesByAddress` entries, added on dev with
  Dynamic Plasma). Any
  other diff is a `FAIL` with the first differing key/index in the record.
- A getter whose argument cannot be built because the reference chain has
  no such entity (e.g. no accelerator project with a phase — `getAll` is
  sorted by descending `lastUpdateTimestamp`, so a fresh submission with
  `phases: []` can be the newest project; the tool scans for the first
  project that has one) is `SKIP` with the reason, not `ERROR`.

Address-keyed getters are exercised against populated addresses resolved
from list getters on the reference node — a pillar owner and a sentinel
owner, a staker / liquidity staker / fuser / delegator / token owner probed
from those candidates — so the comparison does not degenerate to comparing
trivially-empty answers. When the chain has no such entity the sampled
address is used and the fallback is recorded; the resolved address book
(slot, address, source) is logged as an `address-book` record.

Frontier guards: an account-info batch or a getter result comparison counts
only when the frontier hashes of both nodes are identical before and after
it. If the frontier moved, the comparison is retried up to 3 times before
degrading to `INFO` (`frontier-moved-during-batch` / `-getter`). The pool
re-read of a differing account-info batch is wrapped in the same check, so
a frontier advance during the re-read retries the batch instead of
turning pool noise into a false `FAIL`. Only result-bearing getter
comparisons count toward the guard: if zero batches, or zero result-bearing
getters, were actually compared (frontier kept moving, or only
error/missing-method outcomes), the run logs an `ERROR` record and exits
non-zero — an all-INFO run cannot pass as exit 0 without having compared
anything.

One known false-FAIL source is handled explicitly:
`ledger.getAccountInfoByAddress` reads the frontier account store, which
includes node-local unconfirmed pool blocks, so two nodes with identical
frontier hashes can intermittently disagree. A differing address is
re-fetched on both sides once before a FAIL is declared; a difference that
resolves on the re-read is a `PASS` with `poolRetryResolved` recorded.

Left out of the getter list (spec: listed): `stats.*` (node-local),
`ledger.getUnconfirmedBlocksByAddress` (unconfirmed pool), all write methods
and subscriptions.

**Validation** (self-self against one v0.0.10 node, 2026-11-22, S = 14,700,000,
clean tree): exit 0 — M-2a 15 PASS + the frontier entry, M-2b PASS, sample of
exactly 1,000 addresses drawn per the rule above (manifest sha256 in the log;
independently re-simulated height-for-height outside the tool: exact match),
M-2c-accountInfos batches PASS, getters PASS, controls 2 PASS. An address
that never transacted records `accountHeight` 0 and a null frontier hash.
Classification logic is unit-tested with canned answers in
`test/mainnet/test_compare.py` (stdlib unittest; run
`python3 test/mainnet/test_compare.py -v`).

## `scan.py` — M-3: mainnet history scanner

M-3 reads the v0.0.10 reference node (the full-history node of #17).

```sh
# hash-link walk over momentum headers 1..H (pages of 1000, verifies every
# entry is exactly the expected height and that page boundaries chain by
# previousHash; the walk is capped at --to and FAILs unless it reaches
# exactly --to)
python3 test/mainnet/scan.py --node http://127.0.0.1:35997 \
    --headers --to 14661812 --out scan.jsonl

# full account-block scan over a stated range (10 momentums per call):
# counts momentums and account blocks read, reports every
# ChangeTssECDSAPubKey send to the embedded bridge contract (selector
# 15a0c641 = first 4 bytes of the SHA3-256 of
# "ChangeTssECDSAPubKey(string,string,string)", same computation as vm/abi
# Method.Id), ABI-decodes the new key and checks it lies on secp256k1 and
# whether its decompressed X or Y coordinate has a leading zero byte
# (< 2^248), checks the live stored decompressedTssECDSAPubKey via
# embedded.bridge.getBridgeInfo (must be 65 bytes, 0x04 prefix, on-curve
# when non-empty), reports every account block whose Difficulty read as
# unsigned 64-bit is >= 2^63, and records visibility counts for blocks
# above dp.MaxDifficultyForAccountBlock (315,000,000,000,000) and
# constants.MaxDifficultyForAccountBlock (141,750,000)
python3 test/mainnet/scan.py --node http://127.0.0.1:35997 \
    --blocks --from 1 --to 14661812 --out scan.jsonl
```

Range validation: `--from` must be >= 1 and <= `--to`, and an explicit
`--to` above the node's frontier is rejected with an error (exit non-zero)
instead of being silently clamped — a scan that cannot reach the stated
height must not pass below it. An inverted range (`--from 51 --to 50`)
would read 0 momentums and pass; it is rejected the same way.

Momentum 1 is excluded from `--blocks`: `getDetailedMomentumsByHeight` does
not answer at height 1 on full-history nodes (momentum 1 reads fine via
`getMomentumByHash`; the exclusion is logged, and the `from` field of the
`M-3-blocks` record is the effective start, 2 in that case). As for
headers, the scan FAILs unless it reaches exactly `--to`; a page shorter
than requested before `--to` is a short read, not a pass — except when the
node caps the answer at its 10 MB byte budget and returns fewer momentums
with `"more": true` (dev), in which case the scan continues after the last
returned momentum. Every returned momentum must be exactly the expected
height: a page of heights [2..9, 11] for a 2..11 request FAILs even though
its last height reaches the request.

Outcome policy (one policy for both #78 checks, per `docs/TEST-PLAN.md`
M-3 "none found, or each one listed with the height where `dev` and
v0.0.10 would disagree"): **integrity problems FAIL the scan** — short
reads, wrong heights in a page, a difficulty or block-data field that does
not parse, a bridge key that does not base64/ABI-decode, or a malformed
`getBridgeInfo` payload. **Findings are listed and counted but do not by
themselves fail it** — an off-curve, unrecognized, or leading-zero-coordinate
bridge key, an invalid/short live `decompressedTssECDSAPubKey`, and a
difficulty >= 2^63 are logged one by one (`M-3-bridgeKey` with `keyCheck`,
`shortCoord`, `leadingZeroCoord`; `M-3-liveBridgeKey` with `keyCheck`,
`byteLength`, `finding`; `M-3-highDifficulty`) and counted in the
`M-3-blocks` record (`offCurveKeys`, `unexpectedKeyFormats`,
`shortCoordKeys`, `liveBridgeKeyCheck`, `liveBridgeKeyFindings`,
`highDifficultyItems`, `findings`) and in the summary record (`findings`),
so a run with findings is PASS-with-findings-listed, never silently clean.
Blocks exceeding `dp.MaxDifficultyForAccountBlock` (`M-3-aboveDpMaxDifficulty`,
`aboveDpMaxDifficulty`) and `constants.MaxDifficultyForAccountBlock`
(`aboveLegacyMaxDifficulty`, `aboveLegacyMaxDifficultySample`, `maxDifficulty`)
are also reported in `M-3-blocks` for visibility ahead of `DynamicPlasmaSpork`
activation and are not counted as #78 findings.

**Validation**: header walk 1..200,000 on a dev node — 200,000 momentums,
0 link breaks; header walk `--to 1500` on the v0.0.10 node — exactly 1,500
momentums, exit 0; block scan 1..200 on the v0.0.10 node — momentum-1
exclusion logged, 199 momentums / 26 account blocks, exit 0; block scan over
a known ChangeTssECDSAPubKey send — the send is found at the recorded
height/block hash, its key decodes and is on-curve, exit 0. Curve
decompression, leading-zero coordinate detection, live bridge key checks, and
difficulty threshold counters are unit-tested with canned answers in
`test/mainnet/test_scan.py` (stdlib unittest; run
`python3 -m unittest discover -s test/mainnet -v`).

**Limits**: a block-level scan of the full chain is ~1.5M RPC calls; run it
in slices. `ledger.getDetailedMomentumsByHeight(1, 10)` does not answer on
full-history nodes; start block scans at 1 (the tool skips momentum 1
itself) and header walks wherever the question starts.
