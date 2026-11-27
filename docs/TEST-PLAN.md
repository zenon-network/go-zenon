# go-zenon `dev` branch test plan

Status: draft for review, 2026-11-18. Written against `dev` at `924e6b8` (merge of #309).

How to run each procedure, and how gaps are tracked: [TESTING.md](TESTING.md).

## 1. What is being tested

`dev` is 159 merged pull requests ahead of the v0.0.9 line and now contains everything in `master` (v0.0.10, merged by #309). Against `master` the difference is 548 files, about 98,000 added lines. Six pull requests were open against `dev` when this was written; section 9 has their state on 2026-11-25.

The plan has two goals:

1. Show that every change added on `dev` does what its pull request says, on a running network and not only in a unit test.
2. Show that every base function of go-zenon still works: the twelve embedded contracts, the ledger, sync, momentum production, the RPC API, the wallet and the node itself.

One question sits above both: **does a `dev` binary agree with mainnet history and with v0.0.10 nodes until a spork is activated?** Unit tests cannot answer it. Section 7 covers it and it gates any release.

### What `dev` adds

| Area | Main additions | Spork |
|---|---|---|
| Transport | libp2p backend, hot swap from legacy devp2p, `Net.P2PBackend` override (#59, #74, #121 and 14 follow-ups) | `Libp2pSpork` (placeholder id) |
| Fees | Dynamic plasma: fusion and work prices, momentum version 2, plasma-based momentum cap, priced account pool (#63, #74, #168, #174, #179, #183, #223) | `DynamicPlasmaSpork` (placeholder id) |
| Contracts | PTLC contract (#138, #216); embedded refund (#277); per-epoch reward terms (#288); bounded reward loops, accelerator final tally, guardian and token-tuple bounds (#210, #233, #234, #237) | `PtlcSpork`, `EmbeddedRefundSpork`, `EpochRewardTermsSpork`, `DynamicPlasmaSpork` |
| Sync | Side-chain rejection memory and peer quarantine, production readiness gate, request budgets, downloader deadlines (#83, #175, #220, #226, #244, #245, #272, #275, #287, #304, #305) | none |
| RPC | Limits on batches, subscriptions, connections and response sizes; paging fixes; local-only default (#85, #88, #89, #123, #132, #276, #281, #283, #285, #303) | none |
| Node | Config selection, flag handling, shutdown, libznn entry point, key-file handling, genesis checks, database bound to the genesis spork address (#87, #187, #212, #256, #258, #284, #286) | none |
| Performance | Account pool and chain cache rewrite (#43, #44, #64, #67, #69) | none |

Four of the eight spork ids are placeholders (`…01`, `…02`, `…04`, `…07`). Nothing gated by them can activate on mainnet from this code as it stands.

## 2. Where testing stands today

Run on `924e6b8` for this plan (go1.27.1, darwin/arm64):

| Check | Result |
|---|---|
| `go build ./...` | pass |
| `go test ./... -count=1` | pass, 45 packages, 7 packages without tests |
| `go vet ./...` | exit 1: six unkeyed-struct-literal warnings, all in `p2p/legacy` |

The suite is large: 278 test files, about 1,500 tests, 10 fuzz targets (all PTLC). 141 of the 159 pull requests added at least one test; the 18 that did not are listed in appendix A with `0` in the "New tests" column.

What has not been done on this tree, or on any earlier `dev` tree:

- No full mainnet sync of a `dev` binary.
- One devnet smoke run and one end-to-end driver run of the merged tree (2026-11-18; all driver cases pass on the stock, all-on, mainnet-like and all-off genesis). The smoke run found a skipped pillar slot and reproduced issue #105 on the stock late joiner; the driver showed the halt on an unimplemented spork and that Bridge and Liquidity are live on the stock devnet. Individual fixes were rehearsed earlier on a private devnet on the v0.0.10 line, not on `dev`.
- No mixed-version run of `dev` against v0.0.10.
- CI runs `go build` and `go test -failfast -p 1` on Linux only: no `-race`, no `go vet`, no fuzzing, no `libznn` build tag, no devnet, no Windows or macOS. `govulncheck` runs with `continue-on-error`.
- About 115 of the pull requests merged before CI existed on `dev` (#80, 2026-11-03).
- Release binaries are built with Go 1.24.1 through xgo while tests run on the `go.mod` version (1.23).
- No test starts whole `znnd` processes and drives them. The docker devnet is the only full-network setup and nothing automated runs against it.

Packages with source and no tests: `chain/store`, `cmd/znnd`, `consensus/api`, `consensus/storage`, `metadata`, `rpc`, `vm/constants`.

## 3. Exit criteria

`dev` is ready to be cut into a release candidate when all of these hold. Each maps to a level in section 4.

1. L0 and L1 pass on Linux, macOS and Windows, including `-race` and the `libznn` tag.
2. Every row of the base-function matrix (section 5) passes on the devnet with all sporks off, and again with all sporks on.
3. Every feature suite in section 6 passes.
4. A `dev` binary syncs mainnet from genesis to the frontier and its state matches a v0.0.10 node at the checkpoints in section 7.
5. A mixed network of v0.0.10 and `dev` nodes stays on one chain for the agreed soak time with no spork activated.
6. A v0.0.10 data directory opens and continues under the `dev` binary.
7. Every pull request in appendix A has a recorded result for each level marked against it.
8. `test/inventory/check.sh` exits 0: every contract method, RPC method, verifier rule, error value, wire message, flag and config field has a recorded case for each edge class that applies (TESTING.md section 1).
9. Each open item in section 10 has a recorded decision.

## 4. Test levels

| Level | What | Environment | Automated today |
|---|---|---|---|
| L0 | Build and static checks | any machine, CI | partly |
| L1 | Unit, race and fuzz | any machine, CI | partly |
| L2 | Base functions on a live devnet | docker devnet (5 nodes, 3 pillars, chain id 69) | no |
| L3 | Spork activation rehearsals | docker devnet with genesis variants | no |
| L4 | Sync, network and adversarial behaviour | devnet plus fault tools | no |
| L5 | Mixed-version compatibility | 10 to 20 node devnet, v0.0.10 and `dev` images | not in this repository yet; an earlier v0.0.8/v0.0.10 rehearsal used separate tooling |
| L6 | Mainnet history: full sync and state comparison | a host with disk and bandwidth for mainnet | no |
| L7 | Upgrade, soak, load, platforms | devnet and mainnet host | no |

### L0. Build and static checks

| Id | Check | Pass when |
|---|---|---|
| L0-1 | `GOTOOLCHAIN=local go build ./...` on Go 1.26 (the `go.mod` minimum) and `go build ./...` on the release toolchain (`go1.27.4`) | both build |
| L0-2 | `make znnd` and `make libznn` on Linux, macOS, Windows | binaries produced; `znnd version` prints a non-empty version, including from a tree without `.git` |
| L0-3 | `go build -tags libznn ./...` and `go test -tags libznn ./cmd/libznn/...` | pass |
| L0-4 | `go vet ./...` | only the six known `p2p/legacy` warnings, or none once fixed |
| L0-5 | `govulncheck ./...` | no reachable finding without a recorded disposition (gorilla/websocket, #144, is known) |
| L0-6 | `staticcheck ./...` | new findings triaged; this is a first run, so expect a backlog |
| L0-7 | Cross builds through the release workflow in dry-run form (no publish) | all five targets build; checksums produced |
| L0-8 | `go run ./cmd/devnet-keygen --verify-genesis docker/devnet/genesis.json` | committed devnet genesis matches the generator |

L0-7 must not publish. The release workflow is disabled on purpose; run the build steps locally or on a fork.

### L1. Unit, race and fuzz

| Id | Check | Pass when |
|---|---|---|
| L1-1 | `go test ./... -count=1` | 45 packages pass |
| L1-2 | `go test -race ./... -count=1` on an idle machine | no data race; `TestSimple_MomentumInsertionBenchmark` is wall-clock sensitive and is judged without `-race` |
| L1-3 | `go test ./... -count=5 -shuffle=on` | no order-dependent or flaky failure. The suite shares package globals and never uses `t.Parallel`, so shuffle is the cheap way to find leaked state |
| L1-4 | Each of the 10 PTLC fuzz targets, 10 minutes each | no crasher |
| L1-5 | New fuzz targets (to be written): account-block and momentum protobuf and JSON decoding, ABI unpack, RLPx and libp2p frame readers, RPC JSON batch parsing, key-file parsing | 10 minutes each, no crasher |
| L1-6 | Coverage report per package (`-coverprofile`) | recorded; packages under an agreed floor get a ticket. No floor is set yet (section 10) |
| L1-7 | Tests for the seven untested packages | at least a smoke test each, or a recorded reason for none |
| L1-8 | Same as L1-1 on Windows and macOS | pass, platform skips reviewed |

### L2 to L7

Detailed in sections 5 to 8.

## 5. Base-function matrix (L2)

Run the whole matrix twice on the docker devnet: once on a genesis with every spork off except Accelerator, HTLC and Bridge (the mainnet state today), once with every spork on. The same driver runs both.

The devnet as committed has Accelerator and HTLC on from height 0, libp2p at 20, dynamic plasma at 30, and no PTLC, EmbeddedRefund or EpochRewardTerms. Its `bridge-liq` spork is listed as not activated, yet Bridge and Liquidity are live because the spork ladder treats HTLC as implying them (shown by driver cases LAD-1 and LAD-2). The matrix therefore needs **genesis variants** (section 8).

For every method: one success case checked through the matching RPC getter, one rejected case (wrong caller, bad amount or bad state), and balances checked before and after.

### 5.1 Embedded contracts

| Contract | Methods to exercise | Read back with |
|---|---|---|
| Plasma | Fuse, CancelFuse (before and after the lock), SetVariables (spork address only, DP on) | `embedded.plasma.get`, `getEntriesByAddress`, `getRequiredPoWForAccountBlock`, `getVariables` |
| Pillar | Register, RegisterLegacy, UpdatePillar, Delegate, Undelegate, DepositQsr, WithdrawQsr, Revoke (in and out of the window), CollectReward, Update | `embedded.pillar.getAll`, `getByOwner`, `getByName`, `checkNameAvailability`, `getDelegatedPillar`, `getDepositedQsr`, `getUncollectedReward`, `getFrontierRewardByPage`, `getQsrRegistrationCost`, `getPillarEpochHistory`, `getPillarsHistoryByEpoch` |
| Sentinel | Register, DepositQsr, WithdrawQsr, Revoke, CollectReward, Update | `embedded.sentinel.*` |
| Stake | Stake (each duration), Cancel (before and after expiry), CollectReward, Update | `embedded.stake.*` |
| Token | IssueToken, Mint, Burn, UpdateToken (owner change, non-mintable, non-burnable) | `embedded.token.getAll`, `getByOwner`, `getByZts` |
| Swap | RetrieveAssets (needs swap entries in genesis) | `embedded.swap.*` |
| Spork | CreateSpork, ActivateSpork; rejection from any other address; community spork address inside and outside its two height windows | `embedded.spork.getAll` |
| Accelerator | Donate, CreateProject, AddPhase, UpdatePhase, VoteByName, VoteByProdAddress, Update: project accepted, rejected, expired; phase paid | `embedded.accelerator.*` |
| HTLC | Create, Unlock (hash types, preimage sizes), Reclaim (before and after expiry), DenyProxyUnlock, AllowProxyUnlock | `embedded.htlc.*` |
| Bridge | Admin: SetNetwork, RemoveNetwork, SetTokenPair, RemoveTokenPair, SetOrchestratorInfo, SetBridgeMetadata, SetNetworkMetadata, SetAllowKeyGen, ChangeTssECDSAPubKey, ChangeAdministrator, Halt, Unhalt, NominateGuardians, ProposeAdministrator, Emergency. User: WrapToken, UpdateWrapRequest, UnwrapToken, Redeem, RevokeUnwrapRequest | `embedded.bridge.*` (15 getters) |
| Liquidity | Donate, Fund, BurnZnn, SetTokenTuple, LiquidityStake, CancelLiquidityStake, UnlockLiquidityStakeEntries, CollectReward, SetIsHalted, SetAdditionalReward, ChangeAdministrator, ProposeAdministrator, NominateGuardians, Emergency, Update | `embedded.liquidity.*` |
| PTLC | Create, Unlock, ProxyUnlock, Reclaim, CreateWithReclaimLock, ReclaimWithSignature (spork on); every method rejected with the spork off | `embedded.ptlc.*` |

Bridge wrap and unwrap need a TSS signature. The driver holds a test secp256k1 key and signs as the orchestrator would. Time challenges on the bridge and liquidity admin methods need the challenge window to pass; use a genesis variant with short windows or run long enough.

Rewards need epochs. The mainnet epoch is 24 hours. Use a devnet build or genesis variant with a short epoch for reward tests, and one 24-hour run on the stock constants as part of the soak (L7).

### 5.2 Ledger and account chain

| Id | Case |
|---|---|
| B-1 | Send and receive ZNN, QSR and a custom ZTS between two accounts; balances and account heights correct on all five nodes |
| B-2 | Send with fused plasma, with PoW, and with both; under-powered block rejected |
| B-3 | Send with data up to the limit and one byte over |
| B-4 | Receive by the wrong account rejected (rule from genesis on every chain but mainnet, #182) |
| B-5 | Double receive, receive of an unknown send, wrong previous hash, wrong height, bad signature, wrong chain identifier: each rejected with the right error |
| B-6 | An account with more than 500 pending receives pages correctly (#285) |
| B-7 | Contract-receive blocks and descendants: plasma fields must be zero, descendants fully checked (v0.0.10 rule from #309) |
| B-8 | A momentum is produced every 10 seconds by the three pillars in rotation for 1,000 momentums with no gap |
| B-10 | Amount and from-hash fields of a send: negative amount, missing ZTS, from-hash must be zero: each refused with the verifier's own error |
| B-11 | Acknowledgement gap between the account frontier and the confirmed momentum: refused |
| B-12 | Receive carrying data: refused with the right error while the rule is in force |
| B-13 | Forged embedded receive: refused at the RPC boundary (`ErrEmbeddedAddressNotAllowed`, pinned by `TestLedger/B-5n` since #327); the sequencer, changes-hash and acknowledgement layers are covered by verifier and `vm/embedded/tests` unit tests — no RPC e2e path remains |
| B-14 | Descendant blocks attached to a user send: refused |
| B-15 | Walk the recent momentums: chain identifier, version 2 only past the dynamic-plasma enforcement, timestamp, previous, data, fusion and work prices, content, producer a registered pillar |
| B-16 | Acknowledgement recency of the content blocks inside a momentum |
| B-17 | The dynamic-plasma boundary read back from history: version 1 at and below the enforcement height, 2 past it |

### 5.3 RPC

| Id | Case |
|---|---|
| R-1 | Every method in `ledger`, `stats` and the twelve `embedded.*` namespaces called once with valid input against a populated devnet; response decodes with the Dart or Go client types |
| R-2 | Every paged method: page 0, last page, one past the end, page size 0, the maximum, the maximum plus one, and a page index near `uint32` max (#189, #215) |
| R-3 | Response diff against a v0.0.10 node for every read-only method on the same chain state (section 7 covers how both get the same state). Differences must be listed and intended |
| R-4 | Subscriptions over WS: `momentums`, `allAccountBlocks`, `accountBlocksByAddress`, `unreceivedAccountBlocksByAddress`; each account block notified once per momentum (#146); unsubscribe; client disconnect mid-stream |
| R-5 | `ledger.publishRawTransaction` with malformed JSON, null descendants, huge amount strings, deep nesting (#190, #251) |
| R-6 | HTTP and WS on separate ports, on a shared port, each disabled in turn (#87, #119) |

### 5.4 Wallet, CLI and node

| Id | Case |
|---|---|
| O-1 | `znnd version`, `znnd license`; every flag in `app/flags.go` overrides its config field |
| O-2 | Config selection: `--data` alone, `--config` alone, both, neither; a `config.json` in the working directory is not picked up by accident (#187) |
| O-3 | `--pprof` on and off; pprof binds to loopback (#187) |
| O-4 | Producer start with a correct key file, wrong password, wrong index, missing file; password never in logs or printed config (#90) |
| O-5 | Key file written by v0.0.10 opens under `dev`; malformed nonce, wrong `baseAddress`, truncated file each give an error and no panic (#186, #207, #258) |
| O-6 | SIGINT and SIGTERM during sync, during production, and during a subscription stream: clean exit, no goroutine left, database reopens |
| O-7 | Start with a genesis the node rejects: negative balance, supply mismatch, impossible per-entry values (#284) |
| O-8 | Start on a data directory created under a different spork address: refused with a clear message (#286) |
| O-9 | `libznn`: `RunNodeWithConfig` returns an error code instead of exiting the host; `StopNode` then a second `RunNode` in the same process (#212) |
| O-10 | Default config serves RPC on loopback only; exposure warnings appear for a wildcard bind, CORS, vhosts and origins (#88, #126) |

## 6. Feature suites for what `dev` added

### 6.1 Sporks (L3)

One rehearsal per spork on a fresh devnet, driven the way mainnet would be: `CreateSpork`, `ActivateSpork`, wait for the enforcement height.

| Id | Case | Pass when |
|---|---|---|
| S-1 | Before activation, every gated method and rule is off | gated sends rejected; pre-spork behaviour matches a v0.0.10 node |
| S-2 | Activation at height H | all five nodes switch at H, no fork, no node exits |
| S-3 | A node that was offline across H syncs through it | reaches the frontier with the same state |
| S-4 | A node built without the spork in `ImplementedSporksMap` | exits with status 2 at enforcement, as designed |
| S-5 | Restart of every node after activation | spork state survives, chain continues |
| S-6 | Sporks activated in different orders, including all in one momentum | the method table matches the released ladder for all 32 combinations (#300 fixed #290 here) |
| S-7 | An account block that acknowledges a momentum from before H, sent after H | judged by the rule #280 sets (every enforced spork must be acknowledged) |

Run S-1 to S-5 for each of: `Libp2pSpork`, `DynamicPlasmaSpork`, `PtlcSpork`, `EmbeddedRefundSpork`, `EpochRewardTermsSpork`, and `BridgeAndLiquiditySpork` (not activated in the stock devnet genesis).

### 6.2 libp2p (L3, L4)

Follows `docs/libp2p-rollout.md`, which has the expected log lines.

| Id | Case |
|---|---|
| P-1 | Swap at the enforcement height on all nodes; momentum production does not pause for longer than one tick |
| P-2 | `P2PBackend` = `auto`, `legacy`, `libp2p`; a `legacy` node after the swap is isolated, a `libp2p` node before the swap is isolated (#121) |
| P-3 | Bootstrap: a cold node with one bootstrap peer finds the rest through the DHT (the `syncnode` profile). `DefaultBootstrapPeers` ships empty, so the mainnet list is a release input |
| P-4 | Capacity: `MaxPeers` reached, inbound per-origin bound, outbound slots reserved (#151, #307); 16-peer dial loop behaviour |
| P-5 | Peer store: cap enforced on write, newcomers evicted before established peers, diversity keyed on the observed origin (#221, #278); restart keeps the store |
| P-6 | Keepalive: a peer that floods pings gets one pending pong (#169); a silent peer is dropped |
| P-7 | Frame reader: oversized and slow-drip frames (#252) |
| P-8 | Swap failure: libp2p cannot bind its port at H. The node keeps retrying with backoff and legacy cleanup leaves nothing behind (#173) |
| P-9 | Issue #105 (bootstrap deadlock when every node swaps at once and none has a libp2p peer): reproduce or show the operator procedure works |
| P-10 | Legacy before the swap: RLPx handshake with an off-curve key, oversized frames in and after the handshake (#92, #130, #309) |
| P-11 | Discovery: neighbour ids validated before bonding, inbound bond admission, table lock not held across a ping, expired entries swept (#86, #97, #140, #153, #259); the 12 remaining default seeders answer (#279) |

### 6.3 Dynamic plasma (L3, L7)

| Id | Case |
|---|---|
| D-1 | At activation the momentum version becomes 2 and carries fusion and work prices; `embedded.plasma.getRequiredPoWForAccountBlock` quotes match what the verifier accepts (#170, #280) |
| D-2 | Price moves by at most 10% per momentum and never below the minimum; rises under load, falls when idle |
| D-3 | Momentum content: plasma-based cap replaces the 100-block cap; ordering by price; an oversized pending block is skipped and does not end selection (#249); encoded size bound (#145, #181) |
| D-4 | Underpriced block: rejected at the pool, evicted when the price rises, replaced by a higher-priced one for the same height (#213) |
| D-5 | Embedded-call argument pricing and the 16 KiB data limit; trailing bytes rejected (#179, #223) |
| D-6 | Pillar `Update` sends pay the current price and stale ones are replaced (#183) |
| D-7 | `SetVariables` only from the spork address (#174); out-of-range values rejected; the contract batch is sized by input, not by the ceiling (#168) |
| D-8 | Acknowledgement recency: a block acknowledging an old momentum is rejected after activation |
| D-9 | Gated contract fixes turn on with the spork: reward loops bounded per call (#234), accelerator final tally (#237), guardian bounds (#233), token-tuple percentages (#210), HTLC preimage and bridge signature lengths (#179) |
| D-10 | Load: `nom-loadtest` at 10, 50 and 200 TPS for 10 minutes each; record momentum interval, price curve, memory and pool size. The tool is a Node.js project and runs inside `nodebox` |
| D-11 | Rollback across the activation height, then re-apply |

### 6.4 PTLC, embedded refund, epoch reward terms (L3)

| Id | Case |
|---|---|
| C-1 | PTLC happy paths for the three point types (ED25519, BIP340, secp256k1 point) with the independent hash vectors (#152); proxy unlock; reclaim after expiry |
| C-2 | PTLC reclaim lock: `CreateWithReclaimLock`, `ReclaimWithSignature` before expiry with a valid and an invalid signature (#216) |
| C-3 | PTLC rejects undecodable points, wrong point type, zero amount, expired at creation (#162) |
| C-4 | Embedded refund: a failing contract-to-contract send is refunded with the spork on, and behaves as on v0.0.10 with it off (#277); the stalled Burn recovery height |
| C-5 | Epoch reward terms: change a pillar's give-percentages mid-epoch; the epoch that was running settles on the old terms and the next on the new (#288) |

### 6.5 Sync and protocol robustness (L4)

| Id | Case | Tool |
|---|---|---|
| N-1 | Cold sync from genesis of a 10,000-momentum devnet; state equal to a pillar's (#103: momentums 2 to 510 were once skipped) | `syncnode` profile |
| N-2 | Sync with a peer that withholds the head, sends empty hash replies, or stalls mid-batch: the session ends within the deadline (#226, #272) | fault peer |
| N-3 | A peer serving an invalid side-chain: one rollback at most, branch remembered for an hour, serving peer quarantined, relayers of a gossiped invalid momentum not quarantined (#244, #287, #304, #305, #309) | the fault peer (section 8) |
| N-4 | Failed side-chain replacement restores the canonical branch and the pending pool (#83, #120, #175) | forced fork with `docker pause` |
| N-5 | Production readiness: a pillar restarts 100 momentums behind, catches up, and produces in its next slot; a peer that claims a higher TD and delivers nothing holds production for at most the stall window (#245; #291, fixed by #306) | fault peer |
| N-6 | Network partition of 1 pillar from 2 for 5, 25 and 40 momentums, then heal. At 40 the 30-momentum rollback cap is passed (#312 is open: expected to fail today, record the behaviour) | `docker network disconnect` |
| N-7 | Request budget: a peer over the lookup or byte budget is dropped; honest peers under load are not (#84, #129, #220) | fault peer |
| N-8 | Broadcast with one peer that stops reading: other peers still get momentums on time (#275) | fault peer |
| N-9 | Rollback during a peer handshake does not crash the node (#301 fixed #293) | looped connect during forced rollbacks |
| N-10 | 24 hours with random node restarts, pauses and partitions every few minutes: one chain at the end, no panic in any log | chaos script |

### 6.6 RPC hardening (L4)

| Id | Case |
|---|---|
| H-1 | Batch of 1,000 and 1,001 requests; batch response over 25 MB; batch whose client disconnects frees its slot (#89, #302) |
| H-2 | 257 concurrent HTTP requests return 503 for the excess; 1,025 connections (#276) |
| H-3 | WS: 65 subscriptions on a connection, 4,097 globally, per-IP cap when set, connection cap on the WS-only listener (#85, #123, #303) |
| H-4 | A subscriber that stops reading is torn down and does not delay others (#156, #158, #209) |
| H-5 | `getDetailedMomentumsByHeight` over a range of full momentums stays under the byte budget; a single momentum over the budget returns the documented error (#281) |
| H-6 | Name and hash-list inputs at and over their bounds (#250); bridge request paging on a table of 100,000 entries (#283) |
| H-7 | Virtual-host and origin checks, case-insensitive host (#114) |
| H-8 | 1,000 WS handshakes with bad origins leave no goroutines behind (#188) |

## 7. Consensus safety against mainnet and v0.0.10 (L5, L6)

This is the part unit tests cannot settle. These changes are not behind a spork:

| Change | PR | State |
|---|---|---|
| Embedded method table selection | #74, fixed by #300 | fixed to follow the released ladder; needs replay to confirm |
| PoW target comparison made unsigned | #66 | differs for `Difficulty >= 2^63`; issue #78 open, no decision |
| Bridge TSS key: off-curve rejected, coordinates padded | #66 | issue #78 open; scan result for the padding is recorded on #292 |
| Receive recipient rule from genesis | #182 | same on mainnet by construction; changes other chains |
| Contract-receive validation from the renewal height | #309 | same rule and height as v0.0.10 |
| Panic-to-error conversions, nil guards, tombstone skipping, ABI decode guards | #184, #206, #208, #236, #243, #246, #248, #255, #257, #273 | argued neutral by reading; needs replay |
| Account pool and chain cache rewrite | #43, #44, #67, #69 | performance change on the validation path; needs replay |

### L6. Mainnet history

| Id | Case | Pass when |
|---|---|---|
| M-1 | Full sync of a `dev` binary from genesis to the frontier with default config | reaches the frontier, no rejected momentum, no panic |
| M-2 | State comparison against a v0.0.10 node at fixed heights: every spork activation height, the heights around 4,188,199 to 4,188,206 (the HTLC-before-Bridge window from #290), the renewal height 14,567,887, and the frontier | momentum hash and changes hash equal at each height; a sample of 1,000 account infos and every embedded getter equal |
| M-3 | History scan for the #78 cases: any account block with `Difficulty >= 2^63`; any `ChangeTssECDSAPubKey` send with an off-curve key | none found, or each one listed with the height where `dev` and v0.0.10 would disagree |
| M-4 | Follow the frontier for 7 days next to a v0.0.10 node | same frontier hash at every sampled height |
| M-5 | Sync time, peak memory and disk for `dev` and v0.0.10 on the same host | recorded; `dev` not slower by more than an agreed margin |
| M-6 | Same as M-1 with `P2PBackend` left at `auto` | stays on legacy, since `Libp2pSpork` is a placeholder |

There is no local mainnet node today, so L6 needs a host (section 10). M-2 needs both binaries on the same host with RPC on loopback.

### L5. Mixed-version devnet

Needs tooling that renders a larger devnet, runs timed cycles and drives an overtake and a relay attack, pointed at a `dev` image and a v0.0.10 image. An earlier v0.0.8/v0.0.10 rehearsal had such tooling outside this repository; it has to be brought in or rewritten. Both images need the same devnet constants.

| Id | Case | Pass when |
|---|---|---|
| X-1 | 6 `dev` pillars and 6 v0.0.10 pillars, no spork activated, base-function driver running, 6 hours | one chain; every block one side makes is accepted by the other |
| X-2 | Same, with the RPC response diff from R-3 between a `dev` node and a v0.0.10 node | only intended differences |
| X-3 | Rolling upgrade: start all on v0.0.10, replace one node at a time with `dev` | no fork, no missed slot beyond the restart |
| X-4 | Spork activated on the mixed network | v0.0.10 nodes stop at enforcement (they lack the spork), `dev` nodes continue. This is the expected split and confirms the upgrade deadline is real |
| X-5 | Relay of an invalid momentum through v0.0.10 nodes to `dev` nodes | no honest peer quarantined |

## 8. Tooling to build

The first row exists (`test/e2e/`, 97 cases on a chain with every spork (the `all-on` variant) plus 3 destructive ones, TESTING.md procedure P9) and so do the genesis variants that only change the spork list (`test/devnet/genesis.py`, procedure P10). A short epoch, short locks and a bridge administrator with a known key are compiled-in constants; the test build of procedure P11 (`FAST=1 test/devnet/build-image.sh`, 21 cases) provides them without changing the node's source. The rest is the main cost of this plan.

| Tool | Purpose | Notes |
|---|---|---|
| End-to-end driver | Runs sections 5 and 6.1 to 6.4 against any RPC endpoint and reports pass or fail per case id | Written in Go on the repository's own `wallet`, `rpc/server` client and `vm/embedded/definition` ABIs, so it needs no SDK and no Node.js. Proposed home: `test/e2e/`, behind a build tag so `go test ./...` does not start it |
| Genesis variants | `all-off`, `mainnet-like` (Accelerator, HTLC, Bridge on), `all-on`, `activate-live` (sporks created by the driver), `short-epoch` | Extend `cmd/devnet-keygen` to emit them. `short-epoch` needs the epoch length reachable from genesis or a build flag; today it is a constant |
| RPC differ | Calls every read-only method on two endpoints and diffs the JSON | Used by R-3, M-2, X-2 |
| Fault peer | Speaks the wire protocol and misbehaves on request: withholds, stalls, floods, lies about TD, serves an invalid branch | A small probe written for an earlier rehearsal exists outside this repository and is a starting point |
| Chaos script | Pauses, restarts and partitions devnet containers on a schedule and checks for one chain at the end | Shell or Python over `docker compose` |
| CI additions | `-race` job, `go vet`, `libznn` tag, `--verify-genesis`, a nightly devnet job that runs the driver, a weekly fuzz job | The devnet job needs about 10 minutes of runner time per variant |

## 9. Open pull requests

Tested on their own branch before merge, then again as part of `dev`. State on 2026-11-25: #306, #311 and #274 were merged into `dev` on 2026-11-21, so their tests now run as part of `dev`; the other three are still open. This section goes out of date as pull requests merge; check each one's state before relying on it.

| PR | What | Extra tests |
|---|---|---|
| #306 (merged) | Sessions that import nothing no longer hold production (fixes #291) | N-5 with the fault peer; its description says it has not been run on a devnet |
| #311 (merged) | Sentinels for deterministic momentum refusals (part of #299) | N-3 with each of the four refusal types |
| #274 (merged) | Accelerator tally against the active electorate, gated by `DynamicPlasmaSpork` (fixes #270) | Vote, revoke, tally before and after the spork; FPI-7 and FPI-8 |
| #310 | Pillar jail and paid unjail behind a new spork | Its own suite: missed-slot counting, jail, unjail fee burn, election without jailed pillars, S-1 to S-5 |
| #155 (draft) | Remove legacy devp2p; must wait for libp2p activation on mainnet | Repeat 6.2 without legacy; conflicts with `dev` today |
| #72 (draft) | Protocol-level multisig behind `MultisigSpork` | Its own suite; conflicts with `dev` today |

## 10. Decisions needed

1. **Mainnet host for L6.** Where does the full sync run, and who operates it? Nothing in section 7 can start without it.
2. **Issue #78.** Gate the PoW-target and bridge-key changes, or accept them on the strength of the M-3 scan. The scan can run first.
3. **Spork ids.** Four are placeholders. Testing uses genesis-seeded ids; a release needs the real ones and a re-run of 6.1 on that build.
4. **Go toolchain.** Tests run on 1.23, release binaries on 1.24.1. Pick one, or test on both (L0-1 assumes both).
5. **Scope of the release candidate.** Which of the open pull requests (section 9) are in. #310 and #72 add sporks and their own suites.
6. **Coverage floor** for L1-6, if any.
7. **Where the driver and this plan live**: in the repository (`test/e2e/`, `docs/`) or outside it.
8. **Soak lengths**: 6 hours for X-1, 24 hours for N-10 and 7 days for M-4 are proposals.

## 11. Order of work

| Phase | Work | Depends on |
|---|---|---|
| 1 | L0 and L1 in full on three platforms; add `-race`, `vet` and the `libznn` tag to CI | nothing |
| 2 | Start the mainnet sync (M-1) and the M-3 scan; they run for days in the background | decision 1 |
| 3 | Build the driver and genesis variants; run section 5 on `mainnet-like` | nothing |
| 4 | Spork rehearsals (6.1) and feature suites 6.2 to 6.4 | phase 3 |
| 5 | Fault peer and chaos script; 6.5 and 6.6 | phase 3 |
| 6 | Mixed-version runs (L5) and state comparison (M-2) | phases 2 and 3 |
| 7 | Soak, load, upgrade and platform runs (L7): D-10, N-10, M-4, O-5, O-8 | phases 4 to 6 |
| 8 | Fill in appendix A; review failures; decide on the release candidate | all |

Phases 1, 2 and 3 can start at once and do not touch the same files.

## 12. Recording results

One results file per run under `docs/test-results/<date>-<commit>/`, with the case id, commit, environment, outcome and a link to the log. A case passes only for the commit it ran on. After a merge into `dev`, re-run the levels marked against the packages that merge touched (appendix A gives the mapping), and re-run L0 and L1 always.

## Appendix A. Per-PR test matrix

All 159 pull requests merged into `dev`, grouped by the main package they touch. "New tests" is the number of `Test` or `Fuzz` functions the merge added. Levels were assigned from the packages each merge touched and should be corrected by hand where a change is narrower than its package:

- **U** unit (L1), always
- **F** base-function matrix on the devnet (section 5)
- **S** spork rehearsal (6.1)
- **N** network and sync suites (6.2, 6.5)
- **A** RPC suites (5.3, 6.6)
- **O** node, wallet and CLI cases (5.4)
- **R** mainnet replay and mixed-version (section 7)

A `0` under "New tests" means the change has no regression test of its own; those need a case written or a recorded reason.
### Chain, cache, database, account pool (19 PRs)

| PR | Author | Change | New tests | Levels |
|---|---|---|---|---|
| #43 | vilkris4 | Improve account pool performance | 3 | U F R |
| #64 | 0x3639 | fix(account-pool): defensively copy cached blocks to address PR #43 review | 4 | U F R |
| #115 | mrstockholmz | fix(chain): only evict touched addresses on momentum delete | 15 | U F R |
| #120 | mrstockholmz | fix(chain): preserve pending state across pool rebuild | 19 | U F R |
| #133 | 0x3639 | common/db: make ldbManager.Stop idempotent and release every test manager | 1 | U F R |
| #135 | 0x3639 | db: make every manager Stop idempotent and uniform | 9 | U F R |
| #154 | mrstockholmz | fix(nom,types): return errors from DeProto helpers instead of panicking | 8 | U F R |
| #172 | 0x3639 | chain: pin the account pool's pricing cache to the frontier across bursts | 1 | U F R |
| #178 | mrstockholmz | fix(chain/cache): propagate iterator seek errors from findValue | 2 | U F R |
| #192 | 0x3639 | chain/momentum: do not size the time-lookup map for the whole height span | 3 | U F R |
| #204 | mrstockholmz | fix(chain/cache): return ErrCacheStopped instead of panicking on nil DB after Stop | 5 | U F R |
| #213 | mrstockholmz | fix(chain): fail closed when replacement walk previous is non-descending | 1 | U F R |
| #217 | 0x3639 | fix(chain/cache): report a handle closed after acquisition as ErrCacheStopped | 14 | U F R |
| #246 | mrstockholmz | fix(common/db): skip tombstones in enableDelete iterator Next/Prev/Last | 6 | U F R |
| #249 | 0x3639 | fix(chain): skip an oversized pending block instead of ending content selection | 9 | U F R |
| #257 | 0x3639 | fix(chain,db): check what store reads return before using it | 7 | U F R |
| #284 | 0x3639 | fix(chain/genesis): reject per-entry genesis values the contracts could never produce | 7 | U F R |
| #286 | 0x3639 | fix(chain): bind the chain database to the genesis spork address | 8 | U F S R |
| #301 | 0x3639 | fix(chain): bound the uncommitted-block listing by the manager's own blocks | 5 | U F R |

### Protocol, sync, fetcher, downloader (18 PRs)

| PR | Author | Change | New tests | Levels |
|---|---|---|---|---|
| #59 | digitalSloth | feat: libp2p | 59 | U F N A O R |
| #69 | digitalSloth | fix: address cache-chain review feedback on PR #68 | 14 | U F N R |
| #84 | 0x3639 | protocol: bound the number of hashes one GetBlocksMsg may name | 13 | U N |
| #95 | 0x3639 | fetcher: keep the per-peer announce counter exact across a fetch | 12 | U N |
| #103 | 0x3639 | fix(downloader): fresh nodes skip momentums 2-510 during sync | 7 | U N |
| #129 | mrstockholmz | fix(protocol): deduplicate GetBlocksMsg hashes and cap reply size | 7 | U N |
| #175 | edgepillar | fix(protocol): restore pending pool after rejected side-chain replacement | 1 | U N |
| #181 | mrstockholmz | fix(protocol): check momentum content limit before lock-held copy and snapshot | 0 | U N |
| #191 | 0x3639 | protocol/fetcher: make Stop own the event loop and every import it starts | 6 | U N |
| #214 | 0x3639 | fix(p2p/legacy): join the run loop in Stop and the protocol goroutines in Peer.run | 15 | U N |
| #220 | 0x3639 | protocol: pace the requests one peer can have served with a per-peer budget | 27 | U N |
| #226 | 0x3639 | protocol/downloader: deliver the hash fetcher's stop flag and bound oversized hash replies | 6 | U N |
| #238 | mrstockholmz | fix(protocol): join txsyncLoop in Stop and use NewTicker with Stop | 1 | U N |
| #272 | mrstockholmz | fix(protocol/downloader): session-level no-progress deadline | 12 | U N |
| #275 | 0x3639 | fix(protocol): queue broadcasts per peer instead of writing them inline | 8 | U N |
| #287 | 0x3639 | fix(protocol): classify consensus rejections on the extension path too | 4 | U N |
| #304 | 0x3639 | fix(protocol/fetcher): keep the peer that delivered a momentum failing the pre-check | 4 | U N |
| #305 | 0x3639 | fix(protocol): hold a batch refused for its shape against the right peer | 12 | U N |

### P2P (legacy, libp2p, discovery, switcher) (17 PRs)

| PR | Author | Change | New tests | Levels |
|---|---|---|---|---|
| #86 | 0x3639 | p2p/discover: admit inbound bonds before starting them | 7 | U N |
| #91 | 0x3639 | p2p/legacy: answer pings from the keepalive loop | 5 | U N |
| #92 | 0x3639 | p2p/legacy: bound the RLPx frame size before allocating it | 6 | U N |
| #97 | 0x3639 | p2p/discover: expire node database entries without a node record | 8 | U N |
| #121 | mrstockholmz | feat(p2p): add Net.P2PBackend config override for transport selection | 4 | U N O |
| #130 | mrstockholmz | fix(p2p/legacy): bound handshake-phase frames before allocating | 9 | U N |
| #140 | edgepillar | p2p/discover: validate neighbor node IDs before bonding | 2 | U N |
| #151 | edgepillar | p2p/libp2p: preserve tracked outbound dials during capacity rejection | 4 | U N |
| #153 | 0x3639 | p2p/discover: join the expiration sweep on close; keep the record of a refreshed node | 3 | U N |
| #169 | 0x3639 | p2p/libp2p: answer pings from the keepalive loop with one pending pong | 3 | U N |
| #173 | 0x3639 | p2p: leave nothing behind when a backend fails to start | 3 | U N |
| #221 | 0x3639 | p2p/libp2p: enforce the peer-DB cap on the write path and evict newcomers before established peers | 13 | U N |
| #252 | 0x3639 | fix(p2p/libp2p): grow frame buffers as bytes arrive, keep the disconnect write deadline, join protocol goroutines | 8 | U N |
| #259 | 0x3639 | fix(p2p/discover): ping replacement candidates outside the table lock and release the seed iterator | 6 | U N |
| #278 | 0x3639 | fix(p2p/libp2p): key peer-DB diversity on the observed connection origin | 2 | U N |
| #279 | 0x3639 | fix(p2p): prune DefaultSeeders to the nodes that still answer discovery | 2 | U N |
| #307 | 0x3639 | fix(p2p/libp2p): reserve outbound slots and bound inbound peers per origin | 10 | U N |

### RPC server (15 PRs)

| PR | Author | Change | New tests | Levels |
|---|---|---|---|---|
| #85 | 0x3639 | rpc: bound subscriptions per connection and server-wide | 20 | U A O |
| #89 | 0x3639 | rpc: bound batch size and batch response size | 7 | U A |
| #106 | mrstockholmz | rpc/server: correct the Notifier.Notify doc comment | 0 | U A |
| #112 | 0x3639 | rpc: register a codec before Stop can take its view of the codec set | 7 | U A |
| #117 | mrstockholmz | fix(rpc/server): move conn.Close outside sync.Once to prevent re-entrant deadlock | 3 | U A |
| #132 | mrstockholmz | fix(rpc/server): bound concurrent calls per connection | 12 | U A |
| #156 | 0x3639 | rpc/server: tear the connection down on the first terminal write failure; notifier states | 20 | U A |
| #158 | 0x3639 | rpc/server: fail a request whose write outlives the read loop instead of waiting forever | 8 | U A |
| #188 | 0x3639 | rpc/server: stop leaking a goroutine per WebSocket handshake in the origin check | 3 | U A |
| #253 | 0x3639 | fix(rpc/server): validate HTTP batch responses in the client | 6 | U A |
| #254 | 0x3639 | fix(rpc/server): cancel before waiting at teardown and bound client subscriptions | 9 | U A |
| #256 | 0x3639 | fix(node,app): tighten RPC startup, shutdown and config loading | 7 | U A O |
| #276 | 0x3639 | fix(rpc/server): bound the HTTP requests and connections served at once | 23 | U A O |
| #282 | 0x3639 | fix(rpc/server): remove the unreachable IPC endpoint | 0 | U A |
| #302 | 0x3639 | fix(rpc/server): stop a batch when its context has ended | 4 | U A |

### RPC API (19 PRs)

| PR | Author | Change | New tests | Levels |
|---|---|---|---|---|
| #49 | georgezgeorgez | Fix rpc unmarshalJSON bugs | 0 | U A |
| #51 | 0x3639 | Fix GetAccountBlocksByPage to properly set 'more' property | 0 | U A |
| #52 | 0x3639 | Fix isRevocable field in plasma getEntriesByAddress API | 0 | U A |
| #57 | 0x3639 | Fix GetAllUnwrapTokenRequests to sort by RegistrationMomentumHeight | 1 | U A |
| #80 | digitalSloth | ci: test action and build workflows | 0 | U A O |
| #93 | 0x3639 | rpc/embedded: enrich only the requested page of projects and requests | 3 | U A |
| #146 | 0x3639 | rpc/subscribe: notify each account block once per momentum | 5 | U A |
| #150 | 0x3639 | rpc/subscribe: address #146 review follow-ups | 0 | U A |
| #170 | edgepillar | fix(rpc): expose the existing fused-only plasma target in required PoW quotes | 3 | U A |
| #180 | mrstockholmz | fix(rpc/api): nil-guard BalanceInfo and AccountBlockList JSON decode | 6 | U A |
| #189 | 0x3639 | chain/account, rpc/api/embedded: bound height ranges at the frontier and widen page offsets before multiplying | 6 | U F A R |
| #190 | 0x3639 | chain/nom, rpc/api: reject a null descendant entry when decoding an account block from JSON | 6 | U F A R |
| #209 | mrstockholmz | fix(rpc/api/subscribe): isolate delivery per subscription so a blocked connection cannot stall the broadcast worker | 6 | U A |
| #215 | sol-znn | rpc/api: fix pageIndex wrap in ledger paging | 0 | U A |
| #235 | mrstockholmz | fix(rpc/api/embedded): initialise embedded marshal pointer in bridge UnmarshalJSON | 2 | U A |
| #250 | 0x3639 | fix(rpc/api/embedded): bound name and hash-list inputs; fix the countdown underflow | 5 | U A |
| #251 | 0x3639 | fix(chain/nom,rpc/api): decode account-block JSON in one pass and bound amount strings | 10 | U F A R |
| #281 | 0x3639 | fix(rpc/api/ledger): bound getDetailedMomentumsByHeight responses by encoded size | 4 | U A |
| #285 | 0x3639 | fix(rpc/api/ledger): page unreceived blocks past the first 500 pending entries | 7 | U F A R |

### VM, embedded contracts, ABI (28 PRs)

| PR | Author | Change | New tests | Levels |
|---|---|---|---|---|
| #67 | digitalSloth | fix: restore acknowledged-height scoping for user receive validation | 2 | U F R |
| #138 | sol-znn | Add Point Time Locked Contracts (PTLC) | 74 | U F A R |
| #141 | mrstockholmz | fix: return errors from DeProto* helpers instead of panicking on malformed input | 12 | U F R |
| #142 | 0x3639 | Merge master (v0.0.9-alphanet) into dev | 40 | U F N O R |
| #168 | 0x3639 | pillar, vm/embedded: size the contract batch by the input and cap the plasma ceiling at 100,000 base blocks | 2 | U F R |
| #174 | 0x3639 | vm/embedded: govern the plasma variables by the spork address | 4 | U F S R |
| #179 | mrstockholmz | fix(vm,verifier): spork-gated data rules under DynamicPlasmaSpork | 26 | U F S R |
| #185 | 0x3639 | vm/abi: keep the caller's integer intact when packing and bound fixed-byte and array declarations | 7 | U F R |
| #205 | mrstockholmz | fix(vm/abi): reject fixed-array sizes that overflow element layout | 4 | U F R |
| #208 | mrstockholmz | fix(vm,pillar): recover GenerateFromTemplate panics, guard nil cache store in priceUpdate | 3 | U F R |
| #210 | mrstockholmz | fix(vm/embedded): reject oversized token-tuple percentages, spork-gated | 3 | U F S R |
| #212 | mrstockholmz | fix(libznn): add explicit-options entry point, remove exit from library path | 34 | U F N O R |
| #216 | sol-znn | ptlc: optional reclaim lock | 30 | U F A R |
| #223 | 0x3639 | vm: price and bound embedded-call argument data under the Dynamic Plasma spork | 12 | U F S A R |
| #233 | duongynhi000005-oss | bridge, liquidity: widen guardian majority threshold and vote counter to int and bound guardian nominations | 10 | U F R |
| #234 | mrstockholmz | fix(vm/embedded): bound reward-update epoch loops, spork-gated by Dynamic Plasma | 9 | U F S R |
| #236 | mrstockholmz | fix(vm/embedded/definition): iterate per-entity sub-ranges in three getters | 11 | U F R |
| #237 | mrstockholmz | fix(vm/embedded): final tally on expired accelerator voting window, spork-gated | 7 | U F S R |
| #243 | mrstockholmz | fix(vm/embedded/definition): skip tombstones in sentinel and accelerator collection readers | 3 | U F R |
| #244 | 0x3639 | fix(protocol): remember rejected side-chains and quarantine the peers that serve them | 20 | U F N R |
| #255 | 0x3639 | fix(vm): reject ABI values that do not fit their fixed-width type | 10 | U F R |
| #273 | 0x3639 | fix(vm/abi): bound the bytes a dynamic array may decode by its input | 5 | U F R |
| #277 | 0x3639 | fix(vm): refund failed contract-to-contract sends behind EmbeddedRefundSpork | 6 | U F S R |
| #280 | 0x3639 | fix(verifier): require user blocks to acknowledge every enforced spork | 21 | U F S R |
| #283 | 0x3639 | fix(rpc/api/embedded): page bridge wrap/unwrap requests without decoding the whole table | 3 | U F A R |
| #288 | 0x3639 | fix(vm): settle each pillar epoch with the give-percentages in force during it | 13 | U F A R |
| #300 | 0x3639 | fix(vm/embedded): select the method table by the released spork ladder | 3 | U F S R |
| #309 | 0x3639 | Merge master (v0.0.10) into dev | 15 | U F N R |

### Verifier, consensus, pillar, PoW, plasma (12 PRs)

| PR | Author | Change | New tests | Levels |
|---|---|---|---|---|
| #60 | digitalSloth | fix: enable momentum verification before broadcast | 7 | U F N R |
| #83 | 0x3639 | chain,protocol: keep the canonical branch when a side-chain replacement fails | 23 | U F N R |
| #145 | mrstockholmz | fix(pillar): bound DetailedMomentum size in content selector | 18 | U F N R |
| #182 | 0x3639 | verifier: enforce the receive recipient rule from genesis on every chain but mainnet | 4 | U F R |
| #183 | sol-znn | pillar: pay the dynamic fusion price on Update blocks and replace stale ones | 5 | U F R |
| #184 | mrstockholmz | fix(chain,common/db): return errors from stored-data reads instead of panicking | 8 | U F R |
| #206 | mrstockholmz | fix(chain,pillar): return errors from stored-data reads that still panic after #184 | 12 | U F R |
| #211 | mrstockholmz | fix(consensus): return ErrNoActivePillars on empty delegation set instead of hanging | 1 | U F R |
| #225 | 0x3639 | consensus: bound the epoch-point walk to its range | 3 | U F R |
| #245 | 0x3639 | fix(protocol,pillar): gate production on a bounded, progress-based readiness predicate | 12 | U F N R |
| #248 | 0x3639 | fix(verifier): reject a block for an account without a chain instead of dereferencing nil | 3 | U F R |
| #289 | 0x3639 | consensus: pin points, elections and fixed pillar readers to one momentum store | 8 | U F R |

### Node, app, wallet, CLI (16 PRs)

| PR | Author | Change | New tests | Levels |
|---|---|---|---|---|
| #58 | digitalSloth | feat: adds dockerised local devnet | 0 | U O |
| #87 | 0x3639 | node: honor the RPC enable flags at startup | 8 | U O |
| #88 | 0x3639 | node: serve RPC to local clients only by default | 4 | U O |
| #90 | 0x3639 | node: keep the producer password out of printed and logged config | 8 | U O |
| #94 | 0x3639 | app: map the --host flag to the P2P listen host | 5 | U O |
| #98 | 0x3639 | node: stop the WebSocket RPC server on shutdown | 1 | U O |
| #114 | mrstockholmz | fix(node): compare virtual host header case-insensitively | 4 | U O |
| #118 | mrstockholmz | fix(node): build listen and RPC addresses with net.JoinHostPort | 7 | U O |
| #119 | mrstockholmz | fix(node): gate shared-port host check on HTTP being configured | 2 | U O |
| #123 | mrstockholmz | fix(node): bound concurrent WebSocket connections per remote IP | 7 | U O |
| #126 | 0x3639 | node,app: show RPC exposure warnings on the console and warn on wildcard vhosts | 0 | U O |
| #186 | 0x3639 | wallet: reject a malformed key-file nonce as an error and refuse to encrypt an empty key store | 4 | U O |
| #187 | 0x3639 | app: read the config file from the data path unless --config is given, and honor --pprof=false | 7 | U O |
| #207 | mrstockholmz | fix(wallet): use the scanned path as the runtime path, not the serialized one | 6 | U O |
| #258 | 0x3639 | fix(wallet): write key files atomically and check what they claim | 14 | U O |
| #303 | 0x3639 | fix(node): cap connections on the WebSocket-only listener | 9 | U O |

### Cross-cutting, CI, docs, merges (15 PRs)

| PR | Author | Change | New tests | Levels |
|---|---|---|---|---|
| #44 | vilkris4 | Improve account block validation performance | 7 | U F N A R |
| #45 | vilkris4 | Fix versioned db tests | 0 | U |
| #56 | mehowz | chore(deps): bump golang.org/x/crypto v0.1.0 -> v0.31.0 (CVE-2024-45337) | 0 | U |
| #63 | digitalSloth | feat: dynamic plasma | 27 | U F A R |
| #66 | 0x3639 | Numerous Bug Fixes | 42 | U F N A O R |
| #70 | digitalSloth | ci: add PR test workflow, remove flaky UPnP discovery test | 0 | U |
| #71 | digitalSloth | fix: reinstate Libp2pSpork in ImplementedSporksMap | 0 | U F S R |
| #74 | digitalSloth | fix: lilp2p & Dynamic Plasma PR | 24 | U F N A R |
| #139 | mrstockholmz | test(p2p/legacy): address #130 review items 3-6 on handshake-phase tests | 0 | U |
| #152 | edgepillar | test(ptlc): add independent unlock message hash vectors | 1 | U |
| #162 | mrstockholmz | test(ptlc): cover undecodable ED25519 point lock in Create validation | 1 | U |
| #177 | 0x3639 | docs: godoc for every exported identifier (port of #62 onto dev) | 0 | U F N A O R |
| #201 | mrstockholmz | fix(devnet): fund the Plasma-contract genesis block | 0 | U |
| #218 | 0x3639 | test(liquidity): pin the reward-budget guard against an installed overflowing split | 5 | U |
| #219 | 0x3639 | test(chain/nom): pin the decode-path error context and the decoder nesting limit | 8 | U |
