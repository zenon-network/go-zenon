# libp2p Migration — PR Overview & Rollout Plan

Verified against `dev` b5f93c2 (2026-10-17). Operators: the part you need during the activation window is [Operating the activation window](#operating-the-activation-window); the devnet check that proves a fresh node can still join afterwards is the [late-join regression runbook](#late-join-regression-runbook-devnet).

## Summary

Replaces the custom devp2p/RLPX networking layer with [libp2p](https://libp2p.io/). The transition is **spork-gated**: every node ships a binary containing both transport stacks, runs on legacy until the libp2p activation spork's `EnforcementHeight` is reached on its local chain, then atomically swaps to libp2p in-process. No restart required by operators.

The switcher decides from the node's **local** chain, which is what a node that has been offline, or that starts from an empty database, does not have yet. `Net.P2PBackend` (#121) lets an operator start libp2p directly in that case; see [Operating the activation window](#operating-the-activation-window).

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

**1. Log file — primary signal:**

Switcher and libp2p diagnostics are structured `P2PLogger` records written to the log file, not to the container's stdout/stderr — `docker-compose logs` will not show them.

```bash
docker exec <container> tail -f /root/.znn/log/zenon.log | grep -i 'switcher\|libp2p\|swap\|backend'
```

You should see three log lines in the file across the run:

```
# At startup (every pillar):
msg="libp2p spork not active; starting legacy (devp2p/RLPX) backend"

# Immediately after momentum ~20 (from the switcher):
msg="libp2p spork EnforcementHeight reached; swapping to libp2p backend"

msg="libp2p swap complete"
```

Separately, on the container's stdout (from the chain, not the log file), you should see the spork activation announcement around the same time:

```bash
docker-compose logs <container> | grep -A1 'Congratulations'
```

```
===== Congratulations! =====
Just activated spork 'libp2p'
```

If the swap fails, the switcher logs at `Crit` level, which also lands in the error log:

```bash
docker exec <container> cat /root/.znn/log/error/zenon.error.log | grep -i swap
```

```
lvl=crit msg="failed to start libp2p backend during swap; node has no active network listener; will retry" err=<error> attempt=<n> next-retry=<duration>
```

The node keeps retrying with backoff; RPC stays up throughout.

**2. UDP listener disappears:**

```bash
docker exec <container> ss -tunlp | grep 35995
```

Pre-swap: both `tcp` and `udp` rows. Post-swap: only `tcp` (libp2p uses TCP only).

**3. Peer count via RPC:**

```bash
curl -s -X POST -H "Content-Type: application/json" \
  --data '{"jsonrpc":"2.0","method":"stats.networkInfo","id":1}' \
  http://localhost:35997 | jq '.result.numPeers'
```

Peers should drop to zero at the swap and come back within seconds. `publicKey` in this response is the hex `NodeID` for both backends (it is derived from the same key), so it does not tell you which transport is in use; the log file and the UDP listener do.

**4. Goroutine fingerprint (destructive — crashes the container):**

```bash
docker kill -s QUIT <container>
```

Presence of `yamux` or `kad-dht` stack frames in the dump confirms libp2p is running.

### What "good" looks like

| Phase | Height | zenon.log | UDP :35995 | `numPeers` |
|-------|--------|-----------|------------|------------|
| Startup | 1 | `starting legacy ... backend` | present | rising |
| Pre-swap | 1–19 | quiet | present | steady |
| Activation | ~20 | `EnforcementHeight reached` → `swap complete` | disappears | briefly 0 |
| Post-swap | 21+ | quiet | absent | recovers |

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
go test ./p2p/switcher/      # switcher lifecycle and P2PBackend override paths
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
  "P2PBackend": "auto",
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

### `Net.P2PBackend`

| Value | What starts | Startup log line (`zenon.log`, `module=p2p`) | Use it when |
|---|---|---|---|
| `auto` (default, same as omitted) | Legacy if the local chain does not show the spork enforced, then the watcher swaps to libp2p at `EnforcementHeight`; libp2p directly if the local chain already shows it enforced | `libp2p spork not active; starting legacy (devp2p/RLPX) backend` or `libp2p spork already active on local chain; starting libp2p backend directly` | A node whose database already holds the chain past activation, or any node before activation is scheduled |
| `libp2p` | libp2p directly; the oracle is not consulted and the activation watcher does not run | `P2PBackend=libp2p; starting libp2p backend directly (skipping spork oracle)` | Any node that starts from an empty or pre-activation database **after the network has passed `EnforcementHeight`** (see below). Not before: until then nobody serves libp2p |
| `legacy` | Legacy, never swaps | `P2PBackend=legacy; starting legacy backend (will not swap to libp2p)` at **Warn** | Networks where the libp2p spork has not been scheduled. Never on a network that has activated: the node drops off at `EnforcementHeight` and nothing else tells you |

Any other value fails `Start` with `switcher: invalid P2PBackend` before either backend opens a listener. `P2PBackend: libp2p` on a node whose chain is already past activation is harmless: it is what `auto` would have done.

### Persistent peerstore

The libp2p backend remembers peers across restarts via a LevelDB-backed peerstore at `<DataPath>/network/libp2p-peerstore/`. On every successful Zenon handshake, it records the peer's dialable multiaddrs, a last-seen timestamp, and a dial-failure count to disk (entries expire after 30 days unseen). On the next startup, a "warm bootstrap" path dials up to `MinConnectedPeers` of those known peers in parallel with the configured `BootstrapPeers` — so a node that has been online before can rejoin the network even if every bootstrap entry has since rotated. This is the primary safeguard against the failure mode that left the legacy network with 147 stale seeders: bootstrap nodes are now a first-time-setup dependency, not a continuous-availability dependency.

Disk usage is on the order of ~1MB for a fully populated peerstore (~1000 peers). The directory is created automatically on first start.

Operators normally don't need to configure this. The default path can be overridden via an optional `Net.PeerstoreDir` field in `config.json`. Omitting the field uses the default path; explicitly setting it to `""` disables peer persistence — every restart becomes a cold start (not recommended in production).

The peerstore is independent from the legacy `nodeDb` (at `<DataPath>/network/nodes/`); migration between formats is not attempted because the peer-ID encoding and dial endpoints differ between backends.

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

## Operating the activation window

### The problem this solves

The oracle the switcher consults is the node's own chain: `IsSporkActive` is false at height 1 and stays false until the local frontier reaches the spork's `EnforcementHeight`. In `auto` mode a node that starts below that height starts **legacy** and dials `Seeders`. After the network has swapped, nobody serves legacy any more, so such a node finds no peers, cannot download the history that would make its oracle true, and never starts libp2p. Its `BootstrapPeers` are never dialled. It sits at height 1 with zero peers for as long as you leave it (`stats.syncInfo` state `3`, not-enough-peers). This is the late-join deadlock from #105; #121 added the override, and the libp2p-only binary planned for the activation release removes the legacy path altogether.

The persistent peerstore does not help here: it only holds peers the node has already handshaken with over libp2p, and a node that never started libp2p has none.

### The rule

**Once the network has passed `EnforcementHeight`, set `"P2PBackend": "libp2p"` before any start from a clean or pre-activation data directory.** That covers a new node, a resync from scratch, and a restore from a backup taken before activation. Not before that height: a node pinned to `libp2p` while the network is still on legacy has nobody to talk to, so a node set up during the notice period runs `auto`, syncs over legacy and swaps on its own when its chain reaches the height, **provided it gets there before the network does**. Watch any node started during the notice period until it is past `EnforcementHeight`: a node still syncing below the height when the rest of the network swaps loses its legacy peers, its oracle stays false, and it is stranded exactly like a fresh node. The announcement is the moment to prepare, not to switch: check `BootstrapPeers` is populated on every node, have the one-line config change ready, and do not start a resync in the last days before the height unless it will finish in time.

**If the network has passed `EnforcementHeight` and a node on `auto` is still below it** (stuck at any height with `stats.syncInfo` state `3` and no peers, or simply not yet caught up): stop it, set `"P2PBackend": "libp2p"`, start it again, and verify as below. Its chain state is fine; only the transport choice was wrong. This applies equally to a node that was down for the swap and comes back from a pre-activation database.

A resync from scratch means removing the chain state as a whole: `<DataPath>/nom`, `<DataPath>/cache` and `<DataPath>/consensus` together. Removing `nom` alone leaves a cache the node then tries to roll back to the empty chain, which it refuses once the gap is more than 100 momentums (`can't rollback cache ... outside the rollback cache`); the node stops before the networking backend even starts. `<DataPath>/network-private-key` (node identity), `<DataPath>/network/` (legacy node table, libp2p peerstore) and `<DataPath>/wallet/` are not chain state and stay.

`Seeders` can stay in the config; in `libp2p` mode they are parsed at startup but never dialled, so a malformed entry still aborts the start while a stale one is harmless. Make sure `BootstrapPeers` is populated, or that the binary's `DefaultBootstrapPeers` are, otherwise libp2p starts with nothing to dial and logs at `Crit`:

```
no libp2p bootstrap peers configured and no remembered peers; node is isolated until inbound peers arrive or Net.BootstrapPeers is populated
```

Nodes that were online through activation need nothing: on the next restart their oracle is already true and `auto` starts libp2p directly, with the peerstore's warm bootstrap (`warm-bootstrap from peer database` in the log) on top of `BootstrapPeers`.

### Verifying a node started the right way

```bash
# which backend started, and why
grep -i 'backend\|P2PBackend\|swap' <DataPath>/log/zenon.log | tail

# is it syncing (state 1), done (state 2) or stuck without peers (state 3)
curl -s -X POST -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","method":"stats.syncInfo","id":1}' http://localhost:35997

# how many peers it has
curl -s -X POST -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","method":"stats.networkInfo","id":1}' http://localhost:35997 | jq '.result.numPeers'
```

The transport is told by the log line, not by the RPC: `publicKey` in `stats.networkInfo` is the hex `NodeID` on both backends. `state` `2` means `currentHeight` has reached `targetHeight` with at least `MinPeers` connected, and it is the state the pillar needs before it will produce. `<DataPath>` is `~/.znn` on Linux and `~/Library/znn` on macOS unless `DataPath` is set in `config.json`. A node started with `P2PBackend: libp2p` never logs `swapping to libp2p backend` when it later crosses `EnforcementHeight`; the watcher is not running, and that is expected.

### Recovery: a pillar that loses its database after activation

The failure that matters most is a producing pillar whose chain state is gone (disk failure, a wipe to recover from corruption, a host rebuild) on a network that has already swapped. What makes it that pillar is its producer key and the `Producer` block in `config.json` that points at it; the chain state is replaceable. Steps:

1. Stop `znnd`. If a node with the same identity is still running somewhere, stop it too: two nodes with one `network-private-key` fight over the same peer ID.
2. Keep, or restore from backup, three things: `config.json` as a whole, in particular its `Producer` block (`Address`, `Index`, `KeyFilePath`, `Password`) and `WalletPath` if it was customised; the wallet directory it points at (`<DataPath>/wallet/` by default), which holds the producer key file; and `<DataPath>/network-private-key`, the node identity both the legacy `NodeID` and the libp2p peer ID derive from. A node that starts without its `Producer` block starts cleanly as a non-producing node; the immediate sign is `No Pillar configured for current node` on stdout at startup, which is easy to miss, and the pillar manager later logs `pillar has no producer address defined`. If the peerstore at `<DataPath>/network/libp2p-peerstore/` survived, keep it; it shortens the time to the first peers but is not required. Treat that backup set as the pillar itself: `config.json` carries the wallet password, so config plus wallet is enough to produce as the pillar, and `network-private-key` is the raw node identity. Keep both under the same protection and file permissions as the live node.
3. Remove whatever is left of `<DataPath>/nom`, `<DataPath>/cache` and `<DataPath>/consensus`, all three, as described under the rule above.
4. In `config.json`, set `"P2PBackend": "libp2p"` and check `BootstrapPeers` is populated.
5. Start `znnd`. Expect `P2PBackend=libp2p; starting libp2p backend directly` in the log, then `stats.syncInfo` state `1` with `currentHeight` climbing.
6. Production resumes only when `stats.syncInfo` reaches state `2`: the pillar manager refuses every producer slot before that, even when the heights look caught up. Confirm it with the pillar's own momentums: `ledger.getFrontierMomentum` shows the pillar's producing address in `producer` at its slots. No on-chain action is needed; the producing address and the key behind it have not changed.
7. Optional: once the node is past `EnforcementHeight`, `P2PBackend` can go back to `auto`. There is no reason to hurry; `libp2p` keeps working.

### Known gap

A node on `auto` that was already stranded, and a node pinned to `legacy`, are not told that the network has moved on: the switcher's warning for `legacy` is printed once at startup, and a stranded node's oracle never reaches the activation height. Watch for `stats.syncInfo` state `3` or `numPeers` `0` in `stats.networkInfo` that does not recover within a few minutes of starting; on a network that has activated, that is this condition until proven otherwise, and the fix is the one above: stop, set `"P2PBackend": "libp2p"`, start again. A log-only watcher for the `legacy` case is a candidate follow-up to #121, not part of Phase 1.

---

## Late-join regression runbook (devnet)

This is the check that closes item 1 of #105: a fresh node, with nothing but libp2p bootstrap addresses, joins a devnet that has already swapped and syncs from genesis. It is meant to be run against the release candidate that will carry the activation, and again whenever `p2p/switcher/` or the libp2p bootstrap path changes. It uses the devnet's `syncnode` service (see `docker/devnet/README.md`, "Sync node"), whose stock config has a single legacy seeder and a single libp2p bootstrap peer, both pointing at pillar 1.

**Setup.** Start from a wiped devnet (`devnet-down` keeps the pillar volumes and their old logs, which would make the checks below ambiguous), bring it past activation and confirm every pillar swapped. Pillar 1's RPC is on host port `35991`; height 20 takes a little over three minutes from genesis at one momentum per 10 s.

```bash
make devnet-down-wipe && make devnet-up
# poll pillar 1 until the frontier is past 20
curl -s -X POST -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","method":"ledger.getFrontierMomentum","id":1}' http://localhost:35991 | jq '.result.height'
# then, on each pillar, the swap must have happened once in this run
for c in znnd-devnet-pillar znnd-devnet-pillar2 znnd-devnet-pillar3; do
  docker exec $c grep -c 'libp2p swap complete' /root/.znn/log/zenon.log   # expect 1
done
```

**Step 1, the failing control (`auto`).** With the stock sync-node config, the late joiner must strand; this proves the run is actually testing the post-swap condition and that the legacy seeder is not quietly serving it.

```bash
make devnet-sync-reset && make devnet-sync-up
sleep 60
make devnet-sync-status
docker exec znnd-devnet-syncnode grep -i 'backend' /root/.znn/log/zenon.log
```

Expected: `starting legacy (devp2p/RLPX) backend` in the log, `stats.syncInfo` with `currentHeight` still `1` and `state` `3`, and no further progress. If the control syncs, the pillars had not swapped yet, or something still serves legacy; fix that before going on.

**Step 2, the fix (`libp2p`).** Edit `docker/devnet/syncnode/config.json`: add `"P2PBackend": "libp2p"` under `Net` and set `"Seeders": []` so the node has no legacy path at all. The config is baked into the devnet image and copied into the container on its first start, so rebuild the image, then recreate the sync node from a clean slate:

```bash
docker compose build pillar                                   # rebuilds go-zenon-devnet:latest; nothing is restarted
make devnet-sync-reset
docker compose --profile sync up -d --no-deps syncnode        # --no-deps: do not let Compose recreate pillar 1 on the new image
make devnet-sync-logs                                         # Ctrl-C once blocks are flowing
make devnet-sync-status
```

Expected, in order:

| Check | Pass |
|---|---|
| `docker exec znnd-devnet-syncnode grep -c 'P2PBackend=libp2p; starting libp2p backend directly' /root/.znn/log/zenon.log` | `1` |
| `docker exec znnd-devnet-syncnode grep -c 'starting legacy' /root/.znn/log/zenon.log` | `0` |
| `stats.syncInfo` on `:35999` | `state` `1` and `currentHeight` rising, then `state` `2` (`currentHeight` has reached `targetHeight` with enough peers) |
| `docker exec znnd-devnet-syncnode grep -c 'swapping to libp2p backend' /root/.znn/log/zenon.log` after `currentHeight` passes 20 | `0` (the watcher is not running; no restart of the backend at `EnforcementHeight`) |
| `stats.networkInfo` on `:35999`, `.result.numPeers` | at least 1 |
| `ledger.getMomentumsByHeight` with params `[20, 1]` on `:35999` (sync node) and `:35991` (pillar 1) | same `.result.list[0].hash` |

The three RPC checks as fail-closed commands (each line exits non-zero on a miss, including an RPC error, an unreachable endpoint, or a missing, null or empty hash):

```bash
rpc()  { curl -sf -X POST -H 'Content-Type: application/json' --data "{\"jsonrpc\":\"2.0\",\"method\":\"$2\",\"params\":$3,\"id\":1}" "http://localhost:$1"; }
hash() { rpc "$1" ledger.getMomentumsByHeight '[20, 1]' | jq -er '.result.list[0].hash | select(type == "string" and length > 0)'; }
rpc 35999 stats.syncInfo '[]'    | jq -e '.result.state == 2'
rpc 35999 stats.networkInfo '[]' | jq -e '.result.numPeers >= 1'
a=$(hash 35999) && b=$(hash 35991) && [ "$a" = "$b" ]
```

The log-line checks in the table (`starting libp2p backend directly` present once, `starting legacy` and `swapping to libp2p backend` absent) stay as written; `grep -c` prints the count and exits non-zero when it is zero, so read the number, not the exit status, for the two that must be `0`.

Restore `docker/devnet/syncnode/config.json` afterwards (undo the two edits by hand, or `git checkout -- docker/devnet/syncnode/config.json` if the file had no local changes before you started) and run `docker compose build pillar` once more so the stock image is back.

**Record the result** on #105 with the `dev` commit the image was built from, the pillar swap heights, and the sync node's final height.

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

The same announcement carries the operator rule from [Operating the activation window](#operating-the-activation-window): once the network has passed `EnforcementHeight`, any start from a clean or pre-activation data directory needs `"P2PBackend": "libp2p"`. During the notice period itself nodes stay on `auto`; the preparation is to check `BootstrapPeers`.

### Phase E — Governance activates the spork

The spork holder broadcasts `ActivateSpork(<libp2p-spork-id>)`. The activation's `EnforcementHeight` is set to `activation_momentum.Height + SporkMinHeightDelay` (~60s). The activation height is published publicly at least 2 weeks in advance.

### Phase F — Network swap

At `EnforcementHeight`, every node's switcher polls its oracle, sees activation, and fires `swap()` within ~1s. Legacy backends are torn down; libp2p hosts come up on the same TCP port.

**Expected behaviour during the swap window.** Consensus does not pause for the transport transition — momentum production is chain-deterministic and fires on its tick schedule regardless of the local peer set. As a consequence:

- The swap window is roughly the duration of `legacy.Stop()` + `libp2p.Start()`. On a healthy node with NAT mapping disabled this is well under a second; with UPnP/NAT-PMP enabled it can be several seconds.
- During that window the switcher has no active backend, so a momentum produced by the local pillar in this window broadcasts to an empty peer set (`protocol/handler.go:BroadcastMomentum`). The momentum exists in the local chain — it is not lost — but does not propagate via gossip.
- Up to ~1–2 momentums per node can be affected this way, depending on which slot the pillar is elected for during the window.
- The chain's normal block-sync path recovers these: once libp2p peers connect post-swap (within ~10s typically), `getBlocks` requests fetch missing momentums from peers that did receive them, and the head reconciles.

This is a known limitation; pausing momentum production to make the swap perfectly atomic would require coordinating consensus state across all pillars, which the spork model does not provide. The mitigation is to keep the swap window short (default-off NAT mapping helps) and rely on the block-sync recovery path that the chain already implements for normal network partitions.

**Production validation.** The devnet activation test (Phase F triggered at momentum 20) exercises the swap mechanism but not its scale behaviour — 3 pillars on a Docker bridge network is not representative of mainnet partition characteristics. The intended validation path before mainnet activation is:

1. Devnet smoke test (this branch — verified)
2. Testnet activation with a subset of mainnet pillars, soak for ≥30 days, observe swap behaviour under realistic NAT/network conditions
3. Mainnet activation only after the testnet soak passes

**Fresh nodes after the swap.** Until the libp2p-only binary is released, a node that starts from an empty database after this phase must run with `"P2PBackend": "libp2p"`, and a pillar that loses its database follows the recovery steps above. The release that carries the activation should ship the libp2p-only build (Option B in #105) the same day, fresh-sync-tested against the post-activation testnet, so that this rule has a short life.

### Phase G — Post-swap monitoring

For the first 24 hours, monitor:

- Every node has logged either `libp2p swap complete` (it was running through the swap) or a direct libp2p start (`starting libp2p backend directly`, a late joiner or a restart from a post-activation database), has no UDP listener on `ListenPort`, and `stats.networkInfo` `numPeers` recovered after the swap
- Momentum cadence unchanged (~10s/momentum)
- No partitioned forks across bootstrap operators
- Bootstrap-node dashboards show inbound libp2p connections

---

## Risks & Mitigations

| Risk | Severity | Mitigation |
|------|----------|------------|
| Operator misses Phase D binary upgrade | **High** | ≥2 weeks public notice; pillar-coordination check-ins. A node on the placeholder binary stays on legacy forever — safe default but will partition off. |
| libp2p bootstrap unavailability post-swap | **High** | Bootstrap operators upgrade first (they're a subset of pillar operators). Legacy bootstrap list is not a fallback — different transport. |
| Fresh node or resynced pillar after the swap starts legacy (`auto`) and never syncs (#105) | **High** | `"P2PBackend": "libp2p"` for any start from a clean or pre-activation database once the network has passed `EnforcementHeight`; recovery steps above for a pillar; libp2p-only binary released with the activation. The late-join runbook is the check, to be run against the activation release candidate. |
| libp2p host fails to start during swap | **Medium** | Switcher logs `Crit` to `zenon.log`/`zenon.error.log` and retries with backoff, leaving RPC up. Operator can restart znnd; on restart the spork is active so libp2p starts directly. |
| Bootstrap-list staleness over time | **Medium** | Persistent peerstore (LevelDB at `<DataPath>/network/libp2p-peerstore/`) means once a node has been online it remembers ~`MinConnectedPeers` peers across restarts. Bootstrap entries become a first-time-setup dependency rather than a runtime dependency. Mitigates the "147 stale seeders" failure mode from the legacy network. |
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
| Persistent peerstore (go-ds-leveldb) | **Done** | LevelDB-backed peerstore at `<DataPath>/network/libp2p-peerstore/`. Warm-bootstrap on startup dials up to `MinConnectedPeers` known peers in parallel with `BootstrapPeers`. |
| Late-join after the swap (#105) | **Override done, rest gated** | `Net.P2PBackend` override in `dev` (#121). Still open and tied to the activation release: the devnet late-join run (runbook above), and the libp2p-only binary. The dual-startup design in #105 is the post-release cleanup. |

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
