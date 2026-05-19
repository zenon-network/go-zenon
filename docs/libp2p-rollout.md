# libp2p Migration — Changes & Rollout Plan

## Summary

The custom devp2p/RLPX networking layer has been replaced with [libp2p](https://libp2p.io/), the ecosystem-standard Go P2P library. This is a hard network upgrade — all nodes must switch simultaneously.

---

## What Changed

### New Files

| File | Purpose |
|------|---------|
| `p2p/identity.go` | Converts existing secp256k1 ECDSA keys to libp2p `crypto.PrivKey` |
| `p2p/stream_rw.go` | Wraps libp2p streams into the existing `MsgReadWriter` interface |
| `p2p/bootstrap.go` | Parses multiaddr bootstrap peer strings |

### Rewritten Files

| File | Notes |
|------|-------|
| `p2p/server.go` | Same exported API (`Start`, `Stop`, `Peers`, `PeerCount`, `AddPeer`, `Self`). Internals use `libp2p.New()` host, DHT, stream handler. |
| `p2p/peer.go` | Same exported methods (`ID`, `Name`, `RemoteAddr`, `Caps`, `Disconnect`). Wraps libp2p stream instead of RLPX conn. |

### Modified Files

| File | Change |
|------|--------|
| `go.mod` | Go 1.20 → 1.21; added `go-libp2p`, `go-libp2p-kad-dht`, `go-multiaddr` |
| `Dockerfile` | `golang:1.20-alpine` → `golang:1.21-alpine` |
| `node/node.go` | Server literal uses `BootstrapPeers` instead of `BootstrapNodes` |
| `protocol/sync.go` | Map key changed from `discover.NodeID` to `string` |
| `cmd/devnet-keygen/main.go` | Generates multiaddr seeders instead of enode URLs |

### Deleted Files

| Path | Replaced By |
|------|-------------|
| `p2p/rlpx.go` | libp2p Noise security protocol |
| `p2p/dial.go` | libp2p built-in dialing + DHT |
| `p2p/metrics.go` | libp2p BandwidthReporter (optional) |
| `p2p/nat/` (entire directory) | `libp2p.NATPortMap()` |
| `p2p/discover/table.go`, `udp.go`, `database.go` | `go-libp2p-kad-dht` |

### Unchanged Files

- `protocol/handler.go` — zero changes (the key design goal)
- `protocol/peer.go` — zero changes
- `rpc/api/stats.go` — zero changes
- `p2p/message.go`, `p2p/protocol.go`, `p2p/peer_error.go` — zero changes
- `p2p/discover/node.go` — kept for `NodeID` and `Node` type definitions

---

## Wire Protocol Changes

| Aspect | Old (RLPX) | New (libp2p) |
|--------|------------|--------------|
| Transport security | Custom RLPX (ECDSA + AES-CTR + MAC) | Noise protocol |
| Peer identity | 64-byte secp256k1 uncompressed pubkey | libp2p `peer.ID` (multihash of compressed pubkey) |
| Discovery | Custom UDP Kademlia | `go-libp2p-kad-dht` |
| Message framing | RLPX encrypted frames | `[4-byte msg code][4-byte payload length][RLP payload]` |
| NAT | UPnP + NAT-PMP (custom) | `libp2p.NATPortMap()` |
| Bootstrap format | `enode://<pubkey>@<ip>:<port>` | `/ip4/<ip>/tcp/<port>/p2p/<peer-id>` |

---

## Node Identity

The existing `network-private-key` file (secp256k1 ECDSA) is **reused** — same key material, different encoding. However, the **peer ID changes format**:

- **Old**: `NodeID` = 64-byte raw uncompressed public key (displayed as 128 hex chars)
- **New**: `peer.ID` = CIDv1 multihash of the compressed public key (displayed as `16Uiu2HA...`)

This means any external systems that stored or compared old `NodeID` values will need updating.

---

## Configuration Changes

### Seeders Field

The `Seeders` field in `config.json` changes from enode URLs to multiaddr strings:

**Old format:**
```json
"Seeders": ["enode://91125cf1d998...@172.30.0.10:35995"]
```

**New format:**
```json
"Seeders": ["/ip4/172.30.0.10/tcp/35995/p2p/16Uiu2HAmRe2RazMPxYEV1A8Vs7LbVfNVMqBdiw7zgSkBLB3E2vQv"]
```

All other config fields (`ListenHost`, `ListenPort`, `MinPeers`, `MaxPeers`, etc.) remain unchanged.

### Regenerating Devnet Configs

```bash
make devnet-keys --force   # regenerate with multiaddr seeders
make devnet-up             # build & start
```

---

## Rollout Steps

### Prerequisites

1. **New bootstrap nodes must be deployed first.** The 147 hardcoded `enode://` bootstrap nodes in `p2p/config.go` (`DefaultSeeders`) are in legacy format and will not work with the libp2p implementation. `ParseBootstrapPeers` expects multiaddr format (`/ip4/.../tcp/.../p2p/...`). The community must supply new bootstrap nodes running the libp2p stack before mainnet rollout. Once the new nodes are online, `DefaultSeeders` must be replaced with their multiaddr strings. See **Legacy Config** below.

2. **Coordinate a network-wide upgrade.** This is a hard breaking change — nodes running the old RLPX stack cannot communicate with nodes running libp2p. All nodes must switch at the same time.

### Step 1: Deploy Bootstrap Nodes

1. Build the new binary: `make znnd`
2. On each bootstrap node:
   - Stop the old node
   - Replace the binary
   - Update `config.json` — change `Seeders` to multiaddr format (empty for bootstrap nodes)
   - Start the new node
3. Verify each bootstrap node is listening: look for `Listening on [/ip4/X.X.X.X/tcp/35995]` in logs
4. Record each bootstrap node's multiaddr for distribution

### Step 2: Distribute Bootstrap Multiaddrs

Publish the new bootstrap multiaddrs so node operators can update their configs. Format:

```
/ip4/<public-ip>/tcp/35995/p2p/<peer-id>
```

The peer ID can be derived from the node's existing `network-private-key` using:

```bash
# From the go-zenon repo
go run ./cmd/peerid <path-to-network-private-key>
```

Or extracted from the node's startup log.

### Step 3: Node Operator Upgrade

Each node operator should:

1. **Stop** their node
2. **Backup** `~/.znn/` directory (especially `network-private-key` and `wallet/`)
3. **Replace** the `znnd` binary with the new version
4. **Update** `~/.znn/config.json`:
   - Replace `Seeders` entries from enode URLs to multiaddr format
   - All other fields stay the same
5. **Start** the node
6. **Verify** connectivity:
   - Check logs for `connected to bootstrap peer` messages
   - Run `stats.networkInfo` RPC call — should show peers
   - Run `stats.syncInfo` — should show `SyncDone` once synced

### Step 4: Monitor

After rollout, monitor:

- **Peer connectivity**: All nodes should find peers within 30 seconds
- **Block production**: Momentums should continue at the same cadence
- **Sync status**: All nodes should reach `SyncDone`
- **Error logs**: Watch for handshake failures or protocol mismatches

---

## Risks & Mitigations

| Risk | Severity | Mitigation |
|------|----------|------------|
| Bootstrap node unavailability | **High** | Deploy new bootstrap nodes before upgrading any other nodes |
| All-or-nothing upgrade | **High** | Coordinate via governance vote or spork activation |
| go-ethereum dependency conflicts | **Medium** | Resolved during implementation — `go mod tidy` handles it |
| Peer ID format change | **Low** | Document the change; no database migration needed for most operators |
| `protocol/handler.go` changes | **None** | Zero changes — the key design win of the thin adapter approach |

---

## Testing

The docker devnet has been verified:

- 4 containers (3 pillars + 1 RPC) on a bridge network
- All nodes connect via multiaddr seeders
- All pillars produce momentums in round-robin
- RPC node syncs and serves requests
- Chain advances correctly

To reproduce:

```bash
make devnet-down           # clean slate
make devnet-keys --force   # regenerate keys & configs
make devnet-up             # build & start
# Wait ~30 seconds
curl -X POST -H "Content-Type: application/json" \
  --data '{"jsonrpc":"2.0","method":"stats.networkInfo","id":1}' \
  http://localhost:35997
```
