# libp2p Migration — PR Overview & Rollout Plan

## Summary

Replaces the custom devp2p/RLPX networking layer with [libp2p](https://libp2p.io/). The transition is **spork-gated**: every node ships a binary containing both transport stacks, runs on legacy until the libp2p activation spork's `EnforcementHeight` is reached on its local chain, then atomically swaps to libp2p in-process. No restart required by operators.

---

## What Changed

### New packages

| Package | Purpose |
|---------|---------|
| `p2p/libp2p/` | libp2p backend: host, DHT discovery, Noise security, stream handler, identity conversion, bootstrap parsing |
| `p2p/switcher/` | Spork-gated wrapper server: owns both backends, polls the oracle, hot-swaps at EnforcementHeight |
| `p2p/switcher/oracle.go` | Narrow `SporkOracle` interface so the switcher doesn't import the chain package |
| `p2p/switcher/server_test.go` | Lifecycle tests for the switcher (stop-pre-activation, stop-during-swap, nil-oracle, already-active, double-stop) |

### New files at `p2p/` root

| File | Purpose |
|------|---------|
| `p2p/peer.go` | `Peer` interface — both `legacy.Peer` and `libp2p.Peer` satisfy it |
| `p2p/server.go` | `Server` interface — `Start`/`Stop`/`Peers`/`PeerCount`/`AddPeer`/`Self` |
| `p2p/protocol.go` | `Protocol` struct — `Run` now takes the `Peer` interface |
| `p2p/config.go` | `Net` struct gains `BootstrapPeers []string` alongside existing `Seeders` |

### Relocated code

| From | To | Change |
|------|----|--------|
| `p2p/server.go` (old) | `p2p/legacy/server.go` | Package rename + qualified shared-type refs |
| `p2p/peer.go` (old) | `p2p/legacy/peer.go` | Same |
| `p2p/dial.go` | `p2p/legacy/dial.go` | Same |
| `p2p/rlpx.go` | `p2p/legacy/rlpx.go` | Same |
| `p2p/metrics.go` | `p2p/legacy/metrics.go` | Same |
| `p2p/nat/` | `p2p/legacy/nat/` | Same |

`p2p/discover/` and `p2p/message.go`/`p2p/peer_error.go` stay at root — shared by both backends.

### Modified files outside `p2p/`

| File | Change |
|------|--------|
| `node/node.go` | Constructs `switcher.Server` instead of `p2p.Server` directly; adds `sporkOracle` adapter |
| `common/types/spork.go` | Adds `Libp2pSpork` with placeholder ID |
| `cmd/devnet-keygen/` | Derives both enode URLs and multiaddrs from each pillar's key |
| `docker/devnet/genesis.json` | Pre-seeds libp2p activation spork (`activated: true`, `enforcementHeight: 20`) |
| `docker/devnet/config.json` | Both `Seeders` and `BootstrapPeers` populated |

### Architecture

```
p2p/
├─ peer.go, server.go, protocol.go, message.go, peer_error.go   ← shared interfaces
├─ config.go            ← Net struct: Seeders + BootstrapPeers
├─ discover/            ← NodeID/Node types, UDP Kademlia (legacy only)
├─ legacy/              ← devp2p/RLPX backend (pre-activation)
├─ libp2p/              ← libp2p backend (post-activation)
│  └─ server.go         ← DHT, Noise, exponential backoff, peer maintenance
└─ switcher/            ← spork-gated wrapper
   ├─ server.go         ← polls oracle, hot-swaps at EnforcementHeight
   ├─ oracle.go         ← SporkOracle interface
   └─ server_test.go    ← lifecycle tests
```

The switcher implements `p2p.Server`, so `node.go`, `rpc/`, and `protocol/` see a single server type and never reach the active backend directly. `protocol/handler.go` is transport-agnostic — it takes `p2p.Peer` and `p2p.MsgReadWriter`, both satisfied by either backend.

### Node identity

The existing `~/.znn/network-private-key` (secp256k1 ECDSA) is **reused unchanged**. Both backends derive their identity from the same key:

- **Legacy `NodeID`** = 64-byte raw uncompressed pubkey (128 hex chars)
- **libp2p `peer.ID`** = CIDv1 multihash of compressed pubkey (`16Uiu2HAm…`)

External systems that store or compare old `NodeID` values will need updating after activation.

---

## How to Test

### Devnet end-to-end

The devnet's `genesis.json` pre-seeds the libp2p activation spork with `enforcementHeight: 20`. On a fresh devnet, pillars run legacy for ~20 momentums then atomically swap.

```bash
make devnet-down            # clean any prior state
make devnet-keys            # regenerate config files (keeps existing keys)
make devnet-up              # build & start 3 pillars + RPC node
```

Wait ~2 minutes (~20 momentums at 10s each), then verify:

**1. Docker logs — primary signal:**

```bash
docker-compose logs -f pillar pillar2 pillar3 rpc | grep -E '====|spork|backend'
```

You should see four banners across the run:

```
# At startup (every pillar):
===== libp2p =====
Spork not yet active; running on legacy (devp2p/RLPX) backend.
Will swap to libp2p when the activation spork's EnforcementHeight is reached.

# At momentum ~20 (from the chain):
===== Congratulations! =====
Just activated spork 'libp2p'

# Immediately after (from the switcher):
===== libp2p swap starting =====
Spork EnforcementHeight reached. Tearing down legacy backend and bringing up libp2p.

===== libp2p swap complete =====
Now running on libp2p transport.
```

If the swap fails, the node prints to stderr:
```
===== libp2p swap FAILED =====
Failed to start libp2p backend: <error>
Node has no active network listener. Restart znnd to retry.
```

**2. Log file (full record):**

```bash
docker exec <container> cat /root/.znn/log/zenon.log | grep -i 'switcher\|libp2p\|swap\|backend'
```

Expected: `starting legacy backend` → `EnforcementHeight reached; swapping` → `swap complete`

**3. UDP listener disappears:**

```bash
docker exec <container> ss -tunlp | grep 35995
```

Pre-swap: both `tcp` and `udp` rows. Post-swap: only `tcp` (libp2p uses TCP only).

**4. Peer ID format via RPC:**

```bash
curl -s -X POST -H "Content-Type: application/json" \
  --data '{"jsonrpc":"2.0","method":"stats.networkInfo","id":1}' \
  http://localhost:35997 | jq '.result.peers[].publicKey'
```

Pre-swap: 128-char hex strings. Post-swap: base58 strings like `16Uiu2HAm…`.

**5. Goroutine fingerprint (destructive — crashes the container):**

```bash
docker kill -s QUIT <container>
```

Presence of `yamux` or `kad-dht` stack frames in the dump confirms libp2p is running.

### What "good" looks like

| Phase | Height | Docker logs | UDP :35995 | Peer IDs |
|-------|--------|-------------|------------|----------|
| Startup | 0 | `libp2p =====` banner | present | hex |
| Pre-swap | 1–19 | quiet | present | hex |
| Activation | ~20 | swap banners | disappears | (briefly empty) |
| Post-swap | 21+ | quiet | absent | base58 |

Momentum production should not stall at the swap.

### Reproducing from scratch

```bash
make devnet-down
make devnet-keys FORCE=1    # regenerate keys AND configs
make devnet-up
# wait ~2 minutes, then run the checks above
```

### Unit tests

```bash
go test ./p2p/switcher/      # switcher lifecycle (5 tests)
go test ./p2p/libp2p/        # identity, stream_rw, bootstrap parsing
go test ./p2p/legacy/nat/    # UPnP/NAT-PMP
```

---

## Configuration

### `config.json`

Each operator's `config.json` carries both bootstrap formats. The legacy backend reads `Seeders`; the libp2p backend reads `BootstrapPeers`. Only one is in use at any given moment, decided by the spork.

```json
"Net": {
  "ListenHost": "0.0.0.0",
  "ListenPort": 35995,
  "MinPeers": 8,
  "MinConnectedPeers": 16,
  "MaxPeers": 60,
  "MaxPendingPeers": 10,

  "Seeders": [
    "enode://c0e8b1c4...@1.2.3.4:35995"
  ],
  "BootstrapPeers": [
    "/ip4/1.2.3.4/tcp/35995/p2p/16Uiu2HAm..."
  ]
}
```

Existing operator configs that only have `Seeders` continue to boot — the libp2p backend falls back to `DefaultBootstrapPeers` when the field is omitted. `libp2p.ParseBootstrapPeers` tolerates accidentally-pasted `enode://` entries by skipping them with a warning.

### `genesis.json` (devnet only)

The devnet genesis pre-seeds the activation spork:

```json
"SporkConfig": {
  "Sporks": [
    {
      "id": "0000000000000000000000000000000000000000000000000000000000000001",
      "name": "libp2p",
      "description": "Activates the libp2p networking stack",
      "activated": true,
      "enforcementHeight": 20
    }
  ]
}
```

The `id` matches the placeholder in `common/types/spork.go:Libp2pSpork`. On mainnet, this placeholder is replaced with the real `CreateSpork` transaction hash.

---

## Mainnet Rollout Plan

Six phases. A and B can run in parallel; C onward is sequential and coordinated by governance.

### Phase A — Binary distribution with placeholder spork

The first binary ships with `Libp2pSpork.SporkId` set to a placeholder hash that no on-chain spork can match. Operators upgrade at will; nodes continue on legacy. No observable behaviour change — `IsSporkActive` returns false and the switcher stays on legacy indefinitely.

Goal: get the new code into operators' hands so every node has both transport stacks compiled in before activation is announced.

### Phase B — Bootstrap infrastructure deployment

Existing legacy bootstrap node operators upgrade to the new binary and publish their libp2p multiaddrs (derived from the same `network-private-key`). Once collected, the multiaddrs are added to `p2p.DefaultBootstrapPeers` in the source and a new binary is cut. Operators upgrade so their `BootstrapPeers` defaults are populated. No runtime behaviour change yet — this pre-positions the libp2p config for after the swap.

### Phase C — Governance creates the spork

The spork holder broadcasts `CreateSpork("libp2p", "Activates the libp2p networking stack")`. The send-block hash is the real `Libp2pSpork.SporkId`. Published publicly; no behaviour change yet.

### Phase D — Binary update with real spork hash

The placeholder in `common/types/spork.go:Libp2pSpork` is replaced with the real hash from Phase C. Operators upgrade. After this, every running node knows the real spork's ID. Until activation, the oracle returns false and nodes stay on legacy.

**This is the most coordination-sensitive phase.** Any operator who skips it will run a binary that doesn't recognise the activation and will partition off when the network swaps. Communication must make clear this upgrade is mandatory before the announced activation height. At least 2 weeks public notice recommended.

### Phase E — Governance activates the spork

The spork holder broadcasts `ActivateSpork(<libp2p-spork-id>)`. The activation's `EnforcementHeight` is set to `activation_momentum.Height + SporkMinHeightDelay` (~60s). The activation height is published publicly at least 2 weeks in advance.

### Phase F — Network swap

At `EnforcementHeight`, every node's switcher polls its oracle, sees activation, and fires `swap()` within ~1s. Legacy backends are torn down; libp2p hosts come up on the same TCP port. Momentum production should not stall — the few momentums produced during the swap window may briefly fail to propagate but reconcile via normal reorg once peers reconnect.

### Phase G — Post-swap monitoring

For the first 24 hours, monitor:

- All nodes report libp2p peer IDs (base58 `16Uiu2HAm…`) via `stats.networkInfo`
- Momentum cadence unchanged (~10s/momentum)
- No partitioned forks across bootstrap operators
- Bootstrap-node dashboards show inbound libp2p connections

---

## Risks & Mitigations

| Risk | Severity | Mitigation |
|------|----------|------------|
| Operator misses Phase D binary upgrade | **High** | ≥2 weeks public notice; pillar-coordination check-ins. A node on the placeholder binary stays on legacy forever — safe default but will partition off. |
| libp2p bootstrap unavailability post-swap | **High** | Bootstrap operators upgrade first (they're a subset of pillar operators). Legacy bootstrap list is not a fallback — different transport. |
| libp2p host fails to start during swap | **Medium** | Switcher logs `Crit`, prints stderr banner, leaves RPC up. Operator restarts znnd; on restart the spork is active so libp2p starts directly. |
| Persistent peerstore not yet implemented | **Medium** | After restart, node has empty peerstore and must re-bootstrap. Functional but brief connectivity gap. Planned as follow-up (`go-ds-leveldb`). |
| Placeholder spork hash on mainnet | **Low** | Placeholder (`0x...01`) can't match any real `CreateSpork` hash. Node stays on legacy forever — safe default. Risk is operator confusion, not partition. |

---

## Status of Review Items

| Item | Status | Notes |
|------|--------|-------|
| Spork-gated activation | **Done** | `p2p/switcher/` — polls oracle, hot-swaps at EnforcementHeight |
| DHT tuning for small network | **Done** | `ModeServer`, `/znn` prefix, `DisableProviders`/`DisableValues`, `BucketSize(16)`, `RefreshPeriod(1min)`, unconditional start |
| Exponential backoff on bootstrap redial | **Done** | 5s base, 5m cap, ±30% jitter, reset on success. `p2p/libp2p/server.go:56-64` |
| Switcher concurrency bugs | **Done** | Fixed: captured `stopCh` in watcher, nil-check before swap mutations, nil-oracle guard |
| Switcher tests | **Done** | 5 lifecycle tests in `p2p/switcher/server_test.go` |
| Persistent peerstore (go-ds-leveldb) | **Pending** | Planned as follow-up. Not blocking correctness. |

---

## Wire Protocol Comparison

| Aspect | Legacy (devp2p/RLPX) | libp2p |
|--------|----------------------|--------|
| Transport security | Custom RLPX (ECIES + AES-CTR + HMAC-SHA256) | Noise protocol (XX pattern) |
| Peer identity | 64-byte secp256k1 uncompressed pubkey | libp2p `peer.ID` (multihash of compressed pubkey) |
| Discovery | Custom UDP Kademlia (port 35995/UDP) | `go-libp2p-kad-dht` (over TCP) |
| Message framing | RLPX encrypted frames (32-byte header + MAC) | `[4-byte code][4-byte len][RLP payload]` (Noise encrypts below) |
| NAT | Custom UPnP + NAT-PMP | `libp2p.NATPortMap()` |
| Bootstrap format | `enode://<pubkey>@<ip>:<port>` | `/ip4/<ip>/tcp/<port>/p2p/<peer-id>` |

The two protocols share no common bytes. Because the swap is chain-deterministic and atomic across all upgraded nodes, every node flips at exactly the same `EnforcementHeight`.
