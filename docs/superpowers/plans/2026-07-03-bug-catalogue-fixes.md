# Bug-Catalogue Batch-1 Fixes — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix every catalogue entry triaged CONFIRMED-safe in `docs/superpowers/triage-2026-07-03.md`, one commit per bug, on `bugfix/catalogue-batch-1` (off dev @ 70ad9ce).

**Architecture:** No design changes — each task is a minimal, verified fix from the triage record plus the smallest test that demonstrates it (where the package is testable). Quarantined (consensus-sensitive) entries are explicitly out of scope; see triage doc.

**Tech Stack:** Go 1.20+, LevelDB, `go test`; module builds require `GOWORK=off`.

## Global Constraints

- `GOWORK=off` on every go command (parent go.work excludes this module).
- One catalogue entry per commit (shared-fix pairs #56+#61 are one logical bug → one commit).
- NEVER touch quarantined entries: #24, #39, #43, #44, #45, #49, #50-state-machine, #32-mailbox-write, #32-comparer, #42-item-1, #48-item-a, #53-JSON-tag.
- No stored-state or ChangesHash-affecting changes; no JSON wire-tag changes.
- Before changing any error string, grep `vm/embedded/tests/` fixtures for the old string; update fixtures in the same commit.
- Every commit: `gofmt -l .` clean (excluding vendor if any), package tests pass; full `GOWORK=off go test ./...` at each wave end and before push.
- Commits are GPG-signed (repo default). Never `--no-gpg-sign`.
- Commit messages: `<area>: <summary>` subject; body = what/why + "Catalogue entry #N (triage 2026-07-03)" + verification line.

---

## Wave 0 — docs

### Task 0: Commit triage + this plan
- [ ] `git add docs/superpowers/triage-2026-07-03.md docs/superpowers/plans/2026-07-03-bug-catalogue-fixes.md`
- [ ] Commit: `docs: add bug-catalogue triage and batch-1 fix plan`

## Wave 1 — chain & common core

### Task 1 (#26): chain/momentum/ledger_store.go — stop swallowing Apply errors
**Files:** Modify `chain/momentum/ledger_store.go:52-54,81-83`; Create `chain/momentum/ledger_store_test.go`
- [ ] Both sites: `return nil` → `return err` inside the two `if err := ...` blocks (`Apply(patch)` in `AddAccountBlockTransaction`; `setBlockConfirmationHeight`).
- [ ] Test: wrap a memdb in a `db.DB` whose `Apply`/`Put` returns a forced error; call `AddAccountBlockTransaction`; assert the error propagates (currently nil). If constructing the store standalone proves impractical (check `momentum.NewStore` signature), fall back to a table-driven test of the two error paths via a failing `db.DB` stub satisfying only the used methods.
- [ ] `GOWORK=off go test ./chain/...` → PASS. Commit: `chain/momentum: propagate account-block apply errors during momentum insertion`

### Task 2 (#23): common/db — ldbManager.Add must check the real frontier
**Files:** Modify `common/db/versioned_db.go` (ldbManager.Add, ~:325-372); Test `common/db/versioned_db_test.go`
- [ ] Replace the tautological guard: compute `frontierIdentifier := GetFrontierIdentifier(NewLevelDBWrapper(m.ldb).Subset(frontierByte))` under `m.changes.Lock()`, and `if previous != frontierIdentifier { return errors.Errorf("can't insert identifier %v. previous %v doesn't match with current frontier %v", identifier, previous, frontierIdentifier) }` before the Puts/Apply. Delete the old always-true branch.
- [ ] Test (reuse existing mockCommit/mockTransaction helpers): Add t1, Add t2, then Add a transaction with previous = t1's identifier → assert error and frontier unchanged (today: nil error + corrupted frontier).
- [ ] `GOWORK=off go test ./common/db/` → PASS. Commit: `common/db: reject ldb transactions that do not build on the frontier`

### Task 3 (#27): chain/momentum/embedded.go — propagate delegation/pillar read errors
**Files:** Modify `chain/momentum/embedded.go:60,67`
- [ ] `delegations, _ :=` → check-and-return; `registerList, _ :=` → check-and-return (both `return nil, err`).
- [ ] `GOWORK=off go test ./chain/... ./consensus/...` → PASS. Commit: `chain/momentum: propagate storage errors in ComputePillarDelegations`

### Task 4 (#33): chain/genesis/config.go — recover must return an error
**Files:** Modify `chain/genesis/config.go:25-30`; Test `chain/genesis/config_test.go`
- [ ] Named returns `(genesisStore store.Genesis, err error)`; in the deferred recover: log with `"reason", r`, set `genesisStore = nil; err = ErrInvalidGenesisConfig` (sentinel exists in this package — verify name, else use the existing invalid-genesis error the tests assert).
- [ ] Test: genesis JSON that decodes but panics during CheckGenesis (e.g. fusion with `"Amount": null`); assert `ErrInvalidGenesisConfig`, not `(nil, nil)`.
- [ ] `GOWORK=off go test ./chain/genesis/` → PASS. Commit: `chain/genesis: return ErrInvalidGenesisConfig when config validation panics`

### Task 5 (#29): chain/genesis/shared_tests.go — no vacuous balance pass
**Files:** Modify `chain/genesis/shared_tests.go:12-39`; Test `chain/genesis/embedded_genesis_test.go` or `config_test.go`
- [ ] Track `found`; if `!found`, error for any non-zero required amount (exact code in triage/verification record). Zero-amount requirements with no entry must still pass (TestGenesisToFromJson relies on it).
- [ ] Test: config with non-zero fusion, no PlasmaContract genesis block → CheckGenesis errors.
- [ ] `GOWORK=off go test ./chain/genesis/` → PASS. Commit: `chain/genesis: fail balance check when a required address has no genesis block`

### Task 6 (#31): chain/account/mailbox — atMost==0 underflow + unreachable return
**Files:** Modify `chain/account/mailbox/mailbox.go:19-20,84-87`; Create `chain/account/mailbox/mailbox_test.go`
- [ ] Guard `if atMost == 0 { return list, nil }` before the iterator; delete unreachable `return nil` after `panic(err)`.
- [ ] Test: memdb mailbox, MarkAsUnreceived 3 hashes; `atMost=0` → empty; `atMost=1` → 1; `atMost=10` → 3.
- [ ] `GOWORK=off go test ./chain/...` and `go vet ./chain/account/mailbox/` (unreachable-code warning gone) → PASS. Commit: `chain/account: fix GetUnreceivedAccountBlockHashes underflow when atMost is zero`

### Task 7 (#20): common/ticker.go — pre-start clamp + interval guard
**Files:** Modify `common/ticker.go:28-32,54-56`; Create `common/ticker_test.go`
- [ ] `ToTick`: `if !tm.After(t.startTime) { return 0 }` before the division (identity-preserving for all reachable inputs — momentum timestamps are ≥ genesis; ElectionByTime already guards; only pre-genesis wall-clock scheduling changes). `NewTicker`: `if interval < time.Second { panic(...) }` (all real intervals ≥ 1s; test mocks use time.Hour).
- [ ] Test: pre-start time → 0 (today ~1.8e18); post-start identity cases; sub-second interval → panic with clear message (today: runtime divide-by-zero at first ToTick).
- [ ] `GOWORK=off go test ./common/... ./consensus/...` → PASS. Commit: `common: make ticker safe for pre-start times and sub-second intervals`

### Task 8 (#21): common/task.go + pillar/worker.go — panic-safe lifecycle
**Files:** Modify `common/task.go:23-27`, `pillar/worker.go:83-88`
- [ ] task.go: `defer t.finish()` before `action(t)`.
- [ ] worker.go closure: convert to `defer w.working.Unlock()`, `defer w.children.Done()`, `defer common.RecoverStack()` (declared in that order at the top) so a recovered panic cannot skip Unlock/Done and deadlock the worker.
- [ ] `GOWORK=off go test ./pillar/... ./common/... ./vm/embedded/tests/` → PASS. Commit: `common,pillar: make task finish and worker unlock panic-safe`

### Task 9 (#25): common/db — complete the truncated Pop error
- [ ] `versioned_db.go:163` → `errors.Errorf("can't find previous for %v", m.frontierIdentifier)`. Package tests pass. Commit: `common/db: include identifier in memdb Pop error`

### Task 10 (#22): common/types/address.go — correct panic message + double space
- [ ] `:139` cite `AddressSize` and say "address size"; `:56` single space. Grep fixtures for old strings first. Package tests pass. Commit: `common/types: fix DeProtoAddress panic message and SetBytes error spacing`

### Task 11 (#30): chain/account_pool.go — "previous mismatch" error string
- [ ] `:115` `"missing previous"` → `"previous mismatch"`. Grep fixtures. `go test ./chain/...` PASS. Commit: `chain: correct previous-mismatch error reason in account pool`

### Task 12 (#32 items 1+3): delete dead SetFrontier methods + vestigial producer field
- [ ] Delete `momentumStore.SetFrontier` (chain/momentum/momentum.go:13) and `accountStore.SetFrontier` (chain/account/account_block.go:12); delete `producer` field from `AccountBlockMarshal` (chain/nom/account_block.go:336). Do NOT touch the mailbox write or the comparer (quarantined).
- [ ] `GOWORK=off go build ./... && go test ./chain/...` → PASS. Commit: `chain: remove dead SetFrontier methods and vestigial marshal field`

## Wave 2 — vm

### Task 13 (#34): vm/supervisor.go — check generation error before verifying
- [ ] Reorder in `GenerateAutoReceive` (:133-140): `if err != nil { return nil, err }` immediately after `generateEmbeddedReceive`, then the verifier call. `go test ./vm/... ./pillar/...` PASS. Commit: `vm: check generateEmbeddedReceive error before verifying the block`

### Task 14 (#35): vm/plasma.go — cap AvailablePlasma with the plasma constant
- [ ] Replace the `MaxFussedAmountForAccountBig` comparison/return (:78-82) with `constants.MaxFusionPlasmaForAccount` (add a `MaxFusionPlasmaForAccountBig` var beside vm/constants/plasma.go:58). Branch is provably unreachable (FussedAmountToPlasma already caps; ChainPlasma monotone) → behavior-preserving; say so in the commit body. No Fussed→Fused rename (API surface).
- [ ] `go test ./vm/...` PASS. Commit: `vm: use the plasma-unit constant for the AvailablePlasma cap`

### Task 15 (#36): vm/supervisor.go — remove dead double-sign in packBlock
- [ ] Delete the first `if signFunc != nil` block (:245-253); keep the ChangesHash-setting one. Optional test: counting SignFunc through GenerateFromTemplate asserts exactly one invocation.
- [ ] `go test ./vm/... ./vm/embedded/tests/` PASS. Commit: `vm: sign generated blocks once in packBlock`

### Task 16 (#38): vm/vm_context — Done nils the snapshot
- [ ] lifecycle.go Done: add `ctx.accountStoreSnapshot = nil` after reassigning `ctx.Account` (leave the `changes, _ :=` as-is — out of scope). `go test ./vm/...` PASS. Commit: `vm/vm_context: clear account store snapshot in Done`

### Task 17 (#37): vm/abi — fix garbled error messages
- [ ] error.go: swap errArrayOffsetOverflow args (offset = `start+WordSize*size`, len = `len(output)`); errInsufficientLength print `len(output)`; `:40` "varible"→"variable". Grep fixtures for old strings.
- [ ] Create `vm/abi/error_test.go`: short-buffer unpack renders sane integers. `go test ./vm/abi/` PASS. Commit: `vm/abi: fix swapped and mis-typed error message arguments`

### Task 18 (#40): vm/embedded/definition — pin accelerator storage prefixes
- [ ] Move `projectKeyPrefix`/`phaseKeyPrefix` into their own const block with explicit `byte = 12` / `13` + comment "historical values; baked into on-chain storage keys; must never change". Add assertion test (package-local `accelerator_test.go`): `projectKeyPrefix == 12 && phaseKeyPrefix == 13`.
- [ ] `go test ./vm/... ./vm/embedded/tests/` PASS (accelerator suite's hardcoded hashes double-check). Commit: `vm/embedded/definition: make accelerator storage prefixes explicit`

### Task 19 (#41): vm/embedded/definition/swap.go — skip empty values in GetSwapAssets
- [ ] Add `if len(iterator.Value()) == 0 { continue }` before parse (mirrors pillars.go:419-421). RPC-only caller. Test: memdb with one empty-valued 32-byte key + one real entry → listing returns the real entry.
- [ ] `go test ./vm/... ` PASS. Commit: `vm/embedded/definition: skip empty values in GetSwapAssets`

### Task 20 (#42 items 2-4): definition cleanups
- [ ] common.go:404-414 move epoch read after the address err check; rename `Phase.ToProjectMarshal` → `ToPhaseMarshal` (accelerator.go:193; sole caller :210); delete dead `SetTokenPairParam` (bridge.go:1120-1129). Do NOT touch GetAllPillarVotes (quarantined item 1).
- [ ] `GOWORK=off go build ./... && go test ./vm/...` PASS. Commit: `vm/embedded/definition: misc dead-code and ordering cleanups`

### Task 21 (#47): stop aliasing common.Big0 into stored entries
- [ ] `big.NewInt(0)` at stake.go:123, stake.go:217 (`Znn:`), swap.go:127-128, pillars.go:423,452,468 (`Qsr:`). Serialization byte-identical.
- [ ] `go test ./vm/embedded/tests/` PASS (expected-state fixtures prove no serialization change). Commit: `vm/embedded/implementation: do not alias common.Big0 into persisted entries`

### Task 22 (#48 items b-d): pillars.go latent-panic and dead-code cleanup
- [ ] Delete never-written `distributed` map + its debug loop (:373, :473-483); restructure `computePillarRewardForEpoch` so `Weight` is read only after the `!ok` guard (exact shape in triage record); drop always-false `uint8 < 0` clauses (:54,:57). SKIP item (a) (quarantined).
- [ ] `go test ./vm/embedded/tests/` PASS (TestConsensus_1/2 golden logs unchanged). Commit: `vm/embedded/implementation: remove latent nil deref and dead code in pillar rewards`

### Task 23 (#50 log only): accelerator — truthful voting-window log
- [ ] Fix only the Debug log at implementation/accelerator.go:352 (fires in-window, labeled "passed voting period") → e.g. "project vote status inside voting window". NO state-machine change (quarantined). Grep accelerator_test.go expected logs for the old line and update fixtures in the same commit.
- [ ] `go test ./vm/embedded/tests/` PASS. Commit: `vm/embedded/implementation: correct misleading accelerator voting log`

## Wave 3 — consensus

### Task 24 (#51): chain_ticker mismatch error prints the compared hash
- [ ] `consensus/chain_ticker.go:107` `blocks[0].Hash` → `blocks[len(blocks)-1].Hash`. Commit: `consensus: report the compared block hash in chainTicker mismatch error`

### Task 25 (#52): GetPillarDelegationsByEpoch returns instead of panicking
- [ ] `consensus/api.go:67-68`: `common.DealWithErr(err)` → `if err != nil { return nil, err }` (sole caller DealWithErrs the result — net behavior identical). Commit: `consensus: return TickMultiplier error from GetPillarDelegationsByEpoch`

### Task 26 (#53 field + notation): rename Go field, keep wire tag; fix interval notation
- [ ] `consensus/api/pillar_stats.go:13`: field → `ExpectedBlockNum`, KEEP tag `json:"exceptedBlockNum"` + compat comment; mechanical renames (consensus/api.go:59, vm/embedded/implementation/pillars.go:520,526,542,553, rpc/api/embedded/pillar.go:236). `consensus/storage/point.go:100`: `[%v,%v)` ×2 → `(%v,%v]`.
- [ ] `GOWORK=off go build ./... && go test ./vm/embedded/tests/ ./consensus/...` PASS. Commit: `consensus: rename ExceptedBlockNum field (wire tag preserved)`

## Wave 4 — wallet / node shell / protocol

### Task 27 (#54): DeriveForFullPath returns the path
- [ ] `wallet/keystore.go:58` `return path, key, nil` → `return ipath, key, nil`. Create `wallet/keystore_test.go` with `TestDeriveForFullPathReturnsPath`. Commit: `wallet: return the derivation path from DeriveForFullPath`

### Task 28 (#55): honor MaxSearchIndex in address search
- [ ] `KeyStore.FindAddress` gains a `maxIndex uint32` parameter (no in-repo callers); add `Manager.FindAddress(ks, address)` threading `m.config.MaxSearchIndex`. Test: MaxSearchIndex=1, address at index 2 → ErrAddressNotFound; default config finds it. Commit: `wallet: make FindAddress honor the configured MaxSearchIndex`

### Task 29 (#56 + #61, one bug): Lock leaves a live-looking nil entry
- [ ] `wallet/manager.go:151` `m.decrypted[path] = nil` → `delete(m.decrypted, path)`; defense-in-depth nil check in `Stop`'s range. Tests: after Unlock+Lock, `IsUnlocked` false and `GetKeyStore` errors (not nil,nil); Unlock two, Lock one, `Stop` doesn't panic and zeroes the other. Commit: `wallet: delete keystore entry on Lock so IsUnlocked and Stop behave`

### Task 30 (#58): KeyStore.Zero wipes secrets
- [ ] Overwrite `Entropy`/`Seed` bytes before niling; comment the string limitation. Note in body: `keyStoreFromEntropy` aliases the caller's slice — wiping it is the intent. Test: alias `ks.Entropy`, call Zero, assert all-zero. Commit: `wallet: zero entropy and seed bytes in KeyStore.Zero`

### Task 31 (#59): mock zenon restores global state
- [ ] `zenon/mock/zenon.go`: capture logger handlers + `consensus.EpochDuration` + `common.Clock` BEFORE any override; restore all three in `Stop`; add `common.ConsensusLogger` to `AllLoggers`. Check `common.Clock`'s type for the field.
- [ ] Full suite `GOWORK=off go test ./...` (this touches the harness every test uses). Commit: `zenon/mock: restore clock, epoch duration and loggers on Stop`

### Task 32 (#60): downloader findAncestor guard
- [ ] `protocol/downloader/downloader.go:426` `if hash.IsZero()` → `if !hash.IsZero()`; correct the misleading inline comment at the fallthrough return if present. Time-boxed attempt at a unit test via injected `hasBlock`/`getBlock`/`hashCh`; if scaffolding cost explodes, ship fix-only and flag "recommend live devnet/testnet sync verification" in the commit body and PR.
- [ ] `GOWORK=off go build ./... ` + full suite PASS. Commit: `protocol: fix inverted ancestor guard so binary search runs on forks`

### Task 33 (#62): app.Stop nil guard
- [ ] `app/cli.go` Stop: `if nodeManager == nil { return }` first. Commit: `app: guard Stop against a nil node manager`

### Task 34 (#57): error-string typos
- [ ] pillar/errors.go:10 "time time"→"time"; verifier/errors.go:59 "nor"→"not"; verifier/errors.go:77 "is is"→"is". Grep `vm/embedded/tests` fixtures for all three old strings first; update in same commit. Commit: `pillar,verifier: fix error-string typos`

## Wave 5 — rpc

### Task 35 (#1): FusionEntryList.UnmarshalJSON sizes from aux
- [ ] `rpc/api/embedded/plasma.go:153` `len(r.Fusions)` → `len(aux.Fusions)`. Create `rpc/api/embedded/plasma_test.go`: unmarshal a one-entry list into a fresh receiver (panics before, passes after); round-trip equality. Commit: `rpc: fix FusionEntryList UnmarshalJSON panic on fresh receivers`

### Task 36 (#2): Project.UnmarshalJSON sizes from aux
- [ ] `rpc/api/embedded/accelerator.go:152` `len(p.Phases)` → `len(aux.Phases)`. Test as Task 35 (accelerator_test.go in package). Commit: `rpc: fix Project UnmarshalJSON panic on fresh receivers`

### Task 37 (#3): GetRequiredPoWForAccountBlock checks the frontier error
- [ ] Insert `if err != nil { return nil, err }` between GetFrontierContext and its first use (plasma.go:238-239). Commit: `rpc: check frontier context error in GetRequiredPoWForAccountBlock`

### Task 38 (#4): PublishRawTransaction checks the frontier error
- [ ] ledger.go:60-63: check `err` before the `m == nil` fallback. Commit: `rpc: surface frontier momentum error in PublishRawTransaction`

### Task 39 (#5): addConfirmationInfo checks the frontier error
- [ ] ledger_types.go:346: check err; also reuse the captured `store` at :347/:351. Commit: `rpc: check frontier error in addConfirmationInfo`

### Task 40 (#6): stats uses SplitHostPort
- [ ] Extract `peerHost(addr string) string` helper using `net.SplitHostPort` (fallback: raw addr); use in `p2pPeerToPeer`; drop `strings` import if now unused. Create `rpc/api/stats_test.go`: `[::1]:1234`→`::1`, `1.2.3.4:5678`→`1.2.3.4`, bare string passthrough. Commit: `rpc: parse peer IPs with SplitHostPort to support IPv6`

### Task 41 (#7): sentinel passes the frontier momentum through
- [ ] Refactor `toSentinelInfo(m *nom.Momentum, sentinel ...)`; capture `m` in GetByOwner (:56) and GetAllActive (:71); drop the per-entry re-fetch and swallowed error (exact shape in verification record). Existing sentinel fixtures verify. Commit: `rpc: do not swallow frontier errors in sentinel info conversion`

### Task 42 (#8): accelerator GetAll caps pageSize
- [ ] Standard 3-line `RpcMaxPageSize` guard at top of GetAll. Assert in vm/embedded/tests/accelerator_test.go: `GetAll(0, api.RpcMaxPageSize+1)` → `api.ErrPageSizeParamTooBig`. Commit: `rpc: enforce RpcMaxPageSize in accelerator GetAll`

### Task 43 (#9): stats logger module tag
- [ ] `stats.go:29` `net_api` → `stats_api`. Commit: `rpc: tag stats logger with stats_api`

### Task 44 (#10): OsInfo logs gopsutil failures
- [ ] Replace each `if e == nil` block with `if e != nil { api.log.Error(...) } else { ... }` (behavior-preserving; uses the previously dead logger). Commit: `rpc: log gopsutil failures in OsInfo`

### Task 45 (#11 remainder): unwrap-by-address sorting
- [ ] bridge.go:639-649: move sort out of the `else` (empty toAddress branch currently unsorted), `sort.Slice`→`sort.SliceStable`, drop `[:]`. Test in z_bridge_test.go mirroring the existing SortStability test with ≥12 tied heights (Slice-vs-SliceStable needs ~12+ ties — pdqsort is stable below that). Commit: `rpc: stable, unconditional sort for unwrap requests by address`

### Task 46 (#12+#13, one policy commit): uniform wrap/unwrap page-loop error policy
- [ ] Policy: genuine storage errors (`getToken` err) → `return nil, err`; missing referenced entity (`tokenPair == nil` from CheckNetworkAndPairExist) → `continue` (currently a removed pair permanently bricks GetAllUnwrapTokenRequests); CheckNetworkAndPairExist err → `return nil, err`. Apply across GetAllWrapTokenRequests(:329-340), ByToAddress(:380-391), ByToAddressNetworkClassAndChainId(:428-439), GetAllUnsignedWrapTokenRequests(:464-477), unwrap loops (:602-617, :654-669). Wire-visible both directions — spell out in commit body.
- [ ] Test: register unwrap, RemoveTokenPair, GetAllUnwrapTokenRequests returns remaining entries instead of erroring. Commit: `rpc: consistent error policy in bridge wrap/unwrap listings`

### Task 47 (#14): case-insensitive ToAddress filter
- [ ] `toAddress = strings.ToLower(toAddress)` at top of both ByToAddress endpoints (stored values are lowercased at write: implementation/bridge.go:193). Test: wrap with checksummed EVM address, query with same → non-empty. Commit: `rpc: match checksummed addresses in wrap request filters`

### Task 48 (#15): RpcMaxPageSize guards on bridge listers
- [ ] Standard guard on the six endpoints (:303,:344,:395,:443,:581,:624). Test one endpoint with pageSize+1 → ErrPageSizeParamTooBig. Commit: `rpc: enforce RpcMaxPageSize on bridge wrap/unwrap listings`

### Task 49 (#16): drop always-nil error and dead allocation
- [ ] `getConfirmationsToFinality` returns bare `uint64`; delete the five dead err checks at call sites; drop the dead `make` at :485 (plain deletion is wire-byte-identical — verified). Commit: `rpc: simplify getConfirmationsToFinality and remove dead allocation`

### Task 50 (#17): remove unused API struct fields
- [ ] Remove unused `z`/`cs`/`log` fields + initializers from HtlcApi; same pattern in PlasmaApi, StakeApi, SporkApi, SwapApi (unexported — no importer can break). Keep any field actually used. Commit: `rpc: drop unused fields from embedded API structs`

### Task 51 (#18): remove dead uninstallCh
- [ ] subscribe/api.go: delete `uninstallSize` (:20), field (:66), make (:89), select branch (:178-179). Keep `uninstall()` (used by broadcast). Commit: `rpc/subscribe: remove dead uninstall channel`

### Task 52 (#19): subscribe cannot block after Stop
- [ ] Give `Api` the `stopped` channel; `subscribe` selects on `installCh <- subscription` vs `<-s.stopped` → error "subscribe server is stopped". Test if feasible (shrink installSize via var); else fix + rationale in body. Commit: `rpc/subscribe: fail subscriptions after Stop instead of blocking`

## Wave 6 — ship

### Task 53: full verification + push
- [ ] `GOWORK=off gofmt -l .` clean; `GOWORK=off go build ./...`; `GOWORK=off go test ./...` full PASS; `go vet ./chain/account/mailbox/` clean.
- [ ] Push `bugfix/catalogue-batch-1` to origin. gh cannot create PRs (403) → produce compare URL + paste-ready PR body (summary table: entry → commit; quarantine list; wire-visible changes called out; #60 live-sync recommendation).
