# libp2p networking

## Summary

Nodes communicate over [libp2p](https://libp2p.io/). It is the only transport: there is no legacy devp2p/RLPX backend and no runtime switch. `node.NewNode` constructs a `*libp2p.Server` directly.

A node built from this code cannot peer with legacy-only nodes.

## Architecture

```
p2p/
├─ peer.go, server.go, protocol.go, message.go, peer_error.go   ← shared interfaces
├─ config.go            ← Net struct: BootstrapPeers, PeerstoreDir, NATPortMap
├─ discover/            ← NodeID/Node types
└─ libp2p/              ← libp2p server
   └─ server.go         ← DHT, Noise, exponential backoff, peer maintenance
```

`p2p.Server` and `p2p.Peer` are interfaces. `node.go`, `rpc/` and `protocol/` use them and never reach the concrete libp2p types. `protocol/handler.go` is transport-agnostic: it takes `p2p.Peer` and `p2p.MsgReadWriter`.

History sync needs no transport-specific code. Libp2p peers serve the full chain from genesis through the `/znn/eth/1` subprotocol messages (`GetBlockHashesFromNumberMsg`, `GetBlocksMsg`).

## Node identity

The `~/.znn/network-private-key` file (secp256k1 ECDSA) is the node identity. The libp2p `peer.ID` is the CIDv1 multihash of the compressed public key (`16Uiu2HAm...`).

## Configuration

### `config.json`

```json
"Net": {
  "ListenHost": "0.0.0.0",
  "ListenPort": 35995,
  "MinPeers": 8,
  "MinConnectedPeers": 16,
  "MaxPeers": 60,
  "MaxPendingPeers": 10,

  "BootstrapPeers": [
    "/ip4/1.2.3.4/tcp/35995/p2p/16Uiu2HAm..."
  ]
}
```

`p2p.DefaultBootstrapPeers` ships empty, so operators set `Net.BootstrapPeers`. A node with no bootstrap peers and an empty peerstore starts, logs a Crit line, and accepts inbound peers.

A `config.json` that still carries a `"Seeders"` key loads: the key is ignored. `libp2p.ParseBootstrapPeers` skips `enode://` entries with a warning.

### Persistent peerstore

The node remembers peers across restarts in a LevelDB-backed peerstore at `<DataPath>/network/libp2p-peerstore/`. On every successful Zenon handshake, it records the peer's dialable multiaddrs, a last-seen timestamp, and a dial-failure count (entries expire after 30 days unseen). On startup, a "warm bootstrap" dials up to `MinConnectedPeers` of those peers in parallel with the configured `BootstrapPeers`. Bootstrap entries are therefore a first-time-setup dependency, not a continuous-availability dependency.

Disk usage is about 1MB for a fully populated peerstore (about 1000 peers). The directory is created on first start.

The default path can be overridden with the optional `Net.PeerstoreDir` field. Omitting the field uses the default path. Setting it to `""` disables peer persistence, so every restart is a cold start (not recommended in production).

### `genesis.json` (devnet)

The devnet genesis activates the libp2p spork:

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

The spork has no runtime effect. `types.Libp2pSpork` stays in `ImplementedSporksMap` because a node halts on any activated spork missing from that map (`chain/momentum_pool.go`), and the devnet genesis activates it. The entry stays in `genesis.json` because changing it changes the devnet genesis hash.

## How to test

### Devnet end-to-end

```bash
make devnet-keys            # regenerate config files (keeps existing keys)
make devnet-up              # build and start 3 pillars, the RPC node and the observer
```

Pillars must keep producing momentums past height 20. Then start the late joiner:

```bash
make devnet-sync-reset && make devnet-sync-up
make devnet-sync-status     # poll until state is 2 and currentHeight equals targetHeight
```

The sync node bootstraps from the bootstrap pillar only. Check that it discovered other peers through the DHT:

```bash
curl -sX POST http://localhost:35999 -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"stats.networkInfo","params":[]}'
```

The `peers` list must contain at least one entry whose `ip` is not `172.30.0.10`.

### Unit tests

```bash
go test ./p2p/libp2p/        # identity, stream_rw, bootstrap parsing, server lifecycle
go test ./node/...           # net config
```

## Wire protocol

| Aspect | libp2p |
|--------|--------|
| Transport security | Noise protocol (XX pattern) |
| Peer identity | `peer.ID` (multihash of compressed pubkey) |
| Discovery | `go-libp2p-kad-dht` (over TCP) |
| Message framing | `[4-byte code][4-byte len][RLP payload]` (Noise encrypts below) |
| NAT | `libp2p.NATPortMap()` (opt-in via `Net.NATPortMap`) |
| Bootstrap format | `/ip4/<ip>/tcp/<port>/p2p/<peer-id>` |

The node listens on TCP port 35995. Nothing binds UDP.
