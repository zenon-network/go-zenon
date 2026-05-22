# libp2p Migration — Changes & Rollout Plan

## Summary

The custom devp2p/RLPX networking layer is being replaced with [libp2p](https://libp2p.io/), the ecosystem-standard Go P2P library. To avoid the network partition risks of a hard cut, the transition is **gated behind a spork**: every node ships a binary containing both transport stacks, runs on legacy until the libp2p activation spork's `EnforcementHeight` is reached on its local chain, then atomically swaps to libp2p in-process. No restart required by operators.

This document covers (in order): how to observe the swap end-to-end on the docker devnet, the on-disk architecture, the wire-protocol delta between backends, the runtime configuration, the mainnet rollout plan, known risks, and the remaining hardening work flagged by the PR review.

---

## Observing the Swap on the Devnet

The devnet's `genesis.json` pre-seeds the libp2p activation spork as `activated: true` with `enforcementHeight: 20`, so on a fresh devnet the pillars run on the legacy backend for the first 20 momentums and then atomically swap to libp2p. Here is how to confirm the swap is happening end-to-end.

### Quick start

```bash
make devnet-down            # clean any prior state
make devnet-keys            # regenerate config.json files (keeps existing keys)
make devnet-up              # build & start the 3 pillars + RPC node
```

Wait ~2 minutes for ~20 momentums (~10s per momentum). Then run the checks below against any pillar container.

### Check 1 — Watch docker logs (primary signal)

The switcher emits stdout banners at each lifecycle event in addition to the log file, so `docker logs` is the most direct way to observe the swap. You should see four distinct banners across the run of a fresh devnet pillar:

**At startup (every pillar, once):**
```
===== libp2p =====
Spork not yet active; running on legacy (devp2p/RLPX) backend.
Will swap to libp2p when the activation spork's EnforcementHeight is reached.
```

**At momentum 20 (the activation height), from the chain itself:**
```
===== Congratulations! =====
Just activated spork 'libp2p'
```

**Immediately after (from the switcher, within ~1s):**
```
===== libp2p swap starting =====
Spork EnforcementHeight reached. Tearing down legacy backend and bringing up libp2p.

===== libp2p swap complete =====
Now running on libp2p transport.
```

If the swap fails for any reason (e.g. libp2p host fails to bind the port), the switcher prints to **stderr** instead:

```
===== libp2p swap FAILED =====
Failed to start libp2p backend: <error>
Node has no active network listener. Restart znnd to retry.
```

To follow this live across all containers:

```bash
docker-compose logs -f pillar pillar2 pillar3 rpc | grep -E '====|spork|backend'
```

### Check 2 — The switcher's log file (full record)

For the full per-event log line (with structured key=value fields, timestamps, log level), the p2p layer also writes to `/root/.znn/log/zenon.log` inside the container via `log15`:

```bash
docker exec <pillar-container> cat /root/.znn/log/zenon.log | grep -i 'switcher\|libp2p\|swap\|backend'
```

Expected three-line transcript:

```
... module=p2p msg="libp2p spork not active; starting legacy (devp2p/RLPX) backend"
... module=p2p msg="libp2p spork EnforcementHeight reached; swapping to libp2p backend"
... module=p2p msg="libp2p swap complete"
```

To watch live:

```bash
docker exec -it <pillar-container> tail -f /root/.znn/log/zenon.log | grep -i 'libp2p\|swap\|backend'
```

### Check 3 — UDP listener disappears

The legacy backend uses a UDP listener on port 35995 for Kademlia discovery. libp2p uses TCP only. So:

```bash
docker exec <pillar-container> ss -tunlp | grep 35995
```

- **Pre-swap:** both `tcp` and `udp` rows on `:35995`.
- **Post-swap:** only `tcp` on `:35995`.

The disappearance of the UDP listener is a clean, hard signal that the legacy stack was actually torn down.

### Check 4 — Goroutine fingerprint

libp2p pulls in DHT and yamux goroutines that legacy has no equivalent for. Send SIGQUIT to dump the goroutine stacks; docker captures the dump:

```bash
docker kill -s QUIT <pillar-container>      # then read docker logs
```

In the dump, presence of `yamux` or `kad-dht` stack frames confirms libp2p is running. (Note: SIGQUIT crashes the Go runtime; the container will exit. Use this on a single throwaway pillar, not the whole devnet.)

### Check 5 — Peer ID format via RPC

The peer identity format changes across the swap (same `network-private-key`, different encoding):

- Legacy `NodeID` is 128 hex chars (64-byte uncompressed pubkey).
- libp2p peer ID is a `16Uiu2HAm...`-prefixed base58 multihash.

Query the RPC and look at the `publicKey` field of each connected peer:

```bash
curl -s -X POST -H "Content-Type: application/json" \
  --data '{"jsonrpc":"2.0","method":"stats.networkInfo","id":1}' \
  http://localhost:35997 | jq '.result.peers[].publicKey'
```

- **Pre-swap:** long hex strings like `"c0e8b1c4cffe16c4..."` (128 chars).
- **Post-swap:** short base58 strings like `"16Uiu2HAmRe2RazMP..."`.

### What "good" looks like end-to-end

| Phase | momentum height | docker logs (stdout) | zenon.log (file) | UDP :35995 | Peer IDs |
|---|-----------------|---|---|---|---|
| Startup | 0               | `===== libp2p =====` banner | `starting legacy backend` | present | hex |
| Pre-swap | 1 – 19          | quiet | quiet | present | hex |
| Activation | ~20             | `Just activated spork 'libp2p'` + `===== libp2p swap starting =====` + `===== libp2p swap complete =====` | `EnforcementHeight reached; swapping`, `swap complete` | disappears | (briefly empty during swap, then…) |
| Post-swap | 21 – ∞          | quiet | quiet | absent | base58 |

Momentum production should not stall at the swap. If it does, that's a regression worth flagging.

### Reproducing from scratch

```bash
make devnet-down
make devnet-keys FORCE=1    # regenerate keys AND configs
make devnet-up
# wait ~2 minutes (20 momentums × 10s/momentum)
# run the checks above
```

The `FORCE=1` flag regenerates the `network-private-key` files (and therefore the multiaddr/enode URLs derived from them). Without `FORCE=1`, existing keys are preserved.

---

## Architecture

The `p2p` package is split into four pieces: shared types and interfaces at the root, two parallel backend implementations as subpackages, and a wrapper that owns both backends and switches between them.

```
p2p/
├─ peer.go              # Peer interface — both backend peer types satisfy it
├─ server.go            # Server interface — Start/Stop/Peers/PeerCount/AddPeer/Self
├─ protocol.go          # Protocol struct (Run takes the Peer interface)
├─ message.go           # Msg, MsgReadWriter
├─ peer_error.go        # DiscReason and small exported error helpers
├─ config.go            # Net struct: Seeders (legacy bootstrap) + BootstrapPeers (libp2p bootstrap)
├─ discover/            # NodeID/Node types and the UDP Kademlia implementation (used by the legacy backend)
├─ legacy/              # the devp2p/RLPX backend (active pre-activation)
│  ├─ server.go         # TCP listener, peer lifecycle, dial loop
│  ├─ peer.go           # per-peer connection, ping/pong, protocol dispatch
│  ├─ dial.go           # outbound dial state machine
│  ├─ rlpx.go           # RLPX encryption handshake + framing
│  ├─ metrics.go        # bandwidth meters
│  └─ nat/              # UPnP and NAT-PMP port mapping
├─ libp2p/              # the libp2p backend (active post-activation)
│  ├─ server.go         # libp2p host, DHT, stream handler
│  ├─ peer.go           # wraps a libp2p stream + protoHandshake
│  ├─ stream_rw.go      # [4-byte code][4-byte len][RLP payload] framing over libp2p streams
│  ├─ identity.go       # secp256k1 ECDSA → libp2p crypto.PrivKey
│  └─ bootstrap.go      # parse multiaddrs; tolerates accidental enode:// entries with a warning
└─ switcher/            # the spork-gated wrapper
   ├─ server.go         # owns both backends; polls the oracle; hot-swaps at EnforcementHeight
   └─ oracle.go         # narrow SporkOracle interface (node.go provides the chain-backed adapter)
```

The switcher implements `p2p.Server` (the root interface), so all outside callers (`node`, `rpc`, `protocol`) see a single uniform server type and never reach the active backend directly.

`protocol/handler.go` is transport-agnostic: its `Run(peer p2p.Peer, rw p2p.MsgReadWriter)` callback is invoked by whichever backend is active, and `p2p.Peer` is an interface satisfied by both `legacy.Peer` and `libp2p.Peer`. The handler observes the swap as a mass-disconnect of legacy peers followed by inbound libp2p peer connections — both events it was already designed to tolerate.

### Relative to main

- **`p2p/legacy/`** holds the same devp2p/RLPX code that previously lived at `p2p/` root on main, relocated into a subpackage. The source is unchanged except for the `package` declaration and qualification of shared-type references (`Msg` → `p2p.Msg`, etc).
- **`p2p/legacy/nat/`** is the `p2p/nat/` package moved one directory deeper. `p2p/discover/udp.go` has its `nat` import path retargeted to match.
- **`p2p/libp2p/`** and **`p2p/switcher/`** are entirely new.
- **`p2p/peer.go`** and **`p2p/server.go`** at root are entirely new — they define the `Peer` and `Server` interfaces that the two backends both implement. The files of the same names on main are the ones that moved into `legacy/`.
- **`p2p/protocol.go`**, **`p2p/peer_error.go`**, **`p2p/message.go`**, **`p2p/config.go`** are modified at root: `Protocol.Run` now takes the `Peer` interface; a few internal error/sort helpers were exported so both backend subpackages can use them; `Net` gains a `BootstrapPeers []string` field alongside the existing `Seeders`.
- **`p2p/discover/`** is otherwise unchanged.
- Outside `p2p/`: `node/`, `protocol/`, `rpc/api/stats.go` are updated to use the new interfaces; `common/types/spork.go` gains the `Libp2pSpork` placeholder; `cmd/devnet-keygen` is extended to emit both bootstrap formats; devnet `config.json` and `genesis.json` are updated accordingly.

### Spork-gated activation

The `Libp2pSpork` constant in `common/types/spork.go` identifies the activation spork. The switcher's watcher polls a `SporkOracle` every 1 second; when the oracle reports `IsLibp2pActive() = true` (the activation spork's `Activated && EnforcementHeight ≤ frontier.Height`), the watcher fires `swap()`:

1. The legacy backend is detached from the active reference under the wrapper's mutex, then stopped outside the lock (releasing the TCP listener).
2. The libp2p backend is constructed and started on the now-released port.
3. Active reference is repointed at libp2p; readers (RPC) see the new backend on subsequent calls.

The swap is guarded by `sync.Once`, so concurrent triggers cannot double-swap. If the libp2p host fails to start during the swap, the switcher logs `Crit` and prints a stderr banner; the node stays running (for RPC) but has no network listener. A restart retries libp2p startup directly (the spork is now active, so legacy is never reconstructed).

---

## Wire Protocol — Legacy vs libp2p

The two backends ship in the same binary. Exactly one is active at a time; the switcher's spork oracle decides which.

| Aspect | Legacy (devp2p/RLPX) | libp2p |
|--------|----------------------|--------|
| Transport security | Custom RLPX (ECIES + AES-CTR + HMAC-SHA256) | Noise protocol (XX pattern) |
| Peer identity wire format | 64-byte secp256k1 uncompressed pubkey | libp2p `peer.ID` (multihash of compressed pubkey) |
| Discovery | Custom UDP Kademlia (port 35995/UDP) | `go-libp2p-kad-dht` (over TCP) |
| Message framing | RLPX encrypted frames (32-byte header + MAC) | `[4-byte msg code][4-byte payload length][RLP payload]` (Noise encrypts below) |
| NAT | Custom UPnP + NAT-PMP | `libp2p.NATPortMap()` (same underlying libs) |
| Bootstrap format | `enode://<pubkey>@<ip>:<port>` | `/ip4/<ip>/tcp/<port>/p2p/<peer-id>` |

The two wire protocols share no common bytes — a node on legacy cannot read a libp2p frame and vice versa. Because the swap is chain-deterministic and atomic across all upgraded nodes, this is harmless: every node flips at exactly the same `EnforcementHeight` on its own local chain.

---

## Node Identity

The existing `~/.znn/network-private-key` file (secp256k1 ECDSA) is **reused unchanged across the swap**. Both backends derive their on-wire identity from the same private key:

- **Legacy `NodeID`** = 64-byte raw uncompressed public key (displayed as 128 hex chars).
- **libp2p `peer.ID`** = CIDv1 multihash of the compressed public key (displayed as `16Uiu2HAm…`).

The two representations are deterministic transforms of the same key material, so a given operator's "node identity" is stable across the swap even though its on-wire encoding changes. The `cmd/devnet-keygen` tool computes both formats from each devnet pillar's key file, populating both `Seeders` (enode) and `BootstrapPeers` (multiaddr) in the generated configs.

External systems that store or compare old `NodeID` values verbatim will need updating once the spork activates.

---

## Configuration

### `config.json` — two parallel bootstrap fields

Each operator's `config.json` now carries both formats. The legacy backend reads `Seeders`; the libp2p backend reads `BootstrapPeers`. Both can be populated without conflict; only one is in use at any given moment, decided by the spork.

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

Existing operator configs that only have `Seeders` continue to boot — the libp2p backend falls back to `DefaultBootstrapPeers` (baked into the binary release that ships the real spork hash) when the field is omitted.

`libp2p.ParseBootstrapPeers` tolerates accidentally-pasted `enode://` entries in the `BootstrapPeers` field: each such entry is skipped with a warning rather than failing startup. This is defense-in-depth for the inevitable copy-paste mistake during the migration window.

### `genesis.json` (devnet only)

The devnet genesis pre-seeds the activation spork directly into chain storage, bypassing the governance flow. See [`docker/devnet/genesis.json`](../docker/devnet/genesis.json):

```json
"SporkConfig": {
  "Sporks": [
    ...,
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

The `id` matches the placeholder hash in `common/types/spork.go:Libp2pSpork`. On mainnet, this placeholder is replaced with the real `CreateSpork` transaction hash before the activation binary ships.

### Regenerating devnet configs

```bash
make devnet-down
make devnet-keys FORCE=1    # regenerate keys AND configs (omit FORCE=1 to keep existing keys)
make devnet-up
```

`devnet-keygen` derives both the legacy enode URL and the libp2p multiaddr from each pillar's `network-private-key` and writes them into the matching pillar's `config.json`.

---

## Mainnet Rollout Plan

The rollout is six phases. Phases A and B can run in parallel; everything from C onward is sequential and coordinated by governance.

### Phase A — Binary distribution with placeholder spork

The first binary in the spork-gated series ships with `Libp2pSpork.SporkId` set to a placeholder hash that no on-chain spork can match. Operators upgrade at will; nodes continue running on the legacy backend. No observable behaviour change at the network level — `IsSporkActive` returns false for `Libp2pSpork` and the switcher stays on legacy indefinitely.

This phase is about getting the new code into operators' hands so every node has both transport stacks compiled in before activation is announced.

### Phase B — Bootstrap infrastructure deployment

The operators of the existing 147 legacy bootstrap nodes upgrade their hosts to the new binary and publish their libp2p multiaddrs (derived from the same `network-private-key`, no key churn).

Once collected, the multiaddrs are added to `p2p.DefaultBootstrapPeers` in the source, and a new binary is cut. Operators upgrade to this binary so their `BootstrapPeers` defaults are populated. Until activation, the legacy stack is still in use, so this is a no-op for runtime behaviour — it's pre-positioning the libp2p config for after the swap.

### Phase C — Governance creates the spork

The spork holder broadcasts `CreateSpork("libp2p", "Activates the libp2p networking stack")` from `SporkAddress` or `CommunitySporkAddress`. The send-block hash of that transaction is the real `Libp2pSpork.SporkId`.

The hash is published publicly. There is no behaviour change yet — the spork exists in chain storage but is not `Activated`.

### Phase D — Binary update with real spork hash

The placeholder in `common/types/spork.go:Libp2pSpork` is replaced with the real hash from Phase C. A new binary is cut and operators upgrade.

After this upgrade, every running node knows the real spork's ID. Until the spork is activated, the oracle still returns false and nodes stay on legacy.

This is the most coordination-sensitive phase: any operator who skips it will run a binary that doesn't recognise the activation, and will partition off when the rest of the network swaps. Communication should make clear that **this upgrade is mandatory before the announced activation height**.

### Phase E — Governance activates the spork

The spork holder broadcasts `ActivateSpork(<libp2p-spork-id>)`. The activation transaction's `EnforcementHeight` is set to `activation_momentum.Height + SporkMinHeightDelay` (6 momentums, ~60 seconds).

The activation block height is published publicly at least 2 weeks in advance per the security review's recommendation, so all operators have time to confirm they're on the Phase-D binary.

### Phase F — Network swap

At `EnforcementHeight`, every node's switcher polls its oracle, sees activation, and fires `swap()` within ~1 second. Legacy backends are torn down; libp2p hosts come up on the same TCP port. Existing TCP/UDP listeners on 35995 drop and only TCP returns; peer IDs change from hex to base58.

Momentum production should not stall. Pillars elected for slots N, N+1, N+2… continue to produce on schedule; the few momentums produced during the swap window may briefly fail to propagate but reconcile via normal reorg once peers reconnect via libp2p.

### Phase G — Post-swap monitoring

For the first 24 hours after activation, monitor:

- All nodes report libp2p peer IDs (base58 `16Uiu2HAm…`) via `stats.networkInfo`.
- Momentum production cadence unchanged (~10s/momentum).
- No partitioned forks observable across the bootstrap operators.
- Sync state `SyncDone` returns to true for all nodes after the brief swap-window peer drop.
- Bootstrap-node operator dashboards show inbound libp2p connections.

Once the network is stable, the legacy `Seeders` field can be deprecated in a future binary release; for now it stays in `config.json` as harmless dead config.

---

## Risks & Mitigations

| Risk | Severity | Mitigation |
|------|----------|------------|
| Operator missing the Phase D binary upgrade | **High** | ≥2 weeks public notice on the activation height; communication campaign; pillar-coordination check-ins. The activation transaction itself is sent ~60s before swap (`SporkMinHeightDelay`), but the *binary* deadline is whenever Phase D ships. |
| libp2p bootstrap unavailability immediately post-swap | **High** | Bootstrap operators must be on the Phase D binary first (they're a subset of pillar operators in practice). The legacy bootstrap list is *not* a fallback — it's a different transport. |
| libp2p host fails to start during swap | **Medium** | Switcher logs `Crit`, prints stderr banner, leaves RPC up. Operator restarts znnd; on restart the spork is active so libp2p starts directly. |
| Placeholder spork hash shipped to mainnet | **Low** | The placeholder (`0x...01`) cannot match any real `CreateSpork` send-block hash by cryptographic construction. A node on the placeholder binary will never see activation — it stays on legacy forever, which is the safe default. The risk is operator confusion, not partition. |
| go-ethereum dep conflicts on Go 1.21 bump | **Resolved** | Already handled in this PR; `go mod tidy` is clean. |
| Persistent peer database lost in migration | **Medium** | The legacy `nodeDB` (LevelDB) is not yet replicated for libp2p. After swap, a restarting node has an empty peerstore and must re-bootstrap from `BootstrapPeers`. Listed in the review (Condition 4); planned as a follow-up commit (add `go-ds-leveldb` peerstore). |
| DHT mode mis-tuned for Zenon's network size | **Medium** | `dht.ModeAutoServer` + default IPFS parameters are calibrated for millions of nodes; Zenon has <1000. Planned as a follow-up commit (custom DHT options, disable IPFS content routing). |
| `protocol/handler.go` couples to a backend | **Resolved** | Handler takes `p2p.Peer` interface; both backends satisfy it. Zero protocol-layer changes across the swap. |

---

## Remaining work

These items are tracked from the PR review and will land in a follow-up commit before testnet deployment:

- **Persistent peerstore** for libp2p (go-ds-leveldb backend) — recovers the nodeDB resilience the legacy stack had.
- **DHT tuning** for a small network — replace `ModeAutoServer` with explicit options, disable IPFS content routing, set bucket size / refresh interval appropriate for <1000 nodes, and start the DHT on bootstrap nodes too.
- **Exponential backoff** on bootstrap-redial in `peerMaintenanceLoop` — currently a flat 30s retry; the review (H3) flagged thundering-herd risk after coordinated restarts.
- **Version field check** in the libp2p `protoHandshake` — review M3.
- **Prometheus metrics** for the libp2p backend via `BandwidthReporter` — review M5.

None of these affect the activation correctness; they harden the libp2p stack against operational stress.
