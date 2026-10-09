# go-zenon devnet

Self-contained five-node Network of Momentum (NoM) for local development
with a local web explorer. Three pillars produce in rotation, while a public
RPC node and a non-pillar observer node provide a more realistic relay/sync
path. Chain ID `69`, fully isolated from mainnet.

An optional sixth node — the [**sync node**](#sync-node-late-joiner) — stays
off by default and can be activated on demand to watch a cold node discover
peers and catch up to the chain frontier.

## Topology

| Service    | Container               | Static IP      | Role                                           | Host ports        |
|------------|-------------------------|----------------|------------------------------------------------|-------------------|
| `pillar`   | `znnd-devnet-pillar`    | `172.30.0.10`  | Pillar 1 producer + bootstrap (`MinPeers=0`)   | `35991` HTTP RPC  |
| `pillar2`  | `znnd-devnet-pillar2`   | `172.30.0.12`  | Pillar 2 producer                              | _none exposed_    |
| `pillar3`  | `znnd-devnet-pillar3`   | `172.30.0.13`  | Pillar 3 producer                              | _none exposed_    |
| `rpc`      | `znnd-devnet-rpc`       | `172.30.0.11`  | Public RPC ingress                             | `35997`, `35998`  |
| `observer` | `znnd-devnet-observer`  | `172.30.0.14`  | Non-pillar observer / relay peer               | _none exposed_    |
| `explorer` | `znnd-devnet-explorer`  | Docker-assigned | Static Zenon explorer                          | `36000`           |
| `syncnode` | `znnd-devnet-syncnode`  | `172.30.0.15`  | Cold late joiner (`sync` profile, off by default) | `35999`, `36001` |

All core nodes share the bridge network `znnd-devnet` (`172.30.0.0/24`).
The RPC and observer nodes have stable p2p identities and seed from all
three pillars plus each other. Pillar 2 and pillar 3 also seed each other,
which keeps transaction relay from depending on a single bootstrap path.

### Chain ID vs Network ID

The `ChainIdentifier` in `genesis.json` (`69`) is used as **both** the
chain ID and the p2p network ID. There is no separate network ID field —
the node passes `ChainIdentifier` directly to the protocol manager at
startup (`zenon/zenon.go`), which uses it during the peer handshake to
reject connections from nodes on other networks. Every momentum and
account block also carries this value, and it participates in block hash
computation.

Clients connecting to the devnet should set `chainId` to `69`. The
network layer handles `networkId` automatically from the same genesis
value.

Three pillars is the minimum that makes governance interesting on
devnet: any two voting Yes clears both the strict-majority and 33%
quorum gates the Accelerator and spork contracts use to approve
proposals.

## Bring it up

```sh
make devnet-up          # docker compose up -d --build
make devnet-down        # docker compose --profile sync down        (keeps chain state)
make devnet-down-wipe   # docker compose --profile sync down -v     (wipes chain state)
```

`devnet-down-wipe` is the reset button — the next `up` reproduces the
same genesis hash because keystores, network-private-keys,
`genesis.json`, and configs are all committed under `docker/devnet/`.

Use `devnet-down` to stop without losing chain state (e.g., to keep
an activated spork between restarts).

`make devnet-up` does **not** start the sync node — it lives behind the
`sync` compose profile. See [Sync node](#sync-node-late-joiner) below.

## Sync node (late joiner)

The `syncnode` service is a cold, non-producing node used to watch how a
fresh node behaves when it joins an already-running network and catches up
to the chain frontier. It is excluded from `make devnet-up` by the `sync`
compose profile, so you bring the core network up first, let it produce
momentums for a while, then activate the late joiner.

**Auto peer discovery.** Unlike the RPC and observer nodes (which seed from
every other node), the sync node is configured with a **single seeder — the
bootstrap pillar** (`172.30.0.10`). It finds pillar 2/3, the RPC node, and
the observer on its own through the discovery DHT, exactly like a brand-new
node joining with one known bootstrap address. It also ships **no committed
p2p identity**: `znnd` generates a random `network-private-key` on first
start, so nobody seeds *from* it and it never appears in another node's
seeder list.

Bring the core network up first (`make devnet-up`), then drive the late
joiner with:

```sh
make devnet-sync-up       # activate the late joiner (syncs from genesis)
make devnet-sync-logs     # follow peer discovery + block download
make devnet-sync-status   # poll {state, currentHeight, targetHeight} over RPC :35999
```

`devnet-sync-status` calls `stats.syncInfo` on port `35999`. Watch
`currentHeight` climb toward `targetHeight` (the best peer's height); `state`
is `0` = unknown, `1` = syncing, `2` = done, `3` = not-enough-peers yet.

To watch the catch-up in the explorer instead of the CLI, point it at the
late joiner:

```sh
EXPLORER_DEFAULT_ENDPOINT=http://localhost:35999 docker compose up -d --build explorer
```

### Reset modes

The sync node keeps its chain state in the container's writable layer (no
named volume), so the reset level is just how much you tear down before the
next `up`:

```sh
make devnet-sync-stop  && make devnet-sync-up   # no wipe — resume from current height
make devnet-sync-reset && make devnet-sync-up   # wipe sync node only — re-sync from genesis
make devnet-down       && make devnet-up        # full wipe — whole devnet, incl. sync node
```

## RPC endpoints

| Protocol  | URL                          |
|-----------|------------------------------|
| HTTP JSON | `http://localhost:35997`     |
| WebSocket | `ws://localhost:35998`       |
| Pillar 1 HTTP JSON | `http://localhost:35991` |
| Explorer | `http://localhost:36000` |
| Sync node HTTP JSON | `http://localhost:35999` (only while activated) |
| Sync node WebSocket | `ws://localhost:36001` (only while activated) |

## Explorer

The `explorer` service runs the static
[`zenon-network/explorer.zenon.network`](https://github.com/zenon-network/explorer.zenon.network)
bundle behind nginx. The image is built locally from `docker/explorer/Dockerfile`
and pins the upstream bundle to commit
`84b772981f0dd25ed52758f6244f9e1f8d54634b` for reproducible devnet runs.

Open the explorer at:

```text
http://localhost:36000
```

The explorer code runs in your browser, not inside the Docker network, so the
default RPC endpoint must be a host-reachable URL. The devnet image generates
`/devnet-endpoint.js` at container startup and injects it before the explorer
application loads. That script writes these browser local storage keys on every
page load:

| Key | Value |
|-----|-------|
| `defaultEndpoint` | `http://localhost:35997` |
| `nodes` | list with `http://localhost:35997` first |

The endpoint script is served with `Cache-Control: no-store` and intentionally
overwrites stale explorer settings. This keeps a browser that was previously
pointed at another Zenon node from silently showing balances from the wrong
network.

If you open the explorer from another machine, set the RPC endpoint to a URL
that machine can reach:

```sh
EXPLORER_DEFAULT_ENDPOINT=http://YOUR_DOCKER_HOST:35997 make devnet-up
```

After changing `EXPLORER_DEFAULT_ENDPOINT`, rebuild/recreate the explorer:

```sh
docker compose up -d --build explorer
```

Useful checks:

```sh
curl -s http://localhost:36000/devnet-endpoint.js

curl -sX POST http://localhost:35997 \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"ledger.getAccountInfoByAddress","params":["z1qpeet8dcjg0m6x6m3tg437wnc42aa2nez2fzth"]}'
```

Quick smoke check:

```sh
curl -sX POST http://localhost:35997 \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"ledger.getFrontierMomentum","params":[]}'
```

Pillar 2, pillar 3, and the observer only listen on the docker network.
To poke them directly use `docker exec`:

```sh
docker exec znnd-devnet-pillar2 wget -qO- \
  --post-data '{"jsonrpc":"2.0","id":1,"method":"ledger.getFrontierMomentum","params":[]}' \
  --header 'Content-Type: application/json' \
  http://localhost:35997
```

## Dev addresses

All addresses are derived from a fixed BIP-39 mnemonic at path
`m/44'/73404'/i'`. The mnemonic + password are committed to the repo —
**devnet only, never reuse on mainnet**.

```
mnemonic: abstract affair idle position alien fluid board ordinary exist afraid chapter wood wood guide sun walnut crew perfect place firm poverty model side million
password: devnet
```

| Index | Address                                          | Role                                  | Genesis balance              |
|-------|--------------------------------------------------|---------------------------------------|------------------------------|
| 0     | `z1qq9n7fpaqd8lpcljandzmx4xtku9w4ftwyg0mq`       | Pillar 1 producer (lives in pillar)   | —                            |
| 1     | `z1qq6eg8n43g032hanpsfp02qcdmv7zfj3y2lt5d`       | Pillar 1 owner / general dev wallet   | 10,000 ZNN, 100,000 QSR      |
| 2     | `z1qzmzssx28dc0fmvlca05hyxk2kgkgu7n0cj8pl`       | Spork address                         | —                            |
| 3     | `z1qp3yph55qgresyytz83anynr2f4z39x2z3ej3e`       | General dev account 1                 | 50,000 ZNN, 500,000 QSR      |
| 4     | `z1qz9zr5c7a0p8qljvrwt2cy5j99w98c5myrn2un`       | Pillar 2 producer (lives in pillar2)  | —                            |
| 5     | `z1qqleq9qc2u3039fly4ld5qgngdeapa3yks0e3l`       | Pillar 2 owner                        | —                            |
| 6     | `z1qzedcjmds6cwuqu7wkrvl0dadwwauzaana6g8e`       | Pillar 3 producer (lives in pillar3)  | —                            |
| 7     | `z1qq8gll9ey70nym5cjxjqcegymw0g8a4je6kwes`       | Pillar 3 owner                        | —                            |
| 8     | `z1qpeet8dcjg0m6x6m3tg437wnc42aa2nez2fzth`       | General dev account 2                 | 50,000 ZNN, 500,000 QSR      |
| 9     | `z1qqcam4ycu0ta8333hx38r5j2z3ry9jjfxkc7t5`       | General dev account 3                 | 50,000 ZNN, 500,000 QSR      |

The Accelerator contract (`z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22`)
is pre-funded with 1,000,000 ZNN + 10,000,000 QSR for proposal payouts.
The Pillar contract (`z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg`) holds
the 3 × 15,000 ZNN registration stakes (45,000 ZNN total).

### Importing addresses into a wallet

Producer keys (indices 0, 4, 6) live inside their respective pillar
containers' encrypted keystores — you generally don't need them on the
host. Pillar **owners** (indices 1, 5, 7) are the addresses that **vote**
on Accelerator projects, sporks, and other governance actions; import
those into syrius / znn-cli to drive proposals through.

znn-cli example (against the dev rpc):

```sh
# import the dev mnemonic
znn-cli wallet.createFromMnemonic "$MNEMONIC" devnet dev.json

# vote from pillar 2's owner (index 5)
znn-cli -u ws://localhost:35998 -k dev.json -p devnet --index 5 \
  pillar.vote <pillar-name> <yes|no|abstain>
```

### Reaching a 2/3 quorum

The Accelerator (and several other governance contracts) tally votes by
**pillar count**, not delegation weight: each pillar is one vote.
Strict majority + 33 % quorum means any **two** of `dev1`/`dev2`/`dev3`
voting Yes is enough to pass a project or phase. To produce a passing
vote on devnet, sign votes from indices 1 and 5 (or any other pair of
owners).

## Files in this directory

```
docker/devnet/
├── entrypoint.sh                       # role-aware seeder, runs in every container
├── genesis.json                        # ChainIdentifier 69, 3 pillars, dev allocations
├── observer/                           # non-pillar relay peer
│   ├── config.json
│   └── network-private-key
├── pillar/                             # pillar 1 (bootstrap)
│   ├── config.json                     # producer + RPC + Net.MinPeers=0
│   ├── network-private-key             # secp256k1 p2p key (committed)
│   └── wallet/
│       └── z1qq9n7...wyg0mq            # encrypted index-0 keystore
├── pillar2/                            # pillar 2
│   ├── config.json
│   ├── network-private-key
│   └── wallet/
│       └── z1qz9zr5...rn2un            # encrypted index-4 keystore
├── pillar3/                            # pillar 3
│   ├── config.json
│   ├── network-private-key
│   └── wallet/
│       └── z1qzedcj...a6g8e            # encrypted index-6 keystore
├── rpc/
│   ├── config.json                     # no producer, public RPC ingress
│   └── network-private-key
└── syncnode/                           # cold late joiner (sync profile)
    └── config.json                     # single bootstrap seeder, no committed key
```

All keystores are encrypted with the password `devnet`.

## Regenerating

`config.json` files and per-pillar keystores are produced by
[`cmd/devnet-keygen`](../../cmd/devnet-keygen). Re-run after editing
genesis, the keygen, or the static IPs:

```sh
make devnet-keys                # idempotent — leaves existing keys in place
make devnet-keys FORCE=1        # also rotate every keystore + p2p key
go run ./cmd/devnet-keygen --verify-genesis docker/devnet/genesis.json
```

`FORCE=1` will rotate every pillar's p2p key, which changes the enode
URL baked into the seeders list of every other config file — that's
fine because the keygen rewrites them all in the same run. This includes
`syncnode/config.json`, whose single bootstrap seeder is rewritten to the
rotated pillar 1 enode, so the late joiner keeps working after a rotation.
The sync node has no committed keystore or `network-private-key` — it joins
with a fresh identity each time it syncs from genesis.
