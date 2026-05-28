# go-zenon devnet

Self-contained five-node Network of Momentum (NoM) for local development.
Three pillars produce in rotation, while a public RPC node and a non-pillar
observer node keep the transaction relay path closer to a real testnet.
Chain ID `69`, fully isolated from mainnet. This local PTLC testnet variant also
activates the `bridge-liq` and `ptlc` sporks at genesis so the PTLC
embedded contract is available once the network has produced past the
genesis momentum.

## Topology

| Service    | Container               | Static IP      | Role                                           | Host ports        |
|------------|-------------------------|----------------|------------------------------------------------|-------------------|
| `pillar`   | `znnd-devnet-pillar`    | `172.30.0.10`  | Pillar 1 producer + bootstrap (`MinPeers=0`)   | `35991` HTTP RPC  |
| `pillar2`  | `znnd-devnet-pillar2`   | `172.30.0.12`  | Pillar 2 producer                              | _none exposed_    |
| `pillar3`  | `znnd-devnet-pillar3`   | `172.30.0.13`  | Pillar 3 producer                              | _none exposed_    |
| `rpc`      | `znnd-devnet-rpc`       | `172.30.0.11`  | Public RPC ingress                             | `35997`, `35998`  |
| `observer` | `znnd-devnet-observer`  | `172.30.0.14`  | Non-pillar observer / relay peer               | _none exposed_    |

All five share the bridge network `znnd-devnet` (`172.30.0.0/24`).
The RPC and observer nodes have stable p2p identities and seed from all
three pillars plus each other. Pillar 2 and pillar 3 also seed each other,
which keeps the dedicated RPC path from depending on a single bootstrap
connection.

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
make devnet-up      # docker compose up -d --build
make devnet-down    # docker compose down -v   (wipes chain state)
make testnet-ptlc   # reset devnet, run PTLC live-RPC tests, tear down
make ptlc-fuzz      # run PTLC unit/adversarial tests and live fuzz targets
```

`down -v` is the reset button — the next `up` reproduces the same
genesis hash because keystores, network-private-keys, `genesis.json`,
and configs are all committed under `docker/devnet/`.

`make testnet-ptlc` writes tester-friendly artifacts under
`test-results/ptlc/<timestamp>/`:

- `go-test.log` is the full verbose `go test` stream.
- `summary.md` lists the RPC endpoint, suite status, each test case, and
  the package result. The suite includes a two-party ZNN/QSR swap
  choreography and an abort/refund path over predefined devnet terms.

`make ptlc-fuzz` writes the same style of tester-friendly artifacts
under `test-results/ptlc-fuzz/<timestamp>/`:

- `unit-adversarial.log` is the verbose PTLC unit/adversarial test stream.
- `fuzz-*.log` files capture each live Go fuzz target run.
- `summary.md` lists the suite status, each test case, what it covers,
  fuzz execution counts, interesting inputs, and package results.

`test-results/` is ignored because generated logs and summaries include
absolute local paths.

## RPC endpoints

| Protocol  | URL                          |
|-----------|------------------------------|
| HTTP JSON | `http://localhost:35997`     |
| WebSocket | `ws://localhost:35998`       |
| Pillar 1 HTTP JSON | `http://localhost:35991` |

The PTLC live-RPC tests publish through the dedicated RPC node at
`http://localhost:35997` by default. To compare directly against the
producer RPC, override `PTLC_TESTNET_RPC=http://localhost:35991`.

Quick smoke check:

```sh
curl -sX POST http://localhost:35997 \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"ledger.getFrontierMomentum","params":[]}'
```

The other three nodes only listen on the docker network. To poke them
directly use `docker exec`:

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
| 3     | `z1qp3yph55qgresyytz83anynr2f4z39x2z3ej3e`       | General dev account                   | 50,000 ZNN, 500,000 QSR      |
| 4     | `z1qz9zr5c7a0p8qljvrwt2cy5j99w98c5myrn2un`       | Pillar 2 producer (lives in pillar2)  | —                            |
| 5     | `z1qqleq9qc2u3039fly4ld5qgngdeapa3yks0e3l`       | Pillar 2 owner                        | —                            |
| 6     | `z1qzedcjmds6cwuqu7wkrvl0dadwwauzaana6g8e`       | Pillar 3 producer (lives in pillar3)  | —                            |
| 7     | `z1qq8gll9ey70nym5cjxjqcegymw0g8a4je6kwes`       | Pillar 3 owner                        | —                            |

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
└── rpc/
    ├── config.json                     # no producer, public RPC ingress
    └── network-private-key
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
fine because the keygen rewrites them all in the same run.
