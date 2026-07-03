# Review guide — bug-catalogue batch 1

Companion to `pr-body-catalogue-batch-1.md`. One commit per bug; each
commit message carries the full analysis, the catalogue entry number, and
its red/green test status. This guide orders the commits by review
priority and gives a one-line bug → fix summary for each, so a reviewer
can budget attention instead of reading 59 commits cold.

**Sources on the branch:**
- `docs/superpowers/bugs-found-2026-06-11.md` — the original 62-entry catalogue (comment-review findings).
- `docs/superpowers/triage-2026-07-03.md` — per-entry verdicts against dev @ 70ad9ce (what's real, what's not, what's quarantined).
- `docs/superpowers/plans/2026-07-03-bug-catalogue-fixes.md` — the executed plan.

**Suggested review strategy:** Tier 1 line-by-line; Tier 2 for wire/API
semantics you care about; Tiers 3–5 are mechanical error-propagation and
panic fixes reviewable from the diffs alone; Tier 6 at a skim.

---

## Tier 1 — highest scrutiny (state integrity and sync)

These three change how the node persists or syncs state. Each has a
regression test that fails before the fix.

| Commit | Cat. | Bug → fix |
|---|---|---|
| `c70beb1` | #26 | Momentum insertion swallowed account-block apply errors, so a pillar could build (and sign) on locally corrupt state. Errors now abort the insertion. Test fails before fix. |
| `6565697` | #23 | `ldbManager.Add` "frontier" guard compared the historical view against its own frontier — a tautology — so a transaction not building on the real frontier silently corrupted the store. Now rejected, matching memdb semantics. Found to be *worse* than catalogued during empirical verification. |
| `42eb5e5` | #60 | `findAncestor`'s inverted guard: the binary-search fork fallback never ran on real forks (node re-synced from genesis) and always ran when unnecessary. One-character fix restoring go-ethereum-original semantics; first tests in `protocol/` (`TestFindAncestorLongFork` returns 0 before the fix). **Live devnet/testnet sync check still recommended before release.** |

## Tier 2 — wire-visible RPC behavior changes (deliberate)

All response-shape/semantics changes are called out here; nothing else in
the PR changes what goes over the wire.

| Commit | Cat. | Bug → fix |
|---|---|---|
| `5ef3cf3` + `de7ffd1` | #12 #13 | Bridge listings had inconsistent error policy: wrap loops silently shrank pages on storage errors; unwrap loops hard-failed the whole call when a token pair had been removed (one `RemoveTokenPair` permanently bricked the endpoint). `5ef3cf3` made storage errors loud. **Review follow-up `de7ffd1`:** the first cut's removed-pair skip was dead code (`CheckNetworkAndPairExist` reports a missing pair via `ErrTokenNotFound`/`ErrUnknownNetwork`, never a nil pair) *and* it filtered after pagination. Now both unwrap endpoints pre-filter removed-pair/removed-network requests before `Count`/`GetRange` with a per-pair lookup cache: Count covers only listable unwraps, pages are dense. Regression test fails on the prior commit with 'token not found'. |
| `c6e4ad0` | #11 | `GetAllUnwrapTokenRequestsByToAddress` skipped sorting entirely on the empty-address branch and used unstable sort elsewhere. Now one unconditional `sort.SliceStable` by height desc; a 12-tie fixture proves stability (small fixtures pass under both sorts). Completes what PR #57 started. |
| `c379e17` | #14 | Wrap-request address filters compared raw strings, so checksummed EVM addresses returned empty results. Now normalized before compare; test fails before fix with count=0. |
| `419dfaa`, `75072be` | #8 #15 | Accelerator `GetAll` and bridge wrap/unwrap listings accepted unbounded `pageSize`; now return `ErrPageSizeParamTooBig` above 1024, matching every other paged endpoint. |
| `48570fe`, `98f39f7`, `9c0ab7c`, `799de69` | #4 #5 #3 #7 | Frontier lookup errors were discarded (then the nil context/momentum was dereferenced or silently tolerated) in `PublishRawTransaction`, `addConfirmationInfo`, `GetRequiredPoWForAccountBlock`, and sentinel info conversion. All now propagate; the plasma one was a guaranteed nil-pointer panic in the RPC handler on a failed lookup. |
| `4e7c504` | #6 | Peer IPs parsed with `strings.Split(addr, ":")`, mangling IPv6. Now `net.SplitHostPort`. |
| `a89da3a` | #19 | Subscribing after `Stop` blocked forever on a dead channel; now fails fast with an error. |

## Tier 3 — node robustness (error propagation, panic paths; no wire change)

| Commit | Cat. | Bug → fix |
|---|---|---|
| `5fac765` | #33 | Genesis config validation panics were recovered and reported as *success*; now surface as `ErrInvalidGenesisConfig`. Test fails before fix. |
| `50d43b1` | #29 | Genesis balance check was vacuous when a required address had no genesis block; now fails. Test fails before fix. |
| `afc0a41` | #31 | `GetUnreceivedAccountBlockHashes` underflowed when `atMost == 0` (returned everything). Unreachable via the current production caller, but fixed and tested. |
| `7e4d7d6` | #27 | `ComputePillarDelegations` ignored storage errors mid-iteration. |
| `122d223` | #34 | Auto-receive verified a possibly-nil generated block before checking the generation error — pillar panic path. |
| `83f5af0` | #52 | `GetPillarDelegationsByEpoch` panicked (via `DealWithErr`) instead of returning the `TickMultiplier` error. |
| `e50a5c5` | #20 | Ticker misbehaved for pre-start times and sub-second intervals; now tested. |
| `0db7e35` | #21 | Task-finish/worker-unlock could deadlock if a callback panicked; now panic-safe (recast from the catalogued description during triage). |
| `f139184` | #62 | `app` `Stop` dereferenced a nil node manager if startup failed early. |
| `b99795b` | #56 #61 | One bug, one line: `Lock` left a nil keystore entry behind, so `IsUnlocked` stayed true and `Stop` panicked. |
| `0d8fa54` | #38 | VM context `Done` leaked the account-store snapshot. |
| `bbabb9b` | #36 | Generated blocks were signed twice in `packBlock`; dead second signature removed. |
| `678d2c8` | #35 | `AvailablePlasma` cap compared against a unit-mismatched literal (provably dead branch); now uses the plasma-unit constant. |

## Tier 4 — wallet (first tests in `wallet/`)

| Commit | Cat. | Bug → fix |
|---|---|---|
| `7bb14a6` | #54 | `DeriveForFullPath` returned an empty derivation path. |
| `1dadcdd` | #55 | `FindAddress` ignored the configured `MaxSearchIndex` (hardcoded scan). Test `TestFindAddressHonorsMaxIndex`. |
| `b4f0a04` | #58 | `KeyStore.Zero` didn't actually wipe entropy/seed bytes. `TestZeroWipesSecretBytes` fails before fix. |

## Tier 5 — client-side wire types

The node only marshals these; the panics hit Go *clients* decoding
responses into fresh receivers.

| Commit | Cat. | Bug → fix |
|---|---|---|
| `d5f7a90` | #1 | `FusionEntryList.UnmarshalJSON` sized the slice from the receiver's old length but indexed by the decoded payload → index-out-of-range on any fresh receiver. |
| `4db987a` | #2 | Identical pattern in accelerator `Project.UnmarshalJSON`. |

## Tier 6 — diagnostics, dead code, cosmetics (skimmable)

| Commit | Cat. | Bug → fix |
|---|---|---|
| `51cfd5f` | #40 | Accelerator storage prefixes made explicit constants, pinned 12/13 by an assertion test (guards against silent prefix drift). |
| `854bb3a` | #47 | `common.Big0` was aliased into persisted entries (pillars/stake/swap) — a later in-place mutation would corrupt the shared constant. Copies now. |
| `dcbe84a` | #48 b–d | Latent nil deref + dead code in pillar reward computation (item (a) quarantined). |
| `d78247a` | #41 | `GetSwapAssets` decoded empty values into zero entries; now skipped. Test fails before fix. |
| `bf20ea8` | #37 | ABI error messages had swapped/mis-typed format arguments (garbled diagnostics). Tested. |
| `0436544` | #53 | Go field `ExceptedBlockNum` → `ExpectedBlockNum`; the misspelled JSON tag `exceptedBlockNum` is deliberately kept (wire-compatible). Source-compat: GitHub-wide search finds no external module importer of `consensus/api` — only full forks — so the Go-level rename is not a practical break. Also fixes `Point.LeftAppend` interval notation. |
| `a962234` | #50 (log) | Accelerator voting log said the opposite of what happened (log-only sub-item; the state-machine part is quarantined). |
| `7eeca42`, `9905627`, `fc1a02e`, `88575d0`, `3d854df`, `917da66`, `af7fe3e` | #51 #25 #22 #30 #9 #10 #57 | Diagnostic accuracy: right hash/identifier/reason in error messages, stats logger tag, gopsutil failures logged, error-string typos. |
| `a75da05`, `eeebd98`, `afee2f2`, `2569b4a`, `0509b9a` | #32(1,3) #42(2–4) #17 #18 #16 | Dead code removal only: unused `SetFrontier` methods, vestigial marshal field, unused struct fields, dead uninstall channel, dead allocation. No behavior change. |
| `ab344b0`, `a2adc19` | — | gofmt only. `a2adc19` formats the pre-existing `downloader.go` doc comment (file touched by #60); the five remaining `gofmt -l`-dirty files are inherited from upstream, untouched by this branch, and deliberately left alone. |

## Docs-only commits

`5c82340` (triage + plan), `b9362c1`/`53d22b1`/`7a3d280` (PR body), plus
this guide.

## What is deliberately NOT in this PR

Consensus-sensitive quarantine — anything touching send/block acceptance,
contract state evolution, `ChangesHash`, or historical replay: #24, #39,
#43, #44, #45, #49, #50 (state machine), and sub-items of #32/#42/#48/#53.
These need spork gating / upstream coordination; evidence and repro
sketches are in the triage doc. Two entries were invalidated during
triage (#28, #46 — the void `Save` methods panic internally; nothing is
discarded).

## Verification summary

- `GOWORK=off go test ./...` passes at HEAD (and at every wave boundary
  during development — recorded per commit).
- TDD wherever testable: every commit whose fix is observable has a test,
  and the red/green status ("fails before the fix with X") is recorded in
  the commit message. Packages gaining their first tests: `wallet/`,
  `protocol/downloader/`.
- Golden-state suites (accelerator hashes, consensus reward logs, bridge
  fixtures) pass unchanged wherever behavior was meant to be preserved.
- `gofmt -l` clean for all branch-touched files; `git diff --check` clean;
  golangci-lint reports no issues on changed lines (repo baseline issues
  pre-date the branch).

🤖 Generated with [Claude Code](https://claude.com/claude-code)
