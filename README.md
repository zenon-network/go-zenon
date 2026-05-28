# Zenon Node

Reference Golang implementation of the Alphanet - Network of Momentum Phase 0.

## Building from source

Building `znnd` requires both a Go (version 1.16 or later) and a C compiler. You can install them using your favourite package manager. Once the dependencies are installed, please run:

```shell
make znnd
```

## Running `znnd`

Since version `0.0.2`, `znnd` is configured with the Alphanet Genesis and default seeders.

Use [znn-controller](https://github.com/zenon-network/znn_controller_dart) to configure your full node. For more information please consult the [Wiki](https://github.com/zenon-network/znn-wiki).

## PTLC local testing

This branch includes a local PTLC development and test workflow:

- A dockerized five-node devnet with three producing pillars, one dedicated RPC node, and one non-pillar observer/relay node.
- Genesis activation for the bridge/liquidity and PTLC sporks on the local devnet.
- Live PTLC RPC tests that run through the dedicated RPC node by default, including a two-party ZNN/QSR swap choreography over predefined testnet terms.
- Unit, adversarial, and live fuzz coverage for PTLC validation, signature-domain binding, replay competition, expiration, reclaim, and accounting behavior.

Useful commands:

```shell
make devnet-up          # start the local docker devnet
make devnet-down        # stop and wipe local devnet state
make testnet-ptlc       # reset devnet, run live PTLC RPC tests, tear down
make testnet-ptlc-keep  # run live PTLC RPC tests and leave devnet running
make ptlc-fuzz          # run PTLC unit/adversarial tests and live fuzz targets
```

Test reports are written under `test-results/`:

- `test-results/ptlc/<timestamp>/summary.md` for live RPC testnet runs.
- `test-results/ptlc-fuzz/<timestamp>/summary.md` for unit/adversarial/fuzz runs.

`test-results/` is intentionally ignored because the generated summaries and raw logs include machine-local absolute paths.

More detail:

- [PTLC contract docs](docs/ptlc/README.md)
- [Local devnet docs](docker/devnet/README.md)
- [Local devnet hardening notes](PTLC_DEVNET_HARDENING.md)
