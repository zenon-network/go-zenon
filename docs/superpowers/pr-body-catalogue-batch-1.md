# Bug-catalogue batch 1: verified fixes, one commit per bug

This branch fixes the confirmed-safe entries from the 62-entry bug
catalogue assembled during the godoc review
(`docs/superpowers/bugs-found-2026-06-11.md`). Every entry was first
re-verified against dev @ 70ad9ce by re-reading the current
implementation; the verdicts live in `docs/superpowers/triage-2026-07-03.md`
(first commit). Each fix is its own commit with the analysis in the
commit message.

## What is fixed here (42 fix commits)

**Chain/store integrity**
- #26 `chain/momentum`: momentum insertion no longer swallows account-block
  apply errors (silent local-state corruption; a pillar could sign and
  broadcast a momentum built on corrupt state).
- #23 `common/db`: `ldbManager.Add` corrupted the store when a transaction
  didn't build on the frontier (the guard compared against the historical
  view's own frontier — a tautology). Now rejected, matching memdb.
- #27, #29, #31, #33: propagated storage errors in pillar-delegation
  computation, non-vacuous genesis balance checks, mailbox `atMost==0`
  underflow, genesis validation panics no longer reported as success.

**Sync**
- #60 `protocol/downloader`: `findAncestor`'s inverted guard meant the
  binary-search fork fallback never ran when needed (fork → resync from
  genesis) and always ran when not needed (wasted round-trips per sync).
  One-character fix restoring the go-ethereum-original semantics, now
  unit-tested for both fork shapes. **Recommend a live devnet/testnet sync
  verification before release.**

**Wallet** (first tests in `wallet/`)
- #54 derivation path returned empty; #55 `MaxSearchIndex` config had no
  effect; #56+#61 `Lock` left a nil entry (IsUnlocked stayed true, Stop
  panicked); #58 `Zero` didn't actually wipe secret bytes.

**VM / embedded (node-local only)**
- #34 auto-receive verified a possibly-nil block before the error check
  (pillar panic path); #35 unit-mismatched plasma cap (provably dead
  branch); #36 dead double-sign; #37 garbled ABI errors; #38, #40 (storage
  prefixes pinned 12/13 with assertion test), #41, #42, #47 (`common.Big0`
  aliasing), #48 (latent nil-deref in reward computation), #50 (log only).

**Consensus (behavior-neutral)**
- #51 wrong hash in diagnostic; #52 panic → returned error; #53 Go field
  rename (wire tag `exceptedBlockNum` deliberately preserved).

**RPC**
- #1/#2 `UnmarshalJSON` panics on fresh receivers (client-side crashes);
  #3/#4/#5 swallowed frontier errors; #6 IPv6 peer addresses; #7 sentinel
  `(nil, nil)` and `null` page entries; #8/#15 missing `RpcMaxPageSize`
  caps; #9/#10 stats logging; #11 unwrap-by-address stable sort (completes
  PR #57); #12/#13 uniform wrap/unwrap error policy; #14 checksummed
  address filters; #16/#17 dead code; #18/#19 subscribe lifecycle (dead
  channel; post-Stop subscribe no longer blocks forever).

## Wire-visible changes (deliberate, called out)

- #12/#13: storage errors in bridge listings now fail the call instead of
  silently shrinking pages; unwraps whose token pair was removed are
  skipped instead of permanently erroring the endpoint.
- #8/#15: `pageSize > 1024` now returns `ErrPageSizeParamTooBig`
  (matching every other paged endpoint).
- #14: checksummed EVM addresses now match (previously empty result).
- #10 stays wire-compatible (logs instead of erroring); #53 keeps the
  misspelled JSON tag for compatibility.

## Explicitly NOT fixed (consensus-sensitive quarantine)

#24, #39, #43, #44, #45, #49, #50 (state machine), plus sub-items of
#32/#42/#48/#53 — each changes send/block acceptance, contract state
evolution, or historical replay, and needs spork gating / upstream
coordination. Evidence and repro sketches are in the triage document.
Two catalogue entries were invalidated during verification (#28, #46 —
void Save methods panic internally; nothing is discarded).

## Verification

- `GOWORK=off go test ./...` passes at every wave boundary and at HEAD.
- TDD where testable: new tests in `chain/momentum`, `common/db`,
  `chain/genesis`, `chain/account/mailbox`, `common` (ticker), `vm/abi`,
  `vm/embedded/definition`, `wallet` (2 files), `zenon/mock`,
  `protocol/downloader`, `rpc/api`, `rpc/api/embedded`, plus assertions
  added to existing embedded suites. Each fix commit's red/green status is
  recorded in its message.
- Golden-state suites (accelerator hashes, consensus reward logs, bridge
  fixtures) pass unchanged wherever behavior was meant to be preserved.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
