# Dynamic Plasma — Overview & Deployment

## Summary

Dynamic Plasma replaces the fixed plasma pricing model with an adaptive one. Fusion and PoW prices adjust each momentum based on network utilization, similar to how Ethereum's EIP-1559 adjusts base fees. This is gated behind the `DynamicPlasmaSpork` — a hard consensus change once activated.

---

## What Changed

### New Packages

| File | Purpose |
|------|---------|
| `dp/dp.go` | Core dynamic plasma logic: price calculation, base plasma computation, block price comparison |
| `dp/dp_test.go` | Unit tests for price adjustments and block pricing |
| `pillar/content_selector.go` | New momentum content selector that ranks blocks by price and enforces plasma limits |

### Modified Files

| File | Change |
|------|--------|
| `chain/nom/momentum.go` | Added `NextFusionPrice`, `NextWorkPrice` fields (RLP-optional); `DynamicPlasmaMomentumVersion = 2`; hash computation includes price fields when version >= 2 |
| `common/types/spork.go` | Added `DynamicPlasmaSpork` with placeholder hash (see Deployment section for release flow) |
| `pillar/worker_momentum.go` | `generateMomentum` branches on spork: DP path uses `dp.NewDynamicPlasma` + `NewMomentumContentSelector` for content selection and sets price fields on momentum |
| `verifier/momentum.go` | Version validation and content validation branches on `isDynamicPlasmaActive` |
| `vm/vm.go` | `enoughPlasma` uses `AvailablePlasmaV2` and `dp.DifficultyToPlasma` when spork active |
| `vm/plasma.go` | `GetBasePlasmaForAccountBlock` and `AvailablePlasmaV2` for DP-aware plasma calculation |
| `vm/embedded/embedded.go` | `applyDynamicPlasmaDiffs()` adds `SetVariables` method to plasma contract when spork active |
| `vm/embedded/definition/plasma.go` | Added `PlasmaVariables` struct, `SetVariables` ABI, `GetPlasmaVariables`/`Save` methods, default config values |
| `rpc/api/embedded/plasma.go` | `Get`, `GetRequiredPoWForAccountBlock`, `GetVariables` all branch on `IsDynamicPlasmaSporkEnforced()` |
| `vm/vm_context/spork.go` | `IsDynamicPlasmaSporkEnforced()` — wraps `IsSporkActive(DynamicPlasmaSpork)` (the underlying check already requires `Activated && EnforcementHeight <= frontier.Height`) |
| `chain/account_pool.go` | Added `MaxUncommittedBlocksPerAccount = 500` and `CheckUncommittedBlocksCount` — limits the per-account uncommitted chain length to bound node resource consumption (independent of DP, but landed in the same series of commits) |
| `chain/momentum/embedded.go` | Added `GetPlasmaVariables()` on the momentum store, reading the on-chain plasma contract variables |

### Unchanged Files

- `protocol/handler.go` — zero changes
- `protocol/peer.go` — zero changes
- `p2p/` — zero changes
- `consensus/` — zero changes (election/points are unaffected)

---

## How It Works

### Price Mechanism

Each momentum carries two prices — `nextFusionPrice` and `nextWorkPrice` — starting at `MinResourcePrice` (1000). After each momentum, prices adjust based on whether the previous momentum's plasma usage was above or below target:

```
priceChange = currentPrice * (usedPlasma - targetPlasma) / targetPlasma / priceChangeDenominator
nextPrice = currentPrice + priceChange
```

Changes are capped by `maxPriceChangePercent` (default 10%) to prevent wild swings. The minimum price is `MinResourcePrice` (1000).

### Block Pricing

Under dynamic plasma, a block's "effective cost" is no longer just `FusedPlasma` — it's a weighted combination of fused plasma and PoW difficulty, scaled by the current prices:

```
blockValue = FusedPlasma * workPrice + DifficultyToPlasma(Difficulty) * fusionPrice
```

Blocks are ranked by `blockValue / BasePlasma` (higher is better). The content selector (`pillar/content_selector.go`) sorts blocks by this ratio and fills the momentum up to `maxBasePlasmaInMomentum`.

### Minimum Block Price

A block must meet a minimum price to be included:

```
minimumFusedPlasma = AccountBlockBasePlasma * fusionPrice / PriceScaleFactor
```

If a block's fused plasma is below this, it's rejected. This means as prices rise, the minimum QSR cost per transaction increases.

### Transaction Limit Per Momentum

**Pre-DP:** A hard cap of 100 blocks per momentum (`MaxAccountBlocksInMomentum` in `chain/account_pool.go:26`). This applies to the total count of user + contract blocks. Both the producer (`accountPool.filterBlocksToCommit`) and verifier (`verifier/momentum.go:233`) enforce this.

**Post-DP:** The hard block count limit is **removed**. It is replaced by two plasma-based limits:

1. **User blocks** — limited by `maxBasePlasmaInMomentum` (default 4,200,000). Each user block consumes its `BasePlasma` (21,000 for a simple send, more for data-bearing transactions). At the default, this allows ~200 simple sends per momentum — double the pre-DP limit.

2. **Contract blocks** — limited separately by `MaxContractBlocksInMomentum()` which returns `maxBasePlasmaInMomentum / EmbeddedSimplePlasma`. At the default (4,200,000 / 52,500), this allows **80 contract interactions** per momentum. Contract blocks do not consume from the user block plasma budget.

The verifier (`verifier/momentum.go:198-231`) enforces these by iterating blocks and checking `basePlasma.Total() > config.MaxBasePlasmaInMomentum` for user blocks and `contractBlockCount > plasma.MaxContractBlocksInMomentum()` for contract blocks. The old `MaxAccountBlocksInMomentum` check only runs on the pre-DP branch (line 233).

**Can the limit be raised?** Yes — `maxBasePlasmaInMomentum` is already configurable via `SetVariables` on the Plasma contract. Raising it directly increases both the user block and contract block caps. The upper bound is `MaxBasePlasmaInMomentumUpperLimit` (210,000,000,000,000 — effectively 10 billion simple sends per momentum). However, see the trade-offs below.

| `maxBasePlasmaInMomentum` | Max user blocks (simple sends) | Max contract blocks |
|---------------------------|-------------------------------|---------------------|
| 4,200,000 (default)       | 200                           | 80                  |
| 8,400,000                 | 400                           | 160                 |
| 21,000,000                | 1,000                         | 400                 |
| 42,000,000                | 2,000                         | 800                 |

**Trade-offs of raising the limit:**

- **Block production time** — more blocks per momentum means longer processing time for the producing pillar. If it exceeds the momentum interval (10s), momentums will be delayed.
- **State growth** — more transactions per momentum accelerates chain state growth (disk space, sync time).
- **Block propagation** — larger momentums take longer to propagate to peers. On a 3-pillar devnet this is negligible; on mainnet with global nodes it could cause more forks.
- **Content selection overhead** — the content selector sorts all uncommitted blocks by price. With thousands of blocks in the pool, this becomes more expensive (though it's O(n log n), not a practical concern until very high throughput).

**Open question:** The 100-block limit was likely a conservative safety measure. With DP's plasma-based limits providing the same protection against oversized momentums, raising `maxBasePlasmaInMomentum` is safe from a consensus perspective. The question is what throughput the network infrastructure can handle. Testnet soak testing with progressively higher limits would answer this.

### Plasma Calculation Changes

| Concept | Pre-DP | Post-DP |
|---------|--------|---------|
| Blocks per momentum | Hard cap: 100 | Plasma-based: ~200 user + 80 contract (default) |
| Fusion cost per unit | Fixed 2100 plasma | `AccountBlockBasePlasma * fusionPrice / PriceScaleFactor` |
| PoW difficulty per plasma | Fixed 1500 | `1500 * workPrice / PriceScaleFactor` |
| Max fusion units per account | 5000 (fixed) | 100,000,000 (practically unlimited) |
| Max PoW per block | `EmbeddedWDoubleResponse` | Same as max fusion (unlimited) |
| Content selection | FIFO (all uncommitted) | Price-ranked, capped by `maxBasePlasmaInMomentum` |

---

## Configuration

Parameters are stored on-chain in the Plasma contract and adjustable via `SetVariables`. The method enforces `block.Address == types.GovernanceAddress` (see `vm/embedded/implementation/plasma.go:154`). Note that `GovernanceAddress` is currently a hardcoded address in `common/types/address.go:46` with a `// TODO: Update governance address to governance contract` marker — it is **not** the same as `SporkAddress`.

| Parameter | Default | Bounds | Description |
|-----------|---------|--------|-------------|
| `maxBasePlasmaInMomentum` | 4,200,000 | 210,000 – 210,000,000,000,000 | Max base plasma per momentum (default = 200× `AccountBlockBasePlasma`) |
| `fusedPlasmaTarget` | 1,050,000 | ≥ 0 (and `fusedPlasmaTarget + powPlasmaTarget ≤ maxBasePlasmaInMomentum`) | Target fused plasma usage per momentum (default = 25% of max) |
| `powPlasmaTarget` | 1,050,000 | ≥ 0 (same combined constraint as above) | Target PoW plasma usage per momentum (default = 25% of max) |
| `maxPriceChangePercent` | 10 | 1 – 100 | Max price change per adjustment |
| `priceChangeDenominator` | 20 | 1 – 100 | Denominator for price change calculation |

Bounds are enforced in `SetVariablesMethod.ValidateSendBlock` (`vm/embedded/implementation/plasma.go:169-204`).

Defaults are defined in `vm/embedded/definition/plasma.go`. Values take effect after `SetVariables` is called on-chain; if never called, defaults apply.

---

## Testing

### Existing Test Coverage

| File | What it tests |
|------|--------------|
| `dp/dp_test.go` | `NextFusionPrice`, `NextWorkPrice` adjustments; `ComputeBasePlasma` splitting; `HigherPrice` comparison; price clamping and edge cases |
| `vm/embedded/tests/dp_test.go` | End-to-end: `SetVariables` via contract, `GetVariables` round-trip, spork activation gate |
| `vm/embedded/tests/plasma_test.go` | Existing plasma tests (Fuse, CancelFuse) — still pass, unchanged |
| `vm/plasma_test.go` | `AvailablePlasma`, `GetDifficultyForPlasma` — pre-DP tests, still pass |

### What's NOT Tested

- **Price convergence under sustained load** — no automated integration test, but can be validated manually on devnet using the [loadtest tool](#load-testing)
- **Content selector under pressure** — no test for the scenario where blocks compete for limited momentum space with varying prices
- **Cross-spork boundary** — no test for the transition moment when the spork activates mid-chain (version 1 -> 2 momentums)
- **SetVariables parameter bounds** — the `SetVariables` method validates limits but edge case testing is minimal

### How to Test on Devnet

The spork is pre-activated in `docker/devnet/genesis.json`. Start the devnet and query:

```sh
# Check spork is active — version should be 2, prices should be present
curl -s http://localhost:35997 -d '{
  "jsonrpc":"2.0","id":1,
  "method":"ledger.getFrontierMomentum","params":[]
}' | jq '{height: .result.height, version: .result.version, nextFusionPrice: .result.nextFusionPrice, nextWorkPrice: .result.nextWorkPrice}'

# Check plasma config
curl -s http://localhost:35997 -d '{
  "jsonrpc":"2.0","id":1,
  "method":"plasma.getVariables","params":[]
}' | jq

# Check required PoW for a transaction (uses DP pricing)
curl -s http://localhost:35997 -d '{
  "jsonrpc":"2.0","id":1,
  "method":"plasma.getRequiredPoWForAccountBlock",
  "params":[{"address":"z1qq9n7fpaqd8lpcljandzmx4xtku9w4ftwyg0mq","blockType":1,"toAddress":"z1qxemdeddedx0xxxxxxxxxxxxxxxxxxzsg5mv7","data":"b602e311"}]
}' | jq
```

Prices only change under sustained load. On a quiet devnet with 3 pillars and no transactions, they stay at the baseline (1000). To observe price movement, you'd need to send enough transactions to push fused or PoW plasma usage above the targets.

### Load Testing

The [nom-loadtest](https://github.com/digitalSloth/nom-loadtest) tool can be used to generate sustained transaction volume against the devnet to observe dynamic plasma price adjustments.

**Setup:**

```sh
git clone https://github.com/digitalSloth/nom-loadtest
cd nom-loadtest
npm install
npm run build
```

**Running a load test:**

The shell script wrapper defaults to `ws://localhost:35998`, 10 TPS, 60 seconds:

```sh
./run-loadtest.sh
```

Override via environment variables:

```sh
# 50 TPS for 2 minutes
RATE=50 DURATION=120 ./run-loadtest.sh

# Send a fixed number of transactions
TOTAL=1000 RATE=25 ./run-loadtest.sh
```

Or invoke the CLI directly for more control:

```sh
npx tsx src/index.ts \
  --node ws://localhost:35998 \
  --network-id 69 \
  --mnemonic "your twelve word mnemonic ..." \
  --rate 50 \
  --duration 60
```

After a run the tool reports sent/confirmed/failed counts, failure rate, throughput, and latency percentiles (p50, p95, p99). Press `Ctrl+C` to stop early and see partial results.

**Observing price changes:** After a sustained load test, query the frontier momentum to see how prices adjusted:

```sh
curl -s http://localhost:35997 -d '{
  "jsonrpc":"2.0","id":1,
  "method":"ledger.getFrontierMomentum","params":[]
}' | jq '{height: .result.height, nextFusionPrice: .result.nextFusionPrice, nextWorkPrice: .result.nextWorkPrice}'
```

Prices will have moved away from the baseline (1000) if the load pushed fused or PoW plasma usage above the configured targets.

### Updating Plasma Configuration

The on-chain plasma parameters can be updated at runtime via `SetVariables` on the Plasma contract. This requires the governance address mnemonic. The loadtest repo includes a helper script:

```sh
MNEMONIC="your governance mnemonic words here" \
MAX_BASE_PLASMA=8400000 \
FUSED_TARGET=2100000 \
POW_TARGET=2100000 \
MAX_PRICE_CHANGE=10 \
PRICE_DENOMINATOR=20 \
  ./run-plasma-set.sh
```

All five plasma parameters must be set. The script validates them, prints the configuration, waits 3 seconds, then sends the transaction.

| Env var | Contract field | Default | Description |
|---------|---------------|---------|-------------|
| `MAX_BASE_PLASMA` | `maxBasePlasmaInMomentum` | 4,200,000 | Max base plasma per momentum |
| `FUSED_TARGET` | `fusedPlasmaTarget` | 1,050,000 | Target fused plasma usage per momentum |
| `POW_TARGET` | `powPlasmaTarget` | 1,050,000 | Target PoW plasma usage per momentum |
| `MAX_PRICE_CHANGE` | `maxPriceChangePercent` | 10 | Max price change per adjustment (1-100) |
| `PRICE_DENOMINATOR` | `priceChangeDenominator` | 20 | Denominator for price change calculation (1-100) |

Optional: `NODE_URL` (default `ws://localhost:35998`), `NETWORK_ID` (default `69`), `SENDER_INDEX` (default `0`).

To read the current on-chain configuration without making changes:

```sh
./run-plasma-get.sh
```

Or via RPC:

```sh
curl -s http://localhost:35997 -d '{
  "jsonrpc":"2.0","id":1,
  "method":"plasma.getVariables","params":[]
}' | jq
```

---

## Deployment

### Spork Activation Pattern

The `DynamicPlasmaSpork` in `common/types/spork.go` uses a **placeholder hash** (`0000...0002`). This follows the same pattern as the libp2p spork — the binary is safe-by-default because no on-chain spork can match a placeholder. The release flow is:

1. **Ship the binary** with the placeholder. `IsSporkActive` always returns false; the network stays on fixed-price plasma.
2. **Governance broadcasts the CreateSpork tx.** The resulting send-block hash is the real SporkId.
3. **Replace the placeholder** with that hash, release a new binary, coordinate the operator upgrade campaign.
4. **Governance broadcasts ActivateSpork.** After `SporkMinHeightDelay` momentums, `EnforcementHeight` passes and every node enforces dynamic plasma consensus rules.

Tests and devnet override `ImplementedSporksMap` at runtime with the locally-generated hash (see `vm/embedded/tests/dp_test.go` for the existing pattern).

### Devnet

Already deployed. The spork is pre-activated in `docker/devnet/genesis.json` under `SporkConfig.Sporks`:

```json
{
  "id": "0000000000000000000000000000000000000000000000000000000000000001",
  "name": "dynamic-plasma",
  "description": "Activates Dynamic Plasma",
  "activated": true,
  "enforcementHeight": 20
}
```

The devnet genesis hardcodes the real spork ID at genesis time, so the placeholder in `spork.go` does not affect devnet. This is the same approach used by the libp2p spork's devnet deployment.

### Testnet / Mainnet

Once governance creates the spork on-chain and the real SporkId is known:

1. Replace the placeholder in `common/types/spork.go` with the real hash
2. Add the real hash to `ImplementedSporksMap`
3. Release the updated binary
4. Coordinate node operators to upgrade before governance activates the spork

After activation, new momentums will have `version: 2` with `nextFusionPrice` and `nextWorkPrice` fields.

### Risks & Open Questions

| Item | Status | Notes |
|------|--------|-------|
| `DefaultPriceChangeDenominator` = 20 vs documented = 10 | **Mismatch** | The doc in the old version said 10, but `plasma.go` defaults to 20. Confirm the intended value. |
| Price convergence behaviour | **Untested at scale** | No integration tests verify prices converge correctly under realistic load patterns. Recommend testnet soak testing. |
| Max fusion units change (5000 -> 100,000,000) | **Breaking change** | Pre-DP, accounts were limited to 5000 fusion units. Post-DP, the limit is effectively removed. This is intentional but changes the economics significantly. |
| `SetVariables` governance | **GovernanceAddress only** | The `SetVariables` method permission-checks `block.Address == types.GovernanceAddress`. `GovernanceAddress` is a hardcoded placeholder with a TODO to migrate to a governance contract. Ensure this key is properly secured (and the TODO resolved) before mainnet. |
| 100-block limit removed by DP | **Open question** | The pre-DP hard cap of 100 blocks/momentum is replaced by plasma-based limits (~200 user + 80 contract at defaults). The new limit can be raised further via `SetVariables`. Needs testnet soak testing to determine safe throughput ceiling. |

---

## Bugs / Code Smells Spotted During Review

### 1. `pillar/content_selector.go:96` — hash tiebreak is dead code

```go
if err == dp.ErrBlockPriceSame && bytes.Compare(a.Hash.Bytes()[:], b.Hash.Bytes()[:]) > 1 {
    return true
}
```

`bytes.Compare` returns one of `-1, 0, 1`, so `> 1` is **never** true. Rule #5 in the function's own doc comment ("If blocks are of equal priority price-wise then a block hash comparison will determine which block gets higher priority") never fires. On price ties, the sort just preserves input order via `sort.SliceStable`.

Compare with the legacy logic in [`chain/account_pool.go:92`](chain/account_pool.go:92) which uses `> -1`. The intent here is almost certainly `> 0` (or `== 1`).

This is a producer-side bug only — the verifier doesn't re-sort content — so it isn't consensus-breaking, but it makes block selection non-deterministic across pillars when prices tie.

### 2. `dp/dp.go:23-25` — `MaxFusionPlasmaForAccount` uses the wrong multiplier

```go
MaxFusionUnitsPerAccount  = 100000000
MaxFusionPlasmaForAccount = MaxFusionUnitsPerAccount * constants.CostPerFusionUnit   // <-- cost, not plasma
MaxFusedAmountForAccount  = constants.CostPerFusionUnit * MaxFusionUnitsPerAccount
```

`MaxFusionPlasmaForAccount` should be `MaxFusionUnitsPerAccount * constants.PlasmaPerFusionUnit` (= 2.1×10¹¹) by analogy with the legacy [`vm/constants/plasma.go:53`](vm/constants/plasma.go:53). As written, it is identical to `MaxFusedAmountForAccount` (= 10¹⁶), mixing QSR-atomic units with plasma units.

Knock-on effects:

- [`FusedAmountToPlasma`](dp/dp.go:216) returns `MaxFusionPlasmaForAccount` when `amount >= MaxFusedAmountForAccountBig`. At the cap, the returned plasma jumps from `numUnits * PlasmaPerFusionUnit` (~2.1×10¹¹) to 10¹⁶ — a five-order-of-magnitude discontinuity.
- `MaxPoWPlasmaForAccountBlock = MaxFusionPlasmaForAccount`, so the per-block PoW cap and `MaxDifficultyForAccountBlock` (`MaxPoWPlasmaForAccountBlock * 1500` ≈ 1.5×10¹⁹) sit just under `uint64.Max`. Multiplying anywhere downstream is a near-overflow risk that the legacy ~3×10¹⁴ bound did not have.

In practice the cap is never reached on a live network (nobody fuses 100M QSR), so the devnet behaves correctly. But the constants look like a copy/paste slip and should be reviewed before mainnet.

### 3. Minor: `verifier/momentum.go` does not validate `Version >= 2` price floor on activation boundary

`nextFusionPrice()` / `nextWorkPrice()` (lines 175-189) check `Version >= 2 && NextFusionPrice < MinResourcePrice`, but `version()` (lines 119-132) demands `Version == 2` (not `>= 2`) when the spork is active. The pair of checks is consistent today, but if a future spork bumps the version again, the price-floor check would silently apply to version 3+ momentums while `version()` rejects them. Not a bug now, but worth a comment so future-you doesn't trip over it.

---

## Quick Reference

### Key Constants

| Constant | Value | Location |
|----------|-------|----------|
| `MaxAccountBlocksInMomentum` | 100 (pre-DP only) | `chain/account_pool.go:26` |
| `MaxUncommittedBlocksPerAccount` | 500 | `chain/account_pool.go:30` |
| `DynamicPlasmaMomentumVersion` | 2 | `chain/nom/momentum.go:15` |
| `MinResourcePrice` | 1000 | `dp/dp.go:30` |
| `PriceScaleFactor` | 1000 | `dp/dp.go:31` |
| `MaxFusionUnitsPerAccount` (post-DP) | 100,000,000 | `dp/dp.go:23` |
| `PoWDifficultyPerPlasma` | 1500 | `vm/constants/plasma.go:39` |
| `AccountBlockBasePlasma` | 21000 | `vm/constants/plasma.go:28` |
| `EmbeddedSimplePlasma` | 52500 | `vm/constants/plasma.go:31` |
| `MaxBasePlasmaInMomentumUpperLimit` | 210,000,000,000,000 | `vm/embedded/definition/plasma.go:53` |
| `MaxBasePlasmaInMomentumLowerLimit` | 210,000 | `vm/embedded/definition/plasma.go:54` |

### RPC Methods

| Method | Returns |
|--------|---------|
| `ledger.getFrontierMomentum` | Momentum with `nextFusionPrice`, `nextWorkPrice`, `version` |
| `plasma.getVariables` | `maxBasePlasmaInMomentum`, `fusedPlasmaTarget`, `powPlasmaTarget`, `maxPriceChangePercent`, `priceChangeDenominator` |
| `plasma.get` | `currentPlasma`, `maxPlasma`, `qsrAmount` for an address |
| `plasma.getRequiredPoWForAccountBlock` | `availablePlasma`, `basePlasma`, `requiredDifficulty` for a hypothetical transaction |
| `plasma.getEntriesByAddress` | Fusion entries for an address |
